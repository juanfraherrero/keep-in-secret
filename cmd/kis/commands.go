package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"filippo.io/age"
	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
	"github.com/juanfraherrero/keep-in-secret/internal/gitencrypt"
	"github.com/juanfraherrero/keep-in-secret/internal/gitx"
	"github.com/juanfraherrero/keep-in-secret/internal/keys"
	"github.com/juanfraherrero/keep-in-secret/internal/repo"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func openInitialized() (*repo.Repo, error) {
	r, err := repo.Open()
	if err != nil {
		return nil, err
	}
	if r.Broken() {
		return nil, repo.ErrBroken
	}
	if !r.Initialized() {
		return nil, repo.ErrNotInitialized
	}
	return r, nil
}

// ---- init ----

func cmdInit(args []string) error {
	fs := newFlags("init")
	identityFlag(fs)
	name := fs.String("name", "", "your name in the recipients list")
	noAgents := fs.Bool("no-agents", false, "don't add the kis section to AGENTS.md (read by coding agents)")
	fs.BoolVar(&assumeYes, "y", false, "accept defaults without prompting")
	if pos := parseFlags(fs, args); len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}

	if err := gitx.CheckVersion(); err != nil {
		return err
	}
	r, err := repo.Open()
	if err != nil {
		return err
	}
	if r.Initialized() {
		return errors.New("kis is already set up here (.kis/ exists).\n  Need access?        kis unlock\n  Add a partner:      kis add NAME KEY")
	}
	// A .kis/ without config.toml was set up before and lost a file. A new
	// init would replace the master key, rewrite recipients.toml with only
	// you, and leave the other partners' .age files holding the old key.
	if r.Broken() {
		return repo.ErrBroken
	}

	fmt.Println("Setting up kis encryption in", r.Root)
	key, err := chooseIdentity()
	if err != nil {
		return identityError(err, key.Path)
	}
	pub, keyType := key.Pub, key.Type
	// Only age, ssh-ed25519 and ssh-rsa keys can receive the repository key.
	// Check before writing anything, so an ECDSA or sk- key leaves no files.
	if _, err := keys.ParseRecipient(pub); err != nil {
		return err
	}
	me := *name
	if me == "" {
		me = ask("Your name (as partners will see it)", defaultName(r))
	}
	if !validName.MatchString(me) {
		return fmt.Errorf("invalid name %q: use letters, digits, '.', '_' or '-'", me)
	}

	created, err := r.WriteDefaultGitencrypt()
	if err != nil {
		return err
	}
	if created {
		step("created .gitencrypt (.env, .env.* except examples). Edit it to add more paths")
	} else {
		step("using existing .gitencrypt")
	}

	master := crypt.NewKey()
	if err := r.SaveGroupKey(crypt.DefaultGroup, master); err != nil {
		return err
	}
	if err := r.WrapFor(crypt.DefaultGroup, me, master, []string{pub}); err != nil {
		return err
	}
	if err := r.SaveRecipients(repo.Recipients{me: {pub}}); err != nil {
		return err
	}
	if err := r.SaveConfig(repo.Config{Version: 1, DefaultKey: keyType}); err != nil {
		return err
	}
	step("created repository key, encrypted for %s (%s %s)", me, keyType, key.Describe())

	if err := wireGit(r); err != nil {
		return err
	}
	if err := ensureGroups(r); err != nil {
		return err
	}
	if _, err := r.SyncAttributes(); err != nil {
		return err
	}
	plainInHistory := committedPlaintext(r)
	if err := syncAndStage(r); err != nil {
		return err
	}
	step("generated .gitattributes and staged kis files")
	if !*noAgents {
		if err := writeAgentNotes(r, false); err != nil {
			return err
		}
	}

	if err := stageUntrackedSecrets(r); err != nil {
		return err
	}
	if len(plainInHistory) > 0 {
		warn("these were already committed in PLAINTEXT before: %s", strings.Join(plainInHistory, ", "))
		warn("they are encrypted from now on, but old commits still contain them: rotate those secrets")
	}

	if interactive() && confirm("Add partners now?", false) {
		for {
			pname := ask("Partner name (empty to finish)", "")
			if pname == "" {
				break
			}
			karg := ask("Their public key, github:USER, or path to a .pub file", "")
			if err := addRecipient(r, pname, []string{karg}, []string{crypt.DefaultGroup}, false); err != nil {
				warn("%v", err)
			}
		}
	}

	if secrets, _ := r.TrackedSecrets(); len(secrets) > 0 {
		step("encrypted files: %s", strings.Join(secrets, ", "))
	}
	warnOnlyKey(r, key)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Check what is encrypted:   kis status")
	fmt.Println("  2. Commit the setup and push it.")
	fmt.Println("  3. Give partners access:      kis add NAME KEY   (key formats: kis add -h)")
	fmt.Println("  4. Partners clone and run:    kis unlock")
	return nil
}

// warnOnlyKey warns when init ran without a terminal (a coding agent, CI) and
// its key is still the only partner: that machine or platform secret is often
// disposable, and losing it locks everyone out for good.
func warnOnlyKey(r *repo.Repo, key keys.Key) {
	if hasTerminal() {
		return
	}
	if rs, err := r.LoadRecipients(); err != nil || len(rs) != 1 {
		return
	}
	src := key.Path
	if src == "" {
		src = "the one in " + keys.EnvPrivateKey
	}
	warn("The only key that can unlock this repository is %s.", src)
	warn("If it is lost (this machine destroyed, the platform secret deleted), nobody can unlock it again.")
	warn("Add another partner (`kis add NAME KEY`) before pushing.")
}

