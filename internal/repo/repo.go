// Package repo manages kis state inside a git repository: the committed
// .kis/ directory, the local master key cache and the git filter wiring.
package repo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/BurntSushi/toml"
	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
	"github.com/juanfraherrero/keep-in-secret/internal/gitencrypt"
	"github.com/juanfraherrero/keep-in-secret/internal/gitx"
	"github.com/juanfraherrero/keep-in-secret/internal/keys"
)

const (
	Dir            = ".kis"
	configFile     = ".kis/config.toml"
	recipientsFile = ".kis/recipients.toml"
	keysDir        = ".kis/keys"
)

var ErrLocked = errors.New("repository is locked: run `kis unlock`")
var ErrNotInitialized = errors.New("kis is not set up in this repository: run `kis init`")
var ErrNoAccess = errors.New("none of your keys can unlock this repository")

type Config struct {
	Version    int    `toml:"version"`
	DefaultKey string `toml:"default_key"`
}

type Repo struct {
	Root   string // working tree root
	GitDir string // common git dir (shared by worktrees)
}

// Open finds the repository containing the current directory.
func Open() (*Repo, error) {
	root, err := gitx.Run(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("not inside a git repository (run `git init` first)")
	}
	gitDir, err := gitx.Run(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return &Repo{Root: root, GitDir: gitDir}, nil
}

func (r *Repo) path(rel string) string { return filepath.Join(r.Root, filepath.FromSlash(rel)) }

func (r *Repo) Git(args ...string) (string, error) { return gitx.Run(r.Root, args...) }

func (r *Repo) Initialized() bool {
	_, err := os.Stat(r.path(configFile))
	return err == nil
}

func (r *Repo) LoadConfig() (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(r.path(configFile), &c); err != nil {
		if os.IsNotExist(err) {
			return c, ErrNotInitialized
		}
		return c, err
	}
	return c, nil
}

func (r *Repo) SaveConfig(c Config) error {
	return r.writeTOML(configFile, "# kis repository settings. default_key is the key type suggested to new partners.\n", c)
}

// Recipients maps a partner name to their public keys.
type Recipients map[string][]string

func (rs Recipients) Names() []string {
	names := make([]string, 0, len(rs))
	for n := range rs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (r *Repo) LoadRecipients() (Recipients, error) {
	rs := Recipients{}
	if _, err := toml.DecodeFile(r.path(recipientsFile), &rs); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return rs, nil
}

func (r *Repo) SaveRecipients(rs Recipients) error {
	return r.writeTOML(recipientsFile, "# Public keys of everyone who can unlock this repository. Managed by `kis add` / `kis remove`.\n", rs)
}

func (r *Repo) writeTOML(rel, header string, v any) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		return err
	}
	return r.writeFile(rel, buf.Bytes(), 0o644)
}

func (r *Repo) writeFile(rel string, data []byte, perm os.FileMode) error {
	p := r.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, perm)
}

func keyFile(name string) string { return keysDir + "/" + name + ".age" }

// WrapFor writes the master key encrypted to one partner's public keys.
func (r *Repo) WrapFor(name string, master []byte, pubs []string) error {
	wrapped, err := keys.Wrap(master, pubs)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return r.writeFile(keyFile(name), wrapped, 0o644)
}

