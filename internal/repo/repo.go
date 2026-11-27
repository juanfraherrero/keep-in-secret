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
	"github.com/juanfraherrero/keep-in-secret/internal/agents"
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

// ErrBroken is a repository that was set up with kis but lost .kis/config.toml.
// Every command refuses it: kis init would replace the master key and drop
// the partners, and the others would act on an incomplete setup.
var ErrBroken = errors.New("kis was set up in this repository, but .kis/config.toml is missing (deleted, or lost in a merge).\n" +
	"  Restore .kis/config.toml from git history; kis commands refuse to run until then.\n" +
	"  Don't run kis init again: it would replace the repository key and drop the partners in .kis/recipients.toml.")

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
	// --path-format=absolute needs git 2.31; resolve the path ourselves.
	gitDir, err := gitx.Run(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return &Repo{Root: root, GitDir: gitDir}, nil
}

func (r *Repo) path(rel string) string { return filepath.Join(r.Root, filepath.FromSlash(rel)) }

func (r *Repo) Git(args ...string) (string, error) { return gitx.Run(r.Root, args...) }

func (r *Repo) Initialized() bool {
	_, err := os.Stat(r.path(configFile))
	return err == nil
}

// Broken reports a .kis/ directory without config.toml: kis was set up here
// and a file was lost. config.toml is written by every successful kis init,
// so its absence next to other kis files always means damage.
func (r *Repo) Broken() bool {
	if r.Initialized() {
		return false
	}
	_, err := os.Stat(r.path(Dir))
	return err == nil
}