// identityError adds what to do when your own key file can't be opened.
// Only kis's personal key file can be replaced by kis itself.
func identityError(err error, path string) error {
	if !errors.Is(err, keys.ErrUnreadableKey) {
		return err
	}
	if path == keys.DefaultAgeIdentityPath() {
		return fmt.Errorf("%w\n  Restore it from a backup if you have one, or replace it with a new key:\n"+
			"    kis keygen --force   (keeps the old file as identity.bak)\n"+
			"  Repositories you were already in then need a partner to run kis add YOUR_NAME NEW_KEY --force; their files are not re-encrypted", err)
	}
	return fmt.Errorf("%w\n  Restore it from a backup if you have one, or pick another key with --identity PATH", err)
}

// writeAgentNotes adds or refreshes the kis section coding agents read, and
// stages it. On sync (onlyExisting) a section someone removed stays removed.
func writeAgentNotes(r *repo.Repo, onlyExisting bool) error {
	changed, err := r.WriteAgentNotes(onlyExisting)
	if err != nil || len(changed) == 0 {
		return err
	}
	if _, err := r.Git(append([]string{"add", "--"}, changed...)...); err != nil {
		return err
	}
	step("updated %s so coding agents know these secrets are safe to commit", strings.Join(changed, ", "))
	return nil
}