func (r *Repo) RemoveKeyFile(name string) error {
	err := os.Remove(r.path(keyFile(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// UnwrapMasterKey tries every committed key file with the given identities.
func (r *Repo) UnwrapMasterKey(ids []age.Identity) ([]byte, string, error) {
	files, _ := filepath.Glob(filepath.Join(r.path(keysDir), "*.age"))
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no key files in %s", keysDir)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, "", err
		}
		master, err := keys.Unwrap(data, ids...)
		if err == nil && len(master) == crypt.KeySize {
			return master, strings.TrimSuffix(filepath.Base(f), ".age"), nil
		}
		// A key matched but could not be used (e.g. wrong SSH passphrase).
		var noMatch *age.NoIdentityMatchError
		if err != nil && !errors.As(err, &noMatch) {
			return nil, "", err
		}
	}
	return nil, "", ErrNoAccess
}

// Master key cache: .git/kis/key, never committed.

func (r *Repo) cachePath() string { return filepath.Join(r.GitDir, "kis", "key") }

func (r *Repo) MasterKey() ([]byte, error) {
	k, err := os.ReadFile(r.cachePath())
	if os.IsNotExist(err) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	if len(k) != crypt.KeySize {
		return nil, errors.New("corrupted key cache: run `kis unlock` again")
	}
	return k, nil
}

func (r *Repo) SaveMasterKey(k []byte) error {
	if err := os.MkdirAll(filepath.Dir(r.cachePath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(r.cachePath(), k, 0o600)
}

func (r *Repo) ForgetMasterKey() error {
	err := os.Remove(r.cachePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// TempDir creates a private scratch directory inside .git/kis for plaintext
// that must briefly touch disk. Callers remove it.
func (r *Repo) TempDir() (string, error) {
	dir := filepath.Join(r.GitDir, "kis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "tmp-")
}

func (r *Repo) Cipher() (*crypt.Cipher, error) {
	k, err := r.MasterKey()
	if err != nil {
		return nil, err
	}
	return crypt.New(k)
}

// .gitencrypt / .gitattributes

func (r *Repo) ReadGitencrypt() (string, bool) {
	data, err := os.ReadFile(r.path(gitencrypt.FileName))
	return string(data), err == nil
}

func (r *Repo) WriteDefaultGitencrypt() (bool, error) {
	if _, ok := r.ReadGitencrypt(); ok {
		return false, nil
	}
	return true, r.writeFile(gitencrypt.FileName, []byte(gitencrypt.DefaultContent), 0o644)
}

func (r *Repo) readAttributes() string {
	data, _ := os.ReadFile(r.path(".gitattributes"))
	return string(data)
}

// AttributesInSync reports whether .gitattributes reflects .gitencrypt.
func (r *Repo) AttributesInSync() bool {
	ge, ok := r.ReadGitencrypt()
	if !ok {
		return true
	}
	return gitencrypt.InSync(r.readAttributes(), ge)
}

// SyncAttributes rewrites the managed .gitattributes block. It returns true if
// the file changed.
func (r *Repo) SyncAttributes() (bool, error) {
	ge, ok := r.ReadGitencrypt()
	if !ok {
		return false, fmt.Errorf("%s not found", gitencrypt.FileName)
	}
	old := r.readAttributes()
	merged := gitencrypt.Merge(old, gitencrypt.Block(ge))
	if merged == old {
		return false, nil
	}
	return true, r.writeFile(".gitattributes", []byte(merged), 0o644)
}

// File listing

// filterMatches returns the subset of paths whose filter attribute is kis.
// cached reads .gitattributes from the index instead of the working tree.
func (r *Repo) filterMatches(paths []string, cached bool) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	args := []string{"check-attr", "-z", "--stdin", "filter"}
	if cached {
		args = append(args, "--cached")
	}
	out, err := gitx.RunInput(r.Root, []byte(strings.Join(paths, "\x00")+"\x00"), args...)
	if err != nil {
		return nil, err
	}
	parts := gitx.SplitZ(out)
	var matched []string
	for i := 0; i+2 < len(parts); i += 3 {
		if parts[i+2] == "kis" {
			matched = append(matched, parts[i])
		}
	}
	return matched, nil
}

func (r *Repo) lsFiles(args ...string) ([]string, error) {
	out, err := gitx.RunInput(r.Root, nil, append([]string{"ls-files", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}
	return gitx.SplitZ(out), nil
}

// TrackedSecrets lists tracked files that kis encrypts.
func (r *Repo) TrackedSecrets() ([]string, error) {
	files, err := r.lsFiles()
	if err != nil {
		return nil, err
	}
	return r.filterMatches(files, false)
}

// IndexedSecrets lists tracked files that the staged .gitattributes marks for
// encryption, i.e. what was encrypted before a pending .gitencrypt change.
func (r *Repo) IndexedSecrets() ([]string, error) {
	files, err := r.lsFiles()
	if err != nil {
		return nil, err
	}
	return r.filterMatches(files, true)
}

// UntrackedSecrets lists untracked files matching .gitencrypt, split into
// those that can be added and those hidden by .gitignore.
func (r *Repo) UntrackedSecrets() (addable, ignored []string, err error) {
	files, err := r.lsFiles("--others", "--exclude-standard")
	if err != nil {
		return nil, nil, err
	}
	if addable, err = r.filterMatches(files, false); err != nil {
		return nil, nil, err
	}
	files, err = r.lsFiles("--others", "--ignored", "--exclude-standard")
	if err != nil {
		return nil, nil, err
	}
	ignored, err = r.filterMatches(files, false)
	return addable, ignored, err
}

// StagedPlaintext lists staged files that should be encrypted but are not.
func (r *Repo) StagedPlaintext() ([]string, error) {
	out, err := gitx.RunInput(r.Root, nil, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	staged, err := r.filterMatches(gitx.SplitZ(out), true)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, p := range staged {
		blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", ":"+p)
		if err != nil {
			return nil, err
		}
		if !crypt.IsEncrypted(blob) {
			bad = append(bad, p)
		}
	}
	return bad, nil
}

// Dirty lists the given paths that have uncommitted changes.
func (r *Repo) Dirty(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out, err := gitx.RunInput(r.Root, nil, append([]string{"status", "--porcelain", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	var dirty []string
	for _, e := range gitx.SplitZ(out) {
		if len(e) > 3 {
			dirty = append(dirty, e[3:])
		}
	}
	return dirty, nil
}

// Renormalize re-runs the clean filter on tracked secrets and stages the result.
func (r *Repo) Renormalize(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.Git(append([]string{"add", "--renormalize", "--"}, paths...)...)
	return err
}

// Refresh re-checks-out tracked secrets whose working copy does not match the
// lock state (ciphertext while unlocked, plaintext while locked), so the
// smudge filter rewrites them. It returns how many files were rewritten.
func (r *Repo) Refresh(paths []string, wantEncrypted bool) (int, error) {
	var stale []string
	for _, p := range paths {
		data, err := os.ReadFile(r.path(p))
		if err != nil || crypt.IsEncrypted(data) != wantEncrypted {
			stale = append(stale, p)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}
	for _, p := range stale {
		_ = os.Remove(r.path(p))
	}
	_, err := r.Git(append([]string{"checkout", "--"}, stale...)...)
	return len(stale), err
}
