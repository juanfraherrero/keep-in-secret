package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
)

// These tests build the real kis binary and drive real git repositories, each
// "person" with their own HOME so keys and git config never leak between them.

var binDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kis-bin")
	if err != nil {
		panic(err)
	}
	binDir = dir
	out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "kis"), ".").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type person struct {
	t    *testing.T
	name string
	home string
}

func newPerson(t *testing.T, name string) *person {
	home := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return &person{t: t, name: name, home: home}
}

func (p *person) env() []string {
	return []string{
		"HOME=" + p.home,
		"XDG_CONFIG_HOME=" + filepath.Join(p.home, ".config"),
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=" + p.name, "GIT_AUTHOR_EMAIL=" + p.name + "@example.com",
		"GIT_COMMITTER_NAME=" + p.name, "GIT_COMMITTER_EMAIL=" + p.name + "@example.com",
	}
}

func (p *person) run(dir string, name string, args ...string) (string, error) {
	if name == "kis" {
		name = filepath.Join(binDir, "kis")
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = p.env()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *person) must(dir, name string, args ...string) string {
	p.t.Helper()
	out, err := p.run(dir, name, args...)
	if err != nil {
		p.t.Fatalf("%s: %s %s: %v\n%s", p.name, name, strings.Join(args, " "), err, out)
	}
	return out
}

func (p *person) fails(dir, name string, args ...string) string {
	p.t.Helper()
	out, err := p.run(dir, name, args...)
	if err == nil {
		p.t.Fatalf("%s: %s %s: expected failure\n%s", p.name, name, strings.Join(args, " "), out)
	}
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertEncryptedInHead(t *testing.T, p *person, dir, path string) {
	t.Helper()
	blob := p.must(dir, "git", "cat-file", "blob", "HEAD:"+path)
	if !crypt.IsEncrypted([]byte(blob)) {
		t.Fatalf("%s is stored in plaintext in HEAD: %q", path, blob)
	}
}

func assertClean(t *testing.T, p *person, dir string) {
	t.Helper()
	if out := p.must(dir, "git", "status", "--porcelain"); out != "" {
		t.Fatalf("%s: working tree not clean:\n%s", p.name, out)
	}
}

// setupOwner creates a bare remote and an owner repo with a gitignored .env,
// runs `kis init` and pushes.
func setupOwner(t *testing.T, initArgs ...string) (owner *person, remote, work string) {
	return setupOwnerAs(t, newPerson(t, "owner"), initArgs...)
}

func setupOwnerAs(t *testing.T, owner *person, initArgs ...string) (_ *person, remote, work string) {
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "owner-work")
	owner.must(root, "git", "init", "-q", "--bare", "-b", "main", remote)
	owner.must(root, "git", "init", "-q", "-b", "main", work)
	write(t, filepath.Join(work, "README.md"), "hello\n")
	write(t, filepath.Join(work, ".gitignore"), ".env\nnode_modules/\n")
	write(t, filepath.Join(work, ".env"), "SECRET=1\n")
	write(t, filepath.Join(work, ".env.example"), "SECRET=\n")

	out := owner.must(work, "kis", append([]string{"init", "-y", "--name", "owner"}, initArgs...)...)
	if !strings.Contains(out, "Next steps") {
		t.Fatalf("init output missing next steps:\n%s", out)
	}
	owner.must(work, "git", "add", ".")
	owner.must(work, "git", "commit", "-q", "-m", "init")
	owner.must(work, "git", "remote", "add", "origin", remote)
	owner.must(work, "git", "push", "-q", "-u", "origin", "main")

	assertEncryptedInHead(t, owner, work, ".env")
	if blob := owner.must(work, "git", "cat-file", "blob", "HEAD:.env.example"); blob != "SECRET=\n" {
		t.Fatalf(".env.example must stay plaintext, got %q", blob)
	}
	if got := read(t, filepath.Join(work, ".env")); got != "SECRET=1\n" {
		t.Fatalf("owner working copy should be plaintext, got %q", got)
	}
	assertClean(t, owner, work)
	return owner, remote, work
}

func TestOwnerAndPartnerFlow(t *testing.T) {
	owner, remote, work := setupOwner(t)

	// Partner creates a key and clones: without access they only see ciphertext.
	partner := newPerson(t, "partner")
	out := partner.must(partner.home, "kis", "keygen")
	pub := strings.TrimSpace(partner.must(partner.home, "kis", "pubkey"))
	pub = pub[strings.LastIndex(pub, "\n")+1:]
	if !strings.HasPrefix(pub, "age1") || !strings.Contains(out, pub) {
		t.Fatalf("unexpected keygen output:\n%s\npubkey: %s", out, pub)
	}
	pwork := filepath.Join(t.TempDir(), "partner-work")
	partner.must(partner.home, "git", "clone", "-q", remote, pwork)
	if !crypt.IsEncrypted([]byte(read(t, filepath.Join(pwork, ".env")))) {
		t.Fatal("partner without access must see ciphertext")
	}
	if out := partner.fails(pwork, "kis", "unlock"); !strings.Contains(out, pub) {
		t.Fatalf("failed unlock should print the public key to share:\n%s", out)
	}

	// Owner grants access.
	owner.must(work, "kis", "add", "partner", pub)
	owner.must(work, "git", "commit", "-q", "-m", "add partner")
	owner.must(work, "git", "push", "-q")

	partner.must(pwork, "git", "pull", "-q")
	partner.must(pwork, "kis", "unlock")
	if got := read(t, filepath.Join(pwork, ".env")); got != "SECRET=1\n" {
		t.Fatalf("partner should see plaintext after unlock, got %q", got)
	}
	assertClean(t, partner, pwork)

	// Partner changes the secret; owner receives it decrypted.
	write(t, filepath.Join(pwork, ".env"), "SECRET=2\n")
	partner.must(pwork, "git", "commit", "-q", "-am", "rotate secret")
	assertEncryptedInHead(t, partner, pwork, ".env")
	partner.must(pwork, "git", "push", "-q")

	owner.must(work, "git", "pull", "-q")
	if got := read(t, filepath.Join(work, ".env")); got != "SECRET=2\n" {
		t.Fatalf("owner should receive decrypted update, got %q", got)
	}

	// Diffs are readable.
	write(t, filepath.Join(work, ".env"), "SECRET=3\n")
	if d := owner.must(work, "git", "diff"); !strings.Contains(d, "-SECRET=2") || !strings.Contains(d, "+SECRET=3") {
		t.Fatalf("diff should be plaintext:\n%s", d)
	}
	owner.must(work, "git", "checkout", "--", ".env")

	// Removing the partner rotates the key: new commits are unreadable for them.
	owner.must(work, "kis", "remove", "partner")
	owner.must(work, "git", "commit", "-q", "-m", "remove partner")
	owner.must(work, "git", "push", "-q")
	if got := read(t, filepath.Join(work, ".env")); got != "SECRET=2\n" {
		t.Fatalf("owner keeps plaintext after rotation, got %q", got)
	}

	partner.must(pwork, "git", "pull", "-q")
	if !crypt.IsEncrypted([]byte(read(t, filepath.Join(pwork, ".env")))) {
		t.Fatal("removed partner must not be able to decrypt the rotated file")
	}
	partner.must(pwork, "kis", "lock", "--force")
	partner.fails(pwork, "kis", "unlock")
}

func TestSSHInitAndLockUnlock(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	owner := newPerson(t, "owner")
	if err := os.MkdirAll(filepath.Join(owner.home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner.must(owner.home, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(owner.home, ".ssh", "id_ed25519"))
	ownerSSH, _, work := setupOwnerAs(t, owner, "--ssh")
	if cfg := read(t, filepath.Join(work, ".kis", "config.toml")); !strings.Contains(cfg, `default_key = "ssh"`) {
		t.Fatalf("config should default to ssh:\n%s", cfg)
	}
	if rec := read(t, filepath.Join(work, ".kis", "recipients.toml")); !strings.Contains(rec, "ssh-ed25519 ") {
		t.Fatalf("recipient should be the ssh key:\n%s", rec)
	}

	ownerSSH.must(work, "kis", "lock")
	if !crypt.IsEncrypted([]byte(read(t, filepath.Join(work, ".env")))) {
		t.Fatal("lock should leave ciphertext in the working tree")
	}
	assertClean(t, ownerSSH, work)
	ownerSSH.must(work, "kis", "unlock")
	if got := read(t, filepath.Join(work, ".env")); got != "SECRET=1\n" {
		t.Fatalf("unlock with ssh key failed, got %q", got)
	}
	assertClean(t, ownerSSH, work)
}

func TestSafetyNets(t *testing.T) {
	owner, _, work := setupOwner(t)

	// Editing .gitencrypt without syncing blocks the commit.
	write(t, filepath.Join(work, "config", "secrets.json"), `{"token":"abc"}`)
	ge := read(t, filepath.Join(work, ".gitencrypt"))
	write(t, filepath.Join(work, ".gitencrypt"), ge+"config/secrets.json\n")
	owner.must(work, "git", "add", ".")
	if out := owner.fails(work, "git", "commit", "-q", "-m", "x"); !strings.Contains(out, "kis sync") {
		t.Fatalf("expected sync hint:\n%s", out)
	}
	owner.must(work, "kis", "sync")
	owner.must(work, "git", "commit", "-q", "-m", "encrypt secrets.json")
	assertEncryptedInHead(t, owner, work, "config/secrets.json")

	// Without the filter configured, plaintext gets staged and the hook stops it.
	owner.must(work, "git", "config", "--remove-section", "filter.kis")
	write(t, filepath.Join(work, ".env.local"), "LOCAL=1\n")
	owner.must(work, "git", "add", ".env.local")
	if out := owner.fails(work, "git", "commit", "-q", "-m", "leak"); !strings.Contains(out, "PLAINTEXT") {
		t.Fatalf("expected plaintext block:\n%s", out)
	}
	// unlock re-wires git and re-adding encrypts it.
	owner.must(work, "kis", "unlock")
	owner.must(work, "git", "add", ".env.local")
	owner.must(work, "git", "commit", "-q", "-m", "local env")
	assertEncryptedInHead(t, owner, work, ".env.local")

	// A locked repo refuses to encrypt new plaintext instead of leaking it.
	owner.must(work, "kis", "lock")
	write(t, filepath.Join(work, ".env.prod"), "PROD=1\n")
	owner.fails(work, "git", "add", ".env.prod")

	if out := owner.must(work, "kis", "status"); !strings.Contains(out, "LOCKED") || !strings.Contains(out, ".env.local") {
		t.Fatalf("unexpected status:\n%s", out)
	}
}