// ensureGroups validates .gitencrypt and creates, with you as the first
// member, every group it names that has no key yet.
func ensureGroups(r *repo.Repo) error {
	ge, _ := r.ReadGitencrypt()
	if _, errs := gitencrypt.Parse(ge); len(errs) > 0 {
		return errors.Join(errs...)
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	for _, g := range gitencrypt.Groups(ge) {
		if r.HasGroup(g) {
			continue
		}
		_, me, err := groupKey(r, rs, g)
		if err != nil {
			return err
		}
		step("created group %s with you (%s) as its member; add others: kis add NAME --group %s", g, me, g)
	}
	return nil
}

// chooseIdentity picks init's key with the same rule as every command
// (keys.Resolve). --identity and KIS_PRIVATE_KEY are used as they are, and
// KIS_PRIVATE_KEY writes no key file. Otherwise you choose among the keys in
// the default locations, or create a new age key.
func chooseIdentity() (keys.Key, error) {
	res, err := keys.Resolve(identityPath)
	if err != nil {
		var k keys.Key
		if len(res.Keys) > 0 {
			k = res.Keys[len(res.Keys)-1]
		}
		return k, err
	}
	if res.From != keys.FromDefaults {
		return res.Keys[0], nil
	}
	cands := res.Keys

	options := make([]string, 0, len(cands)+1)
	def := len(cands) // "create new" unless an age key exists
	for i, c := range cands {
		options = append(options, fmt.Sprintf("Use existing %s key  %s", c.Type, c.Path))
		if c.Type == keys.TypeAge && def == len(cands) {
			def = i
		}
	}
	options = append(options, "Create a new kis key (recommended)")
	pick := choose("Which key do you want to use to unlock this repository?", options, def)
	if pick < len(cands) {
		return cands[pick], cands[pick].Err
	}
	path := keys.DefaultAgeIdentityPath()
	k := keys.Key{Path: path, Type: keys.TypeAge}
	if _, statErr := os.Stat(path); statErr == nil {
		k.Pub, err = keys.PublicKey(path)
		return k, err
	}
	k.Pub, err = keys.Generate(path)
	if err == nil {
		step("created your key at %s (keep it private, back it up)", path)
	}
	return k, err
}

func defaultName(r *repo.Repo) string {
	if email, err := r.Git("config", "user.email"); err == nil && email != "" {
		local, _, _ := strings.Cut(email, "@")
		if n := sanitizeName(local); n != "" {
			return n
		}
	}
	if u, err := user.Current(); err == nil {
		if n := sanitizeName(u.Username); n != "" {
			return n
		}
	}
	return "me"
}

func sanitizeName(s string) string {
	return regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(strings.ToLower(s), "-")
}

// wireGit installs the filter config and pre-commit hook on this machine.
func wireGit(r *repo.Repo) error {
	if err := r.ConfigureGit(); err != nil {
		return err
	}
	w, err := r.InstallHook()
	if err != nil {
		return err
	}
	if w != "" {
		warn("%s", w)
	} else {
		step("configured git filters and pre-commit check")
	}
	return nil
}

// syncAndStage regenerates .gitattributes, stages kis's own files and re-adds
// every tracked file whose encryption may have changed: current secrets get
// encrypted, files dropped from .gitencrypt go back to plaintext.
func syncAndStage(r *repo.Repo) error {
	previous, err := r.IndexedSecrets()
	if err != nil {
		return err
	}
	if _, err := r.SyncAttributes(); err != nil {
		return err
	}
	if _, err := r.Git("add", "--", ".gitencrypt", ".gitattributes", repo.Dir); err != nil {
		return err
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return err
	}
	leaving := subtract(previous, secrets)
	// The filter no longer applies to these, so decrypt any working copy
	// still holding ciphertext before it is re-added as plaintext.
	for _, p := range leaving {
		path := filepath.Join(r.Root, filepath.FromSlash(p))
		data, err := os.ReadFile(path)
		group, encrypted := crypt.GroupOf(data)
		if err != nil || !encrypted {
			continue
		}
		c, err := r.Cipher(group)
		if err != nil {
			return fmt.Errorf("%s would stop being encrypted, which needs the plaintext: %w", p, err)
		}
		plain, err := c.Decrypt(data)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if err := os.WriteFile(path, plain, 0o644); err != nil {
			return err
		}
	}
	return r.Renormalize(append(secrets, leaving...))
}

func subtract(a, b []string) []string {
	drop := map[string]bool{}
	for _, x := range b {
		drop[x] = true
	}
	var out []string
	for _, x := range a {
		if !drop[x] {
			out = append(out, x)
		}
	}
	return out
}

// committedPlaintext lists secrets whose HEAD version is not encrypted.
func committedPlaintext(r *repo.Repo) []string {
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range secrets {
		blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", "HEAD:"+p)
		if err == nil && !crypt.IsEncrypted(blob) {
			out = append(out, p)
		}
	}
	return out
}

// stageUntrackedSecrets offers to add secrets that are untracked or hidden by
// .gitignore, which is the usual state of a .env before kis.
func stageUntrackedSecrets(r *repo.Repo) error {
	addable, ignored, err := r.UntrackedSecrets()
	if err != nil {
		return err
	}
	if len(ignored) > 0 {
		warn("ignored by .gitignore: %s", strings.Join(ignored, ", "))
		if confirm("Stop ignoring them so they are shared (encrypted)?", true) {
			if err := unignore(r, ignored); err != nil {
				return err
			}
			addable2, stillIgnored, err := r.UntrackedSecrets()
			if err != nil {
				return err
			}
			addable = addable2
			if len(stillIgnored) > 0 {
				warn("still ignored (check nested .gitignore files): %s", strings.Join(stillIgnored, ", "))
			} else {
				step("un-ignored them in .gitignore")
			}
		}
	}
	if len(addable) > 0 && confirm(fmt.Sprintf("Add and encrypt %s now?", strings.Join(addable, ", ")), true) {
		if _, err := r.Git(append([]string{"add", "--"}, addable...)...); err != nil {
			return err
		}
		step("staged encrypted: %s", strings.Join(addable, ", "))
	} else if len(addable) > 0 {
		warn("not staged yet: %s (run `git add` when ready)", strings.Join(addable, ", "))
	}
	return nil
}

func unignore(r *repo.Repo, paths []string) error {
	p := filepath.Join(r.Root, ".gitignore")
	data, _ := os.ReadFile(p)
	s := string(data)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += "\n# kis: committed encrypted, see .gitencrypt\n"
	for _, f := range paths {
		s += "!/" + f + "\n"
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		return err
	}
	_, err := r.Git("add", "--", ".gitignore")
	return err
}

// ---- keygen / pubkey ----

func cmdKeygen(args []string) error {
	fs := newFlags("keygen")
	force := fs.Bool("force", false, "replace an existing key (the old one is kept as identity.bak)")
	parseFlags(fs, args)

	path := keys.DefaultAgeIdentityPath()
	if _, err := os.Stat(path); err == nil {
		if !*force {
			pub, err := keys.PublicKey(path)
			if err != nil {
				return err
			}
			fmt.Printf("You already have a kis key at %s\n\nYour public key (safe to share):\n\n    %s\n\n", path, pub)
			printKeygenNext(pub)
			return nil
		}
		if err := os.Rename(path, path+".bak"); err != nil {
			return err
		}
	}
	pub, err := keys.Generate(path)
	if err != nil {
		return err
	}
	fmt.Printf("Created your kis key at %s\nKeep that file private and backed up; it is what unlocks your repositories.\n\n", path)
	fmt.Printf("Your public key (safe to share):\n\n    %s\n\n", pub)
	printKeygenNext(pub)
	return nil
}

func printKeygenNext(pub string) {
	fmt.Println("Next:")
	fmt.Println("  1. Send the public key above to someone who already has access.")
	fmt.Printf("  2. They run:  kis add YOUR_NAME %s\n", pub)
	fmt.Println("     and push the change.")
	fmt.Println("  3. You pull (or clone) and run:  kis unlock")
}

// cmdPubkey prints the public keys the key rule selects here, each with its
// source, so "which key is kis using?" has one answer.
func cmdPubkey(args []string) error {
	fs := newFlags("pubkey")
	identityFlag(fs)
	if pos := parseFlags(fs, args); len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	res, err := keys.Resolve(identityPath)
	if err != nil {
		return err
	}
	switch res.From {
	case keys.FromEnv:
		k := res.Keys[0]
		fmt.Printf("# %s (%s); key files are ignored while it is set\n%s\n", keys.EnvPrivateKey, k.Type, k.Pub)
		return nil
	case keys.FromFlag:
		k := res.Keys[0]
		fmt.Printf("# %s (%s, from --identity); other keys are ignored\n%s\n", k.Path, k.Type, k.Pub)
		return nil
	}
	if len(res.Keys) == 0 {
		return errors.New("no keys found: run `kis keygen`")
	}
	for _, k := range res.Keys {
		if k.Err != nil {
			warn("%s: %v", k.Path, k.Err)
			continue
		}
		from := ""
		if k.Label != "" {
			from = ", from " + k.Label
		}
		fmt.Printf("# %s (%s%s)\n%s\n", k.Path, k.Type, from, k.Pub)
	}
	return nil
}

// ---- add / remove / rotate ----

func cmdAdd(args []string) error {
	fs := newFlags("add")
	force := fs.Bool("force", false, "replace the keys of an existing partner")
	groupFlag := fs.String("group", "", "groups to add them to, comma-separated (default: the default group)")
	newKey := fs.Bool("new-key", false, "create their key pair now and print the private key once (for CI and cloud agents)")
	identityFlag(fs)
	pos := parseFlags(fs, args)
	if len(pos) < 1 {
		fs.Usage()
		return exitError{}
	}
	var secret string
	if *newKey {
		if len(pos) > 1 {
			return errors.New("--new-key creates the key, so don't pass one")
		}
		pub, err := "", error(nil)
		if secret, pub, err = keys.NewIdentity(); err != nil {
			return err
		}
		pos = append(pos, pub)
	}
	groups, err := parseGroups(*groupFlag)
	if err != nil {
		return err
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}
	if err := addRecipient(r, pos[0], pos[1:], groups, *force); err != nil {
		return err
	}
	if secret == "" {
		fmt.Printf("\nNow commit and push, then ask %s to run:  kis unlock\n", pos[0])
		return nil
	}
	fmt.Printf("\nPrivate key of %s (shown once, saved nowhere):\n\n    %s\n\n", pos[0], secret)
	fmt.Printf("Store it as the %s secret of the CI or cloud agent that acts as %s;\n", keys.EnvPrivateKey, pos[0])
	fmt.Println("there, `kis unlock` reads it from the environment.")
	fmt.Println("Now commit and push. Lost the key? kis add " + pos[0] + " --new-key --force")
	return nil
}

// parseGroups reads a --group flag; empty means the default group.
func parseGroups(flag string) ([]string, error) {
	if flag == "" {
		return []string{crypt.DefaultGroup}, nil
	}
	var out []string
	for _, g := range strings.Split(flag, ",") {
		g = strings.TrimSpace(g)
		if !validName.MatchString(g) {
			return nil, fmt.Errorf("invalid group %q: use letters, digits, '.', '_' or '-'", g)
		}
		out = append(out, g)
	}
	return out, nil
}

// addRecipient gives name access to groups. Without keyArgs, name must be
// an existing partner, whose keys are reused. A group that doesn't exist yet
// is created, with you as a member too.
func addRecipient(r *repo.Repo, name string, keyArgs []string, groups []string, force bool) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name %q: use letters, digits, '.', '_' or '-'", name)
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	_, exists := rs[name]
	var pubs []string
	switch {
	case len(keyArgs) == 0 && !exists:
		return fmt.Errorf("%s is not a partner yet: kis add %s KEY|github:USER|FILE.pub", name, name)
	case len(keyArgs) == 0:
		pubs = rs[name]
	case exists && !force:
		return fmt.Errorf("%s already has access; use --force to replace their keys, or omit the key to add them to a group: kis add %s --group GROUP", name, name)
	}
	var fromGitHub []string // github:USER arguments that returned several keys
	for _, a := range keyArgs {
		p, err := keys.ResolveRecipients(a)
		if err != nil {
			return err
		}
		if strings.HasPrefix(a, "github:") && len(p) > 1 {
			fromGitHub = append(fromGitHub, fmt.Sprintf("%s (%d keys)", a, len(p)))
		}
		pubs = append(pubs, p...)
	}
	for _, g := range groups {
		master, created, err := groupKey(r, rs, g)
		if err != nil {
			return err
		}
		if !force && len(keyArgs) == 0 && slices.Contains(r.Members(g), name) {
			return fmt.Errorf("%s is already in group %s", name, g)
		}
		if err := r.WrapFor(g, name, master, pubs); err != nil {
			return err
		}
		if created != "" {
			step("created group %s (you are a member as %s)", g, created)
		}
	}
	// Replaced keys must also open the groups they were already in.
	if force && len(keyArgs) > 0 {
		for _, g := range r.GroupsOf(name) {
			if slices.Contains(groups, g) {
				continue
			}
			master, err := r.GroupKey(g)
			if err != nil {
				return fmt.Errorf("%s is in group %s, which you can't open to re-wrap for the new keys: %w", name, g, err)
			}
			if err := r.WrapFor(g, name, master, pubs); err != nil {
				return err
			}
		}
	}
	rs[name] = pubs
	if err := r.SaveRecipients(rs); err != nil {
		return err
	}
	if _, err := r.Git("add", "--", repo.Dir); err != nil {
		return err
	}
	// Repositories without groups never hear about them.
	in := ""
	if len(r.Groups()) > 1 {
		in = " to " + strings.Join(groups, ", ")
	}
	step("added %s%s (%d key(s), %s)", name, in, len(pubs), keys.RecipientType(pubs[0]))
	// Every key on a GitHub account can unlock: access is only as safe as the
	// least protected of them, and removing one on GitHub doesn't revoke it.
	if len(fromGitHub) > 0 {
		note("%s: every one of these keys can unlock. To allow a single key, ask %s for it and run: kis add %s KEY --force",
			strings.Join(fromGitHub, ", "), name, name)
	}
	return nil
}