func (r *Repo) LoadConfig() (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(r.path(configFile), &c); err != nil {
		if os.IsNotExist(err) {
			if r.Broken() {
				return c, ErrBroken
			}
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

// Each group has its own master key, wrapped once per member. Default-group
// files keep the original layout: .kis/keys/NAME.age; group GROUP lives in
// .kis/keys/GROUP/NAME.age. Membership is simply which files exist.
func keyFile(group, name string) string {
	if group == crypt.DefaultGroup {
		return keysDir + "/" + name + ".age"
	}
	return keysDir + "/" + group + "/" + name + ".age"
}

// WrapFor writes a group's master key encrypted to one partner's public keys.
func (r *Repo) WrapFor(group, name string, master []byte, pubs []string) error {
	wrapped, err := keys.Wrap(master, pubs)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return r.writeFile(keyFile(group, name), wrapped, 0o644)
}

func (r *Repo) RemoveKeyFile(group, name string) error {
	err := os.Remove(r.path(keyFile(group, name)))
	if os.IsNotExist(err) {
		return nil
	}
	if err == nil && group != crypt.DefaultGroup {
		_ = os.Remove(filepath.Dir(r.path(keyFile(group, name)))) // only if now empty
	}
	return err
}

// Groups lists the groups that have a key: default first, then by name.
func (r *Repo) Groups() []string {
	out := []string{crypt.DefaultGroup}
	entries, _ := os.ReadDir(r.path(keysDir))
	for _, e := range entries {
		if e.IsDir() && len(r.Members(e.Name())) > 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

// HasGroup reports whether group has a key, i.e. at least one member.
func (r *Repo) HasGroup(group string) bool { return len(r.Members(group)) > 0 }

// Members lists who holds a group's key.
func (r *Repo) Members(group string) []string {
	files, _ := filepath.Glob(r.path(keyFile(group, "*")))
	var names []string
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".age"))
	}
	sort.Strings(names)
	return names
}

// GroupsOf lists the groups a partner is a member of.
func (r *Repo) GroupsOf(name string) []string {
	var out []string
	for _, g := range r.Groups() {
		if _, err := os.Stat(r.path(keyFile(g, name))); err == nil {
			out = append(out, g)
		}
	}
	return out
}

// UnwrapKeys opens every group key the given identities have access to. It
// returns the keys by group and the partner name they belong to.
func (r *Repo) UnwrapKeys(ids []age.Identity) (map[string][]byte, string, error) {
	got := map[string][]byte{}
	name := ""
	for _, g := range r.Groups() {
		candidates := r.Members(g)
		if name != "" {
			candidates = []string{name}
		}
		for _, n := range candidates {
			data, err := os.ReadFile(r.path(keyFile(g, n)))
			if err != nil {
				continue
			}
			master, err := keys.Unwrap(data, ids...)
			if err == nil && len(master) == crypt.KeySize {
				got[g], name = master, n
				break
			}
			// A key matched but could not be used (e.g. wrong SSH passphrase).
			var noMatch *age.NoIdentityMatchError
			if err != nil && !errors.As(err, &noMatch) {
				return nil, "", err
			}
		}
	}
	if len(got) == 0 {
		return nil, "", ErrNoAccess
	}
	return got, name, nil
}

// Key cache, never committed: .git/kis/key for the default group (the
// original location), .git/kis/groups/GROUP for the others.

func (r *Repo) cachePath(group string) string {
	if group == crypt.DefaultGroup {
		return filepath.Join(r.GitDir, "kis", "key")
	}
	return filepath.Join(r.GitDir, "kis", "groups", group)
}

// GroupKey returns a group's cached master key, ErrLocked when this clone
// has none (locked, or not a member).
func (r *Repo) GroupKey(group string) ([]byte, error) {
	k, err := os.ReadFile(r.cachePath(group))
	if os.IsNotExist(err) {
		if group != crypt.DefaultGroup {
			return nil, fmt.Errorf("no key for group %s here: ask a member to run `kis add YOUR_NAME --group %s`, then `kis unlock`", group, group)
		}
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

// MasterKey is the default group's key.
func (r *Repo) MasterKey() ([]byte, error) { return r.GroupKey(crypt.DefaultGroup) }

func (r *Repo) SaveGroupKey(group string, k []byte) error {
	p := r.cachePath(group)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, k, 0o600)
}

// HeldGroups lists the groups whose key is cached in this clone.
func (r *Repo) HeldGroups() []string {
	var out []string
	for _, g := range r.Groups() {
		if _, err := r.GroupKey(g); err == nil {
			out = append(out, g)
		}
	}
	return out
}

// ForgetKeys removes every cached group key.
func (r *Repo) ForgetKeys() error {
	if err := os.RemoveAll(filepath.Join(r.GitDir, "kis", "groups")); err != nil {
		return err
	}
	err := os.Remove(r.cachePath(crypt.DefaultGroup))
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

// Cipher returns the cipher for a group's files.
func (r *Repo) Cipher(group string) (*crypt.Cipher, error) {
	k, err := r.GroupKey(group)
	if err != nil {
		return nil, err
	}
	return crypt.NewGroup(k, group)
}

// SecretGroups maps each path to the group its kis-group attribute names.
func (r *Repo) SecretGroups(paths []string) (map[string]string, error) {
	out := map[string]string{}
	if len(paths) == 0 {
		return out, nil
	}
	res, err := gitx.RunInput(r.Root, []byte(strings.Join(paths, "\x00")+"\x00"), "check-attr", "-z", "--stdin", gitencrypt.GroupAttr)
	if err != nil {
		return nil, err
	}
	parts := gitx.SplitZ(res)
	for i := 0; i+2 < len(parts); i += 3 {
		g := parts[i+2]
		if g == "unspecified" || g == "unset" || g == "set" || g == "" {
			g = crypt.DefaultGroup
		}
		out[parts[i]] = g
	}
	return out, nil
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

// StagedWrongGroup lists staged secrets whose ciphertext was made with a
// different group's key than .gitencrypt assigns, which would let people
// outside the group read them.
func (r *Repo) StagedWrongGroup() ([]string, error) {
	out, err := gitx.RunInput(r.Root, nil, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	return r.wrongGroup(gitx.SplitZ(out), ":")
}

// wrongGroup returns the paths whose encrypted blob at rev (":" for the
// index) is for another group than their kis-group attribute.
func (r *Repo) wrongGroup(paths []string, rev string) ([]string, error) {
	secrets, err := r.filterMatches(paths, true)
	if err != nil {
		return nil, err
	}
	want, err := r.SecretGroups(secrets)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, p := range secrets {
		spec := rev + ":" + p
		if rev == ":" {
			spec = ":" + p
		}
		blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", spec)
		if err != nil {
			continue
		}
		if g, ok := crypt.GroupOf(blob); ok && g != want[p] {
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
	// A tracked file missing from disk is a deletion in progress: git add
	// would fail ("unable to stat") and there is nothing to encrypt. Its
	// index entry stays as is; kis status reports it if it is plaintext (#16).
	var present []string
	for _, p := range paths {
		if _, err := os.Lstat(r.path(p)); err == nil {
			present = append(present, p)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if len(present) == 0 {
		return nil
	}
	_, err := r.Git(append([]string{"add", "--renormalize", "--"}, present...)...)
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

// WriteAgentNotes adds or refreshes the kis section of AGENTS.md and of the
// optional agent files that exist. With onlyExisting, files without a kis
// section are left alone (how `kis sync` respects a removed section). It
// returns the files it changed.
func (r *Repo) WriteAgentNotes(onlyExisting bool) ([]string, error) {
	var changed []string
	for i, name := range append([]string{agents.FileName}, agents.Optional...) {
		data, err := os.ReadFile(r.path(name))
		if err != nil && (i > 0 || onlyExisting) {
			continue
		}
		old := string(data)
		if onlyExisting && !agents.Has(old) {
			continue
		}
		if merged := agents.Merge(old); merged != old {
			if err := r.writeFile(name, []byte(merged), 0o644); err != nil {
				return changed, err
			}
			changed = append(changed, name)
		}
	}
	return changed, nil
}
