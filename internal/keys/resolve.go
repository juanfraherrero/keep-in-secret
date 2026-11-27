package keys

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// The key rule: every command that needs "my private key" calls Resolve, so
// given the same flags and environment they all identify you the same way.
// First match wins:
//
//  1. --identity PATH: only that file.
//  2. KIS_PRIVATE_KEY: the key's text (not a path); key files are ignored.
//  3. Default locations: $KIS_IDENTITY, then ConfigDir()/identity,
//     ~/.ssh/id_ed25519 and ~/.ssh/id_rsa, those that exist.
//
// A source that is set but unusable is an error: it never falls through to
// the next one.

const (
	// EnvPrivateKey holds the text of a private key (age, or SSH without a
	// passphrase) for machines that receive secrets as environment
	// variables: CI runners and cloud agents.
	EnvPrivateKey = "KIS_PRIVATE_KEY"
	// EnvIdentityPath is a path to a private key file, tried before the
	// standard places.
	EnvIdentityPath = "KIS_IDENTITY"
)

// From is the step of the rule that produced the key(s).
type From int

const (
	FromDefaults From = iota // default locations
	FromFlag                 // --identity PATH
	FromEnv                  // KIS_PRIVATE_KEY
)

// Key is a private key the rule selected.
type Key struct {
	Path  string // the file; empty for KIS_PRIVATE_KEY
	Type  string // TypeAge or TypeSSH
	Pub   string // public key; empty when Err is set
	Err   error  // a default-location file that can't be read as a key
	Label string // "--identity", "KIS_PRIVATE_KEY", "KIS_IDENTITY", or "" for a standard place

	id age.Identity // KIS_PRIVATE_KEY, already parsed
}

// Source is the key's file, or KIS_PRIVATE_KEY.
func (k Key) Source() string {
	if k.Path == "" {
		return EnvPrivateKey
	}
	return k.Path
}

// Describe says where the key comes from, for messages such as
// "unlocked as NAME (key from KIS_PRIVATE_KEY)".
func (k Key) Describe() string {
	switch {
	case k.Path == "":
		return "key from " + EnvPrivateKey
	case k.Label != "":
		return "key from " + k.Label + " " + k.Path
	}
	return "key " + k.Path
}

// Identity returns the private key; passphrase is only called when a
// passphrase-protected SSH key needs it.
func (k Key) Identity(passphrase func() ([]byte, error)) (age.Identity, error) {
	if k.id != nil {
		return k.id, nil
	}
	return LoadIdentity(k.Path, passphrase)
}

// Resolved is what the key rule found.
type Resolved struct {
	From From
	// Keys holds one key for FromFlag and FromEnv, and every existing
	// default-location file (possibly none) for FromDefaults.
	Keys []Key
}

// Resolve applies the key rule. flagPath is the command's --identity value.
// On error, Keys still holds the failing file when there is one, so callers
// can say what to do about it.
func Resolve(flagPath string) (Resolved, error) {
	if flagPath != "" {
		k, err := fileKey(flagPath, "--identity")
		return Resolved{From: FromFlag, Keys: []Key{k}}, err
	}
	if v := strings.TrimSpace(os.Getenv(EnvPrivateKey)); v != "" {
		id, pub, err := parsePrivateKey([]byte(v + "\n"))
		if err != nil {
			return Resolved{From: FromEnv}, fmt.Errorf("%s: %w", EnvPrivateKey, err)
		}
		return Resolved{From: FromEnv, Keys: []Key{{Type: RecipientType(pub), Pub: pub, Label: EnvPrivateKey, id: id}}}, nil
	}
	res := Resolved{From: FromDefaults}
	if p := os.Getenv(EnvIdentityPath); p != "" {
		k, err := fileKey(p, EnvIdentityPath)
		res.Keys = append(res.Keys, k)
		if err != nil {
			return res, err
		}
	}
	for _, p := range []string{DefaultAgeIdentityPath(), filepath.Join(sshDir(), "id_ed25519"), filepath.Join(sshDir(), "id_rsa")} {
		if _, err := os.Stat(p); err != nil || res.has(p) {
			continue
		}
		k, err := fileKey(p, "")
		k.Err = err
		res.Keys = append(res.Keys, k)
	}
	return res, nil
}

func (r Resolved) has(path string) bool {
	for _, k := range r.Keys {
		if k.Path == path {
			return true
		}
	}
	return false
}

// fileKey reads the public half of the private key file at path.
func fileKey(path, label string) (Key, error) {
	k := Key{Path: path, Label: label, Type: detectType(path)}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("no such file %s", path)
		}
		if label != "" {
			err = fmt.Errorf("%s: %w", label, err)
		}
		return k, err
	}
	pub, err := PublicKey(path)
	if err != nil {
		return k, err
	}
	k.Pub, k.Type = pub, RecipientType(pub)
	return k, nil
}