// groupKey returns a group's master key. A group nobody holds yet is
// created here, wrapped for you; created is then your partner name.
func groupKey(r *repo.Repo, rs repo.Recipients, group string) (master []byte, created string, err error) {
	if r.HasGroup(group) {
		master, err = r.GroupKey(group)
		return master, "", err
	}
	me, err := whoAmI(rs)
	if err != nil {
		return nil, "", fmt.Errorf("creating group %s: %w", group, err)
	}
	master = crypt.NewKey()
	if err := r.WrapFor(group, me, master, rs[me]); err != nil {
		return nil, "", err
	}
	return master, me, r.SaveGroupKey(group, master)
}

// whoAmI finds the partner whose public key is one of this machine's keys.
func whoAmI(rs repo.Recipients) (string, error) {
	name, _, err := identify(rs)
	return name, err
}

// notPartnerError: the key rule worked, but no partner holds the key(s).
type notPartnerError string

func (e notPartnerError) Error() string { return string(e) }

// identify is whoAmI plus the key that proves it, found with the key rule
// (keys.Resolve) and the running command's --identity.
func identify(rs repo.Recipients) (name string, key keys.Key, err error) {
	res, err := keys.Resolve(identityPath)
	if err != nil {
		return "", keys.Key{}, err
	}
	if name, key, ok := partnerOf(rs, res.Keys); ok {
		return name, key, nil
	}
	switch res.From {
	case keys.FromEnv:
		return "", keys.Key{}, notPartnerError("the key in " + keys.EnvPrivateKey + " belongs to no partner (see `kis pubkey` and `kis status`)")
	case keys.FromFlag:
		return "", keys.Key{}, notPartnerError("the key from --identity " + identityPath + " belongs to no partner (see `kis status`)")
	}
	return "", keys.Key{}, notPartnerError("none of this machine's keys belongs to a partner (see `kis pubkey` and `kis status`)")
}

