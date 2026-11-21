package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"
)

func writeSSHKey(t *testing.T, dir string) (path, pub string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(sk, "")
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPub, _ := ssh.NewPublicKey(pk)
	return path, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

func TestWrapUnwrapAgeAndSSH(t *testing.T) {
	dir := t.TempDir()
	agePath := filepath.Join(dir, "identity")
	agePub, err := Generate(agePath)
	if err != nil {
		t.Fatal(err)
	}
	sshPath, sshPub := writeSSHKey(t, dir)
	if got, _ := PublicKey(sshPath); got != sshPub {
		t.Fatalf("ssh public key: got %q want %q", got, sshPub)
	}
	if got, _ := PublicKey(agePath); got != agePub {
		t.Fatalf("age public key: got %q want %q", got, agePub)
	}

	master := []byte("0123456789abcdef0123456789abcdef")
	wrapped, err := Wrap(master, []string{agePub, sshPub})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{agePath, sshPath} {
		id, err := LoadIdentity(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Unwrap(wrapped, id)
		if err != nil || !bytes.Equal(got, master) {
			t.Fatalf("%s: unwrap failed: %v", p, err)
		}
	}

	other, _ := age.GenerateX25519Identity()
	if _, err := Unwrap(wrapped, other); err == nil {
		t.Fatal("unrelated identity must not unwrap")
	}
}

func TestResolveRecipients(t *testing.T) {
	dir := t.TempDir()
	_, sshPub := writeSSHKey(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ana.keys" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "ecdsa-sha2-nistp256 AAAAE2VjZHNh\n%s\n", sshPub)
	}))
	defer srv.Close()
	GitHubKeysURL = srv.URL + "/%s.keys"

	got, err := ResolveRecipients("github:ana")
	if err != nil || len(got) != 1 || got[0] != sshPub {
		t.Fatalf("github: got %v, %v", got, err)
	}
	if _, err := ResolveRecipients("github:nobody"); err == nil {
		t.Fatal("unknown github user must fail")
	}

	pubFile := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(pubFile, []byte(sshPub+"\n"), 0o644)
	if got, err := ResolveRecipients(pubFile); err != nil || got[0] != sshPub {
		t.Fatalf("file: got %v, %v", got, err)
	}
	if _, err := ResolveRecipients(filepath.Join(dir, "id_ed25519")); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("private key file must be rejected, got %v", err)
	}
	if _, err := ResolveRecipients("not-a-key"); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestPassphraseProtectedSSHKey(t *testing.T) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(sk, "", []byte("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
	sshPub, _ := ssh.NewPublicKey(pk)
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	os.WriteFile(path+".pub", []byte(pub+"\n"), 0o644)

	master := []byte("0123456789abcdef0123456789abcdef")
	wrapped, err := Wrap(master, []string{pub})
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	id, err := LoadIdentity(path, func() ([]byte, error) { asked++; return []byte("s3cret"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatal("passphrase must only be asked when decrypting")
	}
	if got, err := Unwrap(wrapped, id); err != nil || !bytes.Equal(got, master) || asked != 1 {
		t.Fatalf("unwrap: %v, asked %d times", err, asked)
	}
}

// TestFormatV1Frozen unwraps a master key wrapped by kis v1, with both an age
// and an SSH identity. See testdata/v1/README.md.
func TestFormatV1Frozen(t *testing.T) {
	wrapped, err := os.ReadFile("testdata/v1/master.age")
	if err != nil {
		t.Fatal(err)
	}
	want := "kis-v1-fixture-key-do-not-use-!!"
	for _, p := range []string{"testdata/v1/identity", "testdata/v1/id_ed25519"} {
		id, err := LoadIdentity(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Unwrap(wrapped, id)
		if err != nil || string(got) != want {
			t.Fatalf("%s: cannot unwrap v1 fixture: %v (%q)", p, err, got)
		}
	}
}
