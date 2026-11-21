// Package keys handles personal identities (age or SSH private keys),
// recipients (their public halves) and wrapping the repository master key
// for each recipient with age.
package keys

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/armor"
	"golang.org/x/crypto/ssh"
)

const (
	TypeAge = "age"
	TypeSSH = "ssh"
)

// ConfigDir is where kis keeps the user's own age identity.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "kis")
		}
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "kis")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kis")
}

func DefaultAgeIdentityPath() string { return filepath.Join(ConfigDir(), "identity") }

func sshDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

// Candidate is a private key found on this machine.
type Candidate struct {
	Path string
	Type string
}

// Candidates lists usable private keys in preference order: KIS_IDENTITY,
// the kis age identity, then SSH ed25519 and RSA keys.
func Candidates() []Candidate {
	var out []Candidate
	add := func(path string) {
		if path == "" {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		for _, c := range out {
			if c.Path == path {
				return
			}
		}
		out = append(out, Candidate{Path: path, Type: detectType(path)})
	}
	add(os.Getenv("KIS_IDENTITY"))
	add(DefaultAgeIdentityPath())
	add(filepath.Join(sshDir(), "id_ed25519"))
	add(filepath.Join(sshDir(), "id_rsa"))
	return out
}

func detectType(path string) string {
	data, err := os.ReadFile(path)
	if err == nil && bytes.Contains(data, []byte("AGE-SECRET-KEY-")) {
		return TypeAge
	}
	return TypeSSH
}

// Generate creates a new age identity at path and returns its public key.
func Generate(path string) (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	pub := id.Recipient().String()
	content := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n", time.Now().Format(time.RFC3339), pub, id.String())
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return pub, nil
}

// PublicKey returns the public key (recipient string) for a private key file.
func PublicKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if bytes.Contains(data, []byte("AGE-SECRET-KEY-")) {
		ids, err := age.ParseIdentities(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		if x, ok := ids[0].(*age.X25519Identity); ok {
			return x.Recipient().String(), nil
		}
		return "", fmt.Errorf("%s: unsupported age identity", path)
	}
	if pub, err := os.ReadFile(path + ".pub"); err == nil {
		return strings.TrimSpace(string(pub)), nil
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && missing.PublicKey != nil {
			return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(missing.PublicKey))), nil
		}
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

// LoadIdentity parses a private key file. Passphrase-protected SSH keys only
// call passphrase when they are actually needed to decrypt.
func LoadIdentity(path string, passphrase func() ([]byte, error)) (age.Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(data, []byte("AGE-SECRET-KEY-")) {
		ids, err := age.ParseIdentities(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return ids[0], nil
	}
	id, err := agessh.ParseIdentity(data)
	if err == nil {
		return id, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pub := missing.PublicKey
	if pub == nil {
		pubData, err := os.ReadFile(path + ".pub")
		if err != nil {
			return nil, fmt.Errorf("%s is passphrase-protected and %s.pub is missing", path, path)
		}
		if pub, _, _, _, err = ssh.ParseAuthorizedKey(pubData); err != nil {
			return nil, fmt.Errorf("%s.pub: %w", path, err)
		}
	}
	return agessh.NewEncryptedSSHIdentity(pub, data, passphrase)
}

// ParseRecipient validates a public key string.
func ParseRecipient(s string) (age.Recipient, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "age1"):
		return age.ParseX25519Recipient(s)
	case strings.HasPrefix(s, "ssh-ed25519 "), strings.HasPrefix(s, "ssh-rsa "):
		return agessh.ParseRecipient(s)
	case strings.HasPrefix(s, "sk-"), strings.HasPrefix(s, "ecdsa-"):
		return nil, fmt.Errorf("unsupported key type %q: use an ed25519 or RSA SSH key, or `kis keygen`", strings.Fields(s)[0])
	}
	return nil, fmt.Errorf("not a public key: %q", truncate(s))
}

// RecipientType classifies a public key string.
func RecipientType(s string) string {
	if strings.HasPrefix(s, "age1") {
		return TypeAge
	}
	return TypeSSH
}

// GitHubKeysURL is where `github:USER` public keys are fetched from.
// KIS_GITHUB_KEYS_URL overrides it (end-to-end tests use a local server).
var GitHubKeysURL = envOr("KIS_GITHUB_KEYS_URL", "https://github.com/%s.keys")

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ResolveRecipients expands a user-supplied key argument: a literal public
// key, "github:<user>", or a path to a file containing public keys.
func ResolveRecipients(arg string) ([]string, error) {
	var lines []string
	switch {
	case strings.HasPrefix(arg, "github:"):
		user := strings.TrimPrefix(arg, "github:")
		resp, err := http.Get(fmt.Sprintf(GitHubKeysURL, user))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("github user %q: %s", user, resp.Status)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return nil, err
		}
		for _, l := range splitLines(string(body)) {
			// Skip key types age cannot encrypt to (ecdsa, security keys).
			if _, err := ParseRecipient(l); err == nil {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			return nil, fmt.Errorf("github user %q has no ed25519 or RSA SSH keys", user)
		}
		return lines, nil
	case looksLikeFile(arg):
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, err
		}
		if bytes.Contains(data, []byte("PRIVATE KEY")) || bytes.Contains(data, []byte("AGE-SECRET-KEY-")) {
			return nil, fmt.Errorf("%s is a private key; pass the public key instead", arg)
		}
		lines = splitLines(string(data))
	default:
		lines = []string{strings.TrimSpace(arg)}
	}
	for _, l := range lines {
		if _, err := ParseRecipient(l); err != nil {
			return nil, err
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no public keys in %q", arg)
	}
	return lines, nil
}

func looksLikeFile(arg string) bool {
	if strings.HasPrefix(arg, "age1") || strings.HasPrefix(arg, "ssh-") {
		return false
	}
	_, err := os.Stat(arg)
	return err == nil
}

func splitLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// Wrap encrypts the master key to every recipient, ASCII-armored so the
// committed key files diff cleanly.
func Wrap(master []byte, recipients []string) ([]byte, error) {
	var rs []age.Recipient
	for _, s := range recipients {
		r, err := ParseRecipient(s)
		if err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	var buf bytes.Buffer
	a := armor.NewWriter(&buf)
	w, err := age.Encrypt(a, rs...)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(master); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := a.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unwrap decrypts a wrapped master key with any of the identities.
func Unwrap(wrapped []byte, identities ...age.Identity) ([]byte, error) {
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(wrapped)), identities...)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