// partnerOf finds the first partner (by name) holding one of ks.
func partnerOf(rs repo.Recipients, ks []keys.Key) (string, keys.Key, bool) {
	for _, n := range rs.Names() {
		for _, pub := range rs[n] {
			for _, k := range ks {
				if k.Err == nil && normalizeKey(k.Pub) == normalizeKey(pub) {
					return n, k, true
				}
			}
		}
	}
	return "", keys.Key{}, false
}

// normalizeKey drops an SSH key's comment so the same key always compares equal.
func normalizeKey(pub string) string {
	f := strings.Fields(pub)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return pub
}

func cmdRemove(args []string) error {
	fs := newFlags("remove")
	noRotate := fs.Bool("no-rotate", false, "skip rotating the group keys (they could still decrypt future changes)")
	force := fs.Bool("force", false, "rotate even with uncommitted changes to encrypted files")
	groupFlag := fs.String("group", "", "only remove them from these groups, comma-separated (default: every group, and the partner list)")
	pos := parseFlags(fs, args)
	if len(pos) != 1 {
		fs.Usage()
		return exitError{}
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	name := pos[0]
	if _, ok := rs[name]; !ok {
		return fmt.Errorf("%s is not a partner (see `kis status`)", name)
	}
	in := r.GroupsOf(name)
	groups := in
	if *groupFlag != "" {
		if groups, err = parseGroups(*groupFlag); err != nil {
			return err
		}
		for _, g := range groups {
			if !slices.Contains(in, g) {
				return fmt.Errorf("%s is not in group %s", name, g)
			}
		}
	}
	// Every check runs for every group before anything is written: a refusal
	// half way would delete the key file without rotating, and a retry would
	// then find nothing to rotate (#10).
	for _, g := range groups {
		if len(r.Members(g)) == 1 {
			if g == crypt.DefaultGroup {
				return errors.New("cannot remove the last partner: nobody could unlock the repository")
			}
			return fmt.Errorf("cannot remove the last member of group %s: nobody could decrypt its files", g)
		}
		if *noRotate {
			continue
		}
		if _, err := r.GroupKey(g); err != nil {
			return fmt.Errorf("you are not in group %s, so you can't rotate its key; ask one of %s, or use --no-rotate", g, strings.Join(r.Members(g), ", "))
		}
		if err := checkClean(r, g, *force); err != nil {
			return err
		}
	}
	for _, g := range groups {
		if err := r.RemoveKeyFile(g, name); err != nil {
			return err
		}
		if !*noRotate {
			if err := rotateGroup(r, rs, g, *force); err != nil {
				return err
			}
		}
	}
	// In recipients.toml but in no group: an earlier kis remove deleted the
	// key file and stopped before rotating (#10). Nothing here says which
	// groups they could read, so no key is rotated now.
	keyless := len(in) == 0
	dropped := len(r.GroupsOf(name)) == 0
	if dropped {
		delete(rs, name)
		if err := r.SaveRecipients(rs); err != nil {
			return err
		}
	}
	if _, err := r.Git("add", "-A", "--", repo.Dir); err != nil {
		return err
	}
	if keyless {
		fmt.Printf("  removed %s from the partner list\n", name)
		warn("%s had no key file in any group, so no group key was rotated", name)
		warn("if %s ever ran kis unlock, they can still decrypt new secrets: run `kis rotate` (every group you are in), and ask a member of each other group to run it too", name)
		warn("%s can still read every secret in past commits: change the real credentials (DB passwords, API keys)", name)
	} else if dropped {
		step("removed %s", name)
		warn("%s can still read every secret in past commits: change the real credentials (DB passwords, API keys)", name)
	} else {
		step("removed %s from %s", name, strings.Join(groups, ", "))
		warn("%s can still read %s secrets in past commits: change the real credentials (DB passwords, API keys)", name, strings.Join(groups, ", "))
	}
	if keyless {
		fmt.Println("\nCommit and push.")
		return nil
	}
	fmt.Println("\nCommit and push. Partners who pull will get the new key automatically.")
	return nil
}

func cmdRotate(args []string) error {
	fs := newFlags("rotate")
	force := fs.Bool("force", false, "rotate even with uncommitted changes to encrypted files")
	groupFlag := fs.String("group", "", "groups to rotate, comma-separated (default: every group you are in)")
	parseFlags(fs, args)
	r, err := openInitialized()
	if err != nil {
		return err
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	groups := r.HeldGroups()
	if *groupFlag != "" {
		if groups, err = parseGroups(*groupFlag); err != nil {
			return err
		}
	}
	if len(groups) == 0 {
		return repo.ErrLocked
	}
	for _, g := range groups {
		if err := rotateGroup(r, rs, g, *force); err != nil {
			return err
		}
	}
	if _, err := r.Git("add", "--", repo.Dir); err != nil {
		return err
	}
	fmt.Println("\nCommit and push. Partners who pull will get the new key automatically.")
	return nil
}

// rotateGroup replaces a group's master key, wraps it for the group's
// current members and re-encrypts the group's tracked files.
func rotateGroup(r *repo.Repo, rs repo.Recipients, group string, force bool) error {
	if _, err := r.GroupKey(group); err != nil {
		return err
	}
	if err := checkClean(r, group, force); err != nil {
		return err
	}
	files, err := groupFiles(r, group)
	if err != nil {
		return err
	}
	if _, err := r.Refresh(files, false); err != nil { // working copies must be plaintext
		return err
	}
	master := crypt.NewKey()
	members := r.Members(group)
	for _, n := range members {
		if err := r.WrapFor(group, n, master, rs[n]); err != nil {
			return err
		}
	}
	if err := r.SaveGroupKey(group, master); err != nil {
		return err
	}
	if err := r.Renormalize(files); err != nil {
		return err
	}
	what := "repository"
	if group != crypt.DefaultGroup {
		what = group
	}
	step("new %s key for %s; re-encrypted %d file(s)", what, strings.Join(members, ", "), len(files))
	return nil
}

// checkClean refuses to rotate group while its tracked secrets have
// uncommitted changes, unless force.
func checkClean(r *repo.Repo, group string, force bool) error {
	if force {
		return nil
	}
	files, err := groupFiles(r, group)
	if err != nil {
		return err
	}
	dirty, err := r.Dirty(files)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return fmt.Errorf("uncommitted changes in %s: commit them first or use --force", strings.Join(dirty, ", "))
	}
	return nil
}

// groupFiles lists the tracked secrets .gitencrypt puts in group.
func groupFiles(r *repo.Repo, group string) ([]string, error) {
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return nil, err
	}
	groups, err := r.SecretGroups(secrets)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range secrets {
		if groups[p] == group {
			out = append(out, p)
		}
	}
	return out, nil
}

// readable splits tracked secrets into those this clone holds the key for
// and, by group, those it doesn't.
func readable(r *repo.Repo) (ok []string, locked map[string][]string, err error) {
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return nil, nil, err
	}
	groups, err := r.SecretGroups(secrets)
	if err != nil {
		return nil, nil, err
	}
	held := r.HeldGroups()
	locked = map[string][]string{}
	for _, p := range secrets {
		if slices.Contains(held, groups[p]) {
			ok = append(ok, p)
		} else {
			locked[groups[p]] = append(locked[groups[p]], p)
		}
	}
	return ok, locked, nil
}

// ---- unlock / lock ----

func cmdUnlock(args []string) error {
	fs := newFlags("unlock")
	identityFlag(fs)
	parseFlags(fs, args)
	if err := gitx.CheckVersion(); err != nil {
		return err
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}

	res, err := keys.Resolve(identityPath)
	if err != nil {
		return err
	}
	if len(res.Keys) == 0 {
		return errors.New("no key found on this machine.\n  Run `kis keygen`, send the printed public key to someone with access,\n  and run `kis unlock` again once they have added you")
	}
	var ids []age.Identity
	var loaded []keys.Key
	for _, k := range res.Keys {
		if k.Err != nil {
			warn("skipping %v", k.Err)
			continue
		}
		id, err := k.Identity(passphrasePrompt(k.Path))
		if err != nil {
			if res.From == keys.FromFlag {
				return err
			}
			warn("skipping %v", err)
			continue
		}
		ids = append(ids, id)
		loaded = append(loaded, k)
	}
	masters, name, err := r.UnwrapKeys(ids)
	if err != nil && !errors.Is(err, repo.ErrNoAccess) {
		if strings.Contains(err.Error(), "decryption password incorrect") {
			return errors.New("incorrect passphrase for your SSH key; nothing was unlocked")
		}
		return err
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kis: none of your keys has access to this repository yet.")
		fmt.Fprintln(os.Stderr, "Send one of these public keys to someone with access and ask them to run `kis add YOUR_NAME KEY`:")
		for _, k := range res.Keys {
			if k.Err != nil {
				continue
			}
			if k.Path == "" {
				fmt.Fprintf(os.Stderr, "    %s  (from %s)\n", k.Pub, keys.EnvPrivateKey)
			} else {
				fmt.Fprintf(os.Stderr, "    %s\n", k.Pub)
			}
		}
		return exitError{}
	}
	// Start clean: keys of groups you were removed from must not linger.
	if err := r.ForgetKeys(); err != nil {
		return err
	}
	var groups []string
	for _, g := range r.Groups() {
		if k, ok := masters[g]; ok {
			if err := r.SaveGroupKey(g, k); err != nil {
				return err
			}
			groups = append(groups, g)
		}
	}
	if err := wireGit(r); err != nil {
		return err
	}
	files, locked, err := readable(r)
	if err != nil {
		return err
	}
	n, err := r.Refresh(files, false)
	if err != nil {
		return err
	}
	// Secrets staged while the filter was missing are plaintext in the index;
	// `git add` alone may skip them (unchanged stat), so re-clean them here.
	staged, err := r.StagedPlaintext()
	if err != nil {
		return err
	}
	if err := r.Renormalize(staged); err != nil {
		return err
	}
	who := name
	if rs, err := r.LoadRecipients(); err == nil {
		if _, k, ok := partnerOf(repo.Recipients{name: rs[name]}, loaded); ok {
			who += " (" + k.Describe() + ")"
		}
	}
	if len(r.Groups()) == 1 {
		step("unlocked as %s; decrypted %d file(s)", who, n)
	} else {
		step("unlocked as %s (groups: %s); decrypted %d file(s)", who, strings.Join(groups, ", "), n)
	}
	for _, g := range slices.Sorted(maps.Keys(locked)) {
		verb := map[bool]string{true: "stays", false: "stay"}[len(locked[g]) == 1]
		note("not in group %s: %s %s encrypted (ask a member: kis add %s --group %s)", g, strings.Join(locked[g], ", "), verb, name, g)
	}
	if len(staged) > 0 {
		step("re-encrypted staged %s", strings.Join(staged, ", "))
	}
	return nil
}

func cmdLock(args []string) error {
	fs := newFlags("lock")
	force := fs.Bool("force", false, "lock even with uncommitted changes to encrypted files (they are lost)")
	parseFlags(fs, args)
	r, err := openInitialized()
	if err != nil {
		return err
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return err
	}
	if dirty, err := r.Dirty(secrets); err != nil {
		return err
	} else if len(dirty) > 0 && !*force {
		return fmt.Errorf("uncommitted changes in %s would be lost: commit them or use --force", strings.Join(dirty, ", "))
	}
	if err := r.ForgetKeys(); err != nil {
		return err
	}
	n, err := r.Refresh(secrets, true)
	if err != nil {
		return err
	}
	step("locked; %d file(s) back to ciphertext. Run `kis unlock` to decrypt again", n)
	return nil
}

// ---- status / sync ----

func cmdStatus(args []string) error {
	fs := newFlags("status")
	check := fs.Bool("check", false, "exit with status 1 unless it is safe to push (for CI and hooks)")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	since := fs.String("since", "", "also check every commit in REV..HEAD, e.g. origin/main for a pull request")
	if pos := parseFlags(fs, args); len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	st, err := collectStatus(*since)
	if err != nil {
		if *asJSON && errors.Is(err, repo.ErrNotInitialized) {
			printJSON(statusJSON{Version: version, Scope: statusScope, Encrypted: []string{}, Files: []repo.FileStatus{}, Problems: []repo.Problem{}})
			return exitError{}
		}
		if *asJSON && errors.Is(err, repo.ErrBroken) {
			printJSON(statusJSON{Version: version, Scope: statusScope, Encrypted: []string{}, Files: []repo.FileStatus{},
				Problems: []repo.Problem{{Severity: repo.SeverityError, Path: ".kis/config.toml",
					Message: "kis was set up here, but this file is missing; don't run kis init again, it would drop the partners",
					Fix:     "restore .kis/config.toml from git history"}}})
			return exitError{}
		}
		return err
	}
	if *asJSON {
		printJSON(st)
	} else {
		printStatus(st)
	}
	if *check && !st.SafeToPush {
		return exitError{}
	}
	return nil
}

type statusJSON struct {
	Version     string            `json:"kis_version"`
	Initialized bool              `json:"initialized"`
	SafeToPush  bool              `json:"safe_to_push"`
	Repository  string            `json:"repository,omitempty"`
	Git         *gitJSON          `json:"git,omitempty"`
	Unlocked    bool              `json:"unlocked"`
	You         string            `json:"you,omitempty"`      // the partner this machine's key belongs to
	YourKey     string            `json:"your_key,omitempty"` // that key's file, or KIS_PRIVATE_KEY
	GitFilters  bool              `json:"git_filters"`
	PreCommit   bool              `json:"pre_commit_hook"`
	InSync      bool              `json:"attributes_in_sync"`
	DefaultKey  string            `json:"default_key,omitempty"`
	Partners    []partnerJSON     `json:"partners,omitempty"`
	Groups      []groupJSON       `json:"groups,omitempty"`
	Encrypted   []string          `json:"encrypted_files"`
	Scope       string            `json:"scope"`
	Files       []repo.FileStatus `json:"files"`
	Problems    []repo.Problem    `json:"problems"`
}

const statusScope = "files matched by .gitencrypt; other files are not scanned for secrets"

type gitJSON struct {
	Version   string `json:"version"`
	Supported bool   `json:"supported"`
}

type partnerJSON struct {
	Name   string   `json:"name"`
	Keys   int      `json:"keys"`
	Type   string   `json:"type"`
	Groups []string `json:"groups"`
}

type groupJSON struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	HaveKey bool     `json:"have_key"`
}

func collectStatus(since string) (statusJSON, error) {
	r, err := openInitialized()
	if err != nil {
		return statusJSON{}, err
	}
	cfg, err := r.LoadConfig()
	if err != nil {
		return statusJSON{}, err
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return statusJSON{}, err
	}
	audit, err := r.Audit(since)
	if err != nil {
		return statusJSON{}, err
	}
	held := r.HeldGroups()
	st := statusJSON{
		Version:     version,
		Initialized: true,
		Scope:       statusScope,
		Repository:  r.Root,
		Unlocked:    len(held) > 0,
		GitFilters:  r.GitConfigured(),
		PreCommit:   r.HookInstalled(),
		InSync:      r.AttributesInSync(),
		DefaultKey:  cfg.DefaultKey,
		Files:       audit.Files,
		Problems:    audit.Problems,
	}
	you, yourKey, err := identify(rs)
	if err == nil {
		st.You, st.YourKey = you, yourKey.Source()
	} else if _, notPartner := err.(notPartnerError); !notPartner {
		// A KIS_PRIVATE_KEY or KIS_IDENTITY that can't be used would
		// otherwise just read as "not a partner".
		st.Problems = append(st.Problems, repo.Problem{Severity: repo.SeverityWarning,
			Message: err.Error(), Fix: "set that variable to a usable private key, or unset it"})
	}
	if v, err := gitx.Version(); err == nil {
		st.Git = &gitJSON{fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]), gitx.Supported(v)}
	}
	for _, n := range rs.Names() {
		st.Partners = append(st.Partners, partnerJSON{n, len(rs[n]), keys.RecipientType(rs[n][0]), r.GroupsOf(n)})
	}
	for _, g := range r.Groups() {
		st.Groups = append(st.Groups, groupJSON{g, r.Members(g), slices.Contains(held, g)})
	}
	// A clone that cannot encrypt on `git add` may leak the next commit,
	// but CI checkouts never commit, so these only warn.
	if st.Unlocked && !st.GitFilters {
		st.Problems = append(st.Problems, repo.Problem{Severity: repo.SeverityWarning,
			Message: "git filters are not configured in this clone: `git add` would stage secrets in plaintext", Fix: "kis unlock"})
	}
	if st.Unlocked && !st.PreCommit {
		st.Problems = append(st.Problems, repo.Problem{Severity: repo.SeverityWarning,
			Message: "the pre-commit check is not installed in this clone", Fix: "kis unlock"})
	}
	if st.Git != nil && !st.Git.Supported {
		st.Problems = append(st.Problems, repo.Problem{Severity: repo.SeverityWarning,
			Message: fmt.Sprintf("git %s is too old: kis needs %d.%d+", st.Git.Version, gitx.MinVersion[0], gitx.MinVersion[1]), Fix: fmt.Sprintf("upgrade git to %d.%d or newer", gitx.MinVersion[0], gitx.MinVersion[1])})
	}
	st.SafeToPush = repo.Audit{Problems: st.Problems}.Safe()
	if st.Problems == nil {
		st.Problems = []repo.Problem{}
	}
	if st.Files == nil {
		st.Files = []repo.FileStatus{}
	}
	st.Encrypted = []string{}
	for _, f := range st.Files {
		if f.State == repo.FileEncrypted {
			st.Encrypted = append(st.Encrypted, f.Path)
		}
	}
	return st, nil
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func printStatus(st statusJSON) {
	yesNo := func(ok bool, fix string) string {
		if ok {
			return "ok"
		}
		return "MISSING (run `" + fix + "`)"
	}
	fmt.Printf("repository      %s\n", st.Repository)
	if st.Git != nil {
		note := "ok"
		if !st.Git.Supported {
			note = fmt.Sprintf("TOO OLD: kis needs %d.%d+, upgrade git", gitx.MinVersion[0], gitx.MinVersion[1])
		}
		fmt.Printf("git             %s %s\n", st.Git.Version, note)
	}
	state := "unlocked"
	if !st.Unlocked {
		state = "LOCKED (run `kis unlock`)"
	}
	fmt.Printf("state           %s\n", state)
	if st.You != "" {
		fmt.Printf("you             %s (key %s)\n", st.You, st.YourKey)
	} else {
		fmt.Println("you             not a partner: no key here has access (see `kis pubkey`)")
	}
	fmt.Printf("git filters     %s\n", yesNo(st.GitFilters, "kis unlock"))
	fmt.Printf("pre-commit      %s\n", yesNo(st.PreCommit, "kis unlock"))
	sync := "ok"
	if !st.InSync {
		sync = "OUT OF DATE (run `kis sync`)"
	}
	fmt.Printf(".gitattributes  %s\n", sync)
	fmt.Printf("default key     %s\n", st.DefaultKey)
	grouped := len(st.Groups) > 1
	fmt.Println("partners")
	for _, p := range st.Partners {
		in := ""
		if grouped {
			in = "  in " + strings.Join(p.Groups, ", ")
		}
		fmt.Printf("  %-14s %d %s key(s)%s\n", p.Name, p.Keys, p.Type, in)
	}
	fmt.Println("encrypted files")
	if len(st.Files) == 0 {
		fmt.Println("  (none tracked yet)")
	}
	for _, f := range st.Files {
		group := ""
		if grouped && f.Group != "" {
			group = " [" + f.Group + "]"
			if !f.Readable && f.State == repo.FileEncrypted {
				group = " [" + f.Group + ", no access here]"
			}
		}
		fmt.Printf("  %-30s %s%s\n", f.Path, map[string]string{
			repo.FileEncrypted:  "encrypted in git",
			repo.FilePlainIndex: "PLAINTEXT in index (run `kis sync`)",
			repo.FilePlainHead:  "PLAINTEXT in last commit",
			repo.FileUntracked:  "untracked (git add it to share)",
			repo.FileIgnored:    "ignored by .gitignore (won't be shared)",
			repo.FileWrongGroup: "encrypted for the WRONG group (run `kis sync`)",
		}[f.State], group)
	}
	switch {
	case !st.SafeToPush:
		fmt.Println("safe to push    NO: fix the problems below before pushing")
	case len(st.Encrypted) == 0:
		fmt.Println("safe to push    yes: no secret files are tracked yet")
	default:
		fmt.Printf("safe to push    yes: %s %s encrypted in git\n", strings.Join(st.Encrypted, ", "), map[bool]string{true: "is", false: "are"}[len(st.Encrypted) == 1])
	}
	fmt.Println("                (only files matched by .gitencrypt are checked; other files are not scanned for secrets)")
	for _, p := range st.Problems {
		mark, where := "!", p.Path
		if p.Severity == repo.SeverityError {
			mark = "✗"
		}
		if p.Commit != "" && p.Commit != "HEAD" {
			where += " @ " + p.Commit
		}
		if where != "" {
			where += ": "
		}
		fmt.Printf("  %s %s%s\n    fix: %s\n", mark, where, p.Message, p.Fix)
	}
}

func cmdSync(args []string) error {
	fs := newFlags("sync")
	identityFlag(fs)
	if pos := parseFlags(fs, args); len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}
	if err := ensureGroups(r); err != nil {
		return err
	}
	changed, err := r.SyncAttributes()
	if err != nil {
		return err
	}
	if err := syncAndStage(r); err != nil {
		return err
	}
	if changed {
		step("updated .gitattributes from .gitencrypt")
	} else {
		step(".gitattributes already up to date")
	}
	if err := writeAgentNotes(r, true); err != nil {
		return err
	}
	if err := stageUntrackedSecrets(r); err != nil {
		return err
	}
	secrets, _ := r.TrackedSecrets()
	step("encrypted files: %s", strings.Join(secrets, ", "))
	return nil
}
