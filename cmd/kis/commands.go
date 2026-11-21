package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"filippo.io/age"
	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
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
	if !r.Initialized() {
		return nil, repo.ErrNotInitialized
	}
	return r, nil
}

// ---- init ----

func cmdInit(args []string) error {
	fs := newFlags("init")
	useSSH := fs.Bool("ssh", false, "use your SSH key (~/.ssh/id_ed25519) and make SSH the default for partners")
	identity := fs.String("identity", "", "private key file to use (age or SSH)")
	name := fs.String("name", "", "your name in the recipients list")
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

	fmt.Println("Setting up kis encryption in", r.Root)
	keyPath, pub, keyType, err := chooseIdentity(*useSSH, *identity)
	if err != nil {
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
	if err := r.SaveMasterKey(master); err != nil {
		return err
	}
	if err := r.WrapFor(me, master, []string{pub}); err != nil {
		return err
	}
	if err := r.SaveRecipients(repo.Recipients{me: {pub}}); err != nil {
		return err
	}
	if err := r.SaveConfig(repo.Config{Version: 1, DefaultKey: keyType}); err != nil {
		return err
	}
	step("created repository key, encrypted for %s (%s key %s)", me, keyType, keyPath)

	if err := wireGit(r); err != nil {
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

	if err := stageUntrackedSecrets(r); err != nil {
		return err
	}
	if len(plainInHistory) > 0 {
		warn("these were already committed in PLAINTEXT before: %s", strings.Join(plainInHistory, ", "))
		warn("they are encrypted from now on, but old commits still contain them: rotate those secrets")
	}

	cfg := repo.Config{DefaultKey: keyType}
	if interactive() && confirm("Add partners now?", false) {
		for {
			pname := ask("Partner name (empty to finish)", "")
			if pname == "" {
				break
			}
			karg := ask("Their public key, github:USER, or path to a .pub file", "")
			if err := addRecipient(r, pname, []string{karg}, false); err != nil {
				warn("%v", err)
			}
		}
	}

	if secrets, _ := r.TrackedSecrets(); len(secrets) > 0 {
		step("encrypted files: %s", strings.Join(secrets, ", "))
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Commit and push:   git commit -m \"Set up kis encryption\" && git push")
	fmt.Println("  2. Ask each partner to install kis and send you their public key:")
	printPartnerKeyInstructions(cfg)
	fmt.Println("  3. Give them access:  kis add NAME KEY   (then commit and push)")
	fmt.Println("  4. They clone and run: kis unlock")
	return nil
}

func chooseIdentity(useSSH bool, explicit string) (path, pub, keyType string, err error) {
	if explicit != "" {
		pub, err = keys.PublicKey(explicit)
		return explicit, pub, keys.RecipientType(pub), err
	}
	cands := keys.Candidates()
	if useSSH {
		for _, c := range cands {
			if c.Type == keys.TypeSSH {
				pub, err = keys.PublicKey(c.Path)
				if err == nil {
					if _, perr := keys.ParseRecipient(pub); perr != nil {
						err = perr
					}
				}
				return c.Path, pub, keys.TypeSSH, err
			}
		}
		return "", "", "", errors.New("no SSH key found in ~/.ssh: create one with `ssh-keygen -t ed25519`, or run `kis init` without --ssh")
	}

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
		c := cands[pick]
		pub, err = keys.PublicKey(c.Path)
		if err == nil {
			_, err = keys.ParseRecipient(pub)
		}
		return c.Path, pub, c.Type, err
	}
	path = keys.DefaultAgeIdentityPath()
	if _, statErr := os.Stat(path); statErr == nil {
		pub, err = keys.PublicKey(path)
		return path, pub, keys.TypeAge, err
	}
	pub, err = keys.Generate(path)
	if err == nil {
		step("created your key at %s (keep it private, back it up)", path)
	}
	return path, pub, keys.TypeAge, err
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

func printPartnerKeyInstructions(cfg repo.Config) {
	if cfg.DefaultKey == keys.TypeSSH {
		fmt.Println("       This repo uses SSH keys. They send you ~/.ssh/id_ed25519.pub")
		fmt.Printf("       (Windows: %s), or you run: kis add NAME github:THEIR_USER\n", `%USERPROFILE%\.ssh\id_ed25519.pub`)
		fmt.Println("       No SSH key yet? `ssh-keygen -t ed25519`, or `kis keygen` for a kis key.")
		return
	}
	fmt.Println("       They run `kis keygen` and send you the age1… line it prints.")
	fmt.Println("       (Key is stored in ~/.config/kis/identity on macOS/Linux, %APPDATA%\\kis\\identity on Windows.)")
	fmt.Println("       Already have an SSH key? `kis add NAME github:THEIR_USER` works too.")
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
	if len(leaving) > 0 {
		c, err := r.Cipher()
		if err != nil {
			return fmt.Errorf("%s would stop being encrypted, which needs the plaintext: %w", strings.Join(leaving, ", "), err)
		}
		// The filter no longer applies to these, so decrypt any working copy
		// still holding ciphertext before it is re-added as plaintext.
		for _, p := range leaving {
			path := filepath.Join(r.Root, filepath.FromSlash(p))
			data, err := os.ReadFile(path)
			if err != nil || !crypt.IsEncrypted(data) {
				continue
			}
			plain, err := c.Decrypt(data)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			if err := os.WriteFile(path, plain, 0o644); err != nil {
				return err
			}
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

func cmdPubkey([]string) error {
	cands := keys.Candidates()
	if len(cands) == 0 {
		return errors.New("no keys found: run `kis keygen`")
	}
	for _, c := range cands {
		pub, err := keys.PublicKey(c.Path)
		if err != nil {
			warn("%s: %v", c.Path, err)
			continue
		}
		fmt.Printf("# %s (%s)\n%s\n", c.Path, c.Type, pub)
	}
	return nil
}

// ---- add / remove / rotate ----

func cmdAdd(args []string) error {
	fs := newFlags("add")
	force := fs.Bool("force", false, "replace the keys of an existing partner")
	pos := parseFlags(fs, args)
	if len(pos) < 2 {
		fs.Usage()
		return exitError{}
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}
	if err := addRecipient(r, pos[0], pos[1:], *force); err != nil {
		return err
	}
	fmt.Printf("\nNow commit and push, then ask %s to run:  kis unlock\n", pos[0])
	return nil
}

func addRecipient(r *repo.Repo, name string, keyArgs []string, force bool) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name %q: use letters, digits, '.', '_' or '-'", name)
	}
	master, err := r.MasterKey()
	if err != nil {
		return err
	}
	var pubs []string
	for _, a := range keyArgs {
		p, err := keys.ResolveRecipients(a)
		if err != nil {
			return err
		}
		pubs = append(pubs, p...)
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	if _, exists := rs[name]; exists && !force {
		return fmt.Errorf("%s already has access; use --force to replace their keys", name)
	}
	if err := r.WrapFor(name, master, pubs); err != nil {
		return err
	}
	rs[name] = pubs
	if err := r.SaveRecipients(rs); err != nil {
		return err
	}
	if _, err := r.Git("add", "--", repo.Dir); err != nil {
		return err
	}
	step("added %s (%d key(s), %s)", name, len(pubs), keys.RecipientType(pubs[0]))
	return nil
}

func cmdRemove(args []string) error {
	fs := newFlags("remove")
	noRotate := fs.Bool("no-rotate", false, "skip rotating the repository key (they could still decrypt future changes)")
	force := fs.Bool("force", false, "rotate even with uncommitted changes to encrypted files")
	pos := parseFlags(fs, args)
	if len(pos) != 1 {
		fs.Usage()
		return exitError{}
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}
	if _, err := r.MasterKey(); err != nil {
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
	if len(rs) == 1 {
		return errors.New("cannot remove the last partner: nobody could unlock the repository")
	}
	delete(rs, name)
	if !*noRotate {
		if err := rotate(r, rs, *force); err != nil {
			return err
		}
	}
	if err := r.RemoveKeyFile(name); err != nil {
		return err
	}
	if err := r.SaveRecipients(rs); err != nil {
		return err
	}
	if _, err := r.Git("add", "--", repo.Dir); err != nil {
		return err
	}
	step("removed %s", name)
	warn("%s can still read every secret in past commits: change the real credentials (DB passwords, API keys)", name)
	fmt.Println("\nCommit and push. Partners who pull will get the new key automatically.")
	return nil
}

func cmdRotate(args []string) error {
	fs := newFlags("rotate")
	force := fs.Bool("force", false, "rotate even with uncommitted changes to encrypted files")
	parseFlags(fs, args)
	r, err := openInitialized()
	if err != nil {
		return err
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	if err := rotate(r, rs, *force); err != nil {
		return err
	}
	fmt.Println("\nCommit and push. Partners who pull will get the new key automatically.")
	return nil
}

// rotate replaces the master key, wraps it for rs and re-encrypts tracked secrets.
func rotate(r *repo.Repo, rs repo.Recipients, force bool) error {
	if _, err := r.MasterKey(); err != nil {
		return err
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return err
	}
	if dirty, err := r.Dirty(secrets); err != nil {
		return err
	} else if len(dirty) > 0 && !force {
		return fmt.Errorf("uncommitted changes in %s: commit them first or use --force", strings.Join(dirty, ", "))
	}
	if _, err := r.Refresh(secrets, false); err != nil { // working copies must be plaintext
		return err
	}
	master := crypt.NewKey()
	for _, n := range rs.Names() {
		if err := r.WrapFor(n, master, rs[n]); err != nil {
			return err
		}
	}
	if err := r.SaveMasterKey(master); err != nil {
		return err
	}
	if err := r.Renormalize(secrets); err != nil {
		return err
	}
	if _, err := r.Git("add", "--", repo.Dir); err != nil {
		return err
	}
	step("new repository key for %s; re-encrypted %d file(s)", strings.Join(rs.Names(), ", "), len(secrets))
	return nil
}

// ---- unlock / lock ----

func cmdUnlock(args []string) error {
	fs := newFlags("unlock")
	identity := fs.String("identity", "", "private key file to use (age or SSH)")
	parseFlags(fs, args)
	if err := gitx.CheckVersion(); err != nil {
		return err
	}
	r, err := openInitialized()
	if err != nil {
		return err
	}

	var paths []string
	if *identity != "" {
		paths = []string{*identity}
	} else {
		for _, c := range keys.Candidates() {
			paths = append(paths, c.Path)
		}
	}
	if len(paths) == 0 {
		return errors.New("no key found on this machine.\n  Run `kis keygen`, send the printed public key to someone with access,\n  and run `kis unlock` again once they have added you")
	}
	var ids []age.Identity
	for _, p := range paths {
		id, err := keys.LoadIdentity(p, passphrasePrompt(p))
		if err != nil {
			warn("skipping %v", err)
			continue
		}
		ids = append(ids, id)
	}
	master, name, err := r.UnwrapMasterKey(ids)
	if err != nil && !errors.Is(err, repo.ErrNoAccess) {
		if strings.Contains(err.Error(), "decryption password incorrect") {
			return errors.New("incorrect passphrase for your SSH key; nothing was unlocked")
		}
		return err
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kis: none of your keys has access to this repository yet.")
		fmt.Fprintln(os.Stderr, "Send one of these public keys to someone with access and ask them to run `kis add YOUR_NAME KEY`:")
		for _, p := range paths {
			if pub, err := keys.PublicKey(p); err == nil {
				fmt.Fprintf(os.Stderr, "    %s\n", pub)
			}
		}
		return exitError{}
	}
	if err := r.SaveMasterKey(master); err != nil {
		return err
	}
	if err := wireGit(r); err != nil {
		return err
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return err
	}
	n, err := r.Refresh(secrets, false)
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
	step("unlocked as %s; decrypted %d file(s)", name, n)
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
	if err := r.ForgetMasterKey(); err != nil {
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

func cmdStatus([]string) error {
	r, err := openInitialized()
	if err != nil {
		return err
	}
	cfg, err := r.LoadConfig()
	if err != nil {
		return err
	}
	rs, err := r.LoadRecipients()
	if err != nil {
		return err
	}
	state := "unlocked"
	if _, err := r.MasterKey(); err != nil {
		state = "LOCKED (run `kis unlock`)"
	}
	yesNo := func(ok bool, fix string) string {
		if ok {
			return "ok"
		}
		return "MISSING (run `" + fix + "`)"
	}
	fmt.Printf("repository      %s\n", r.Root)
	if v, err := gitx.Version(); err == nil {
		note := "ok"
		if !gitx.Supported(v) {
			note = fmt.Sprintf("TOO OLD: kis needs %d.%d+ (run `kis unlock` for upgrade steps)", gitx.MinVersion[0], gitx.MinVersion[1])
		}
		fmt.Printf("git             %d.%d.%d %s\n", v[0], v[1], v[2], note)
	}
	fmt.Printf("state           %s\n", state)
	fmt.Printf("git filters     %s\n", yesNo(r.GitConfigured(), "kis unlock"))
	fmt.Printf("pre-commit      %s\n", yesNo(r.HookInstalled(), "kis unlock"))
	sync := "ok"
	if !r.AttributesInSync() {
		sync = "OUT OF DATE (run `kis sync`)"
	}
	fmt.Printf(".gitattributes  %s\n", sync)
	fmt.Printf("default key     %s\n", cfg.DefaultKey)
	fmt.Println("partners")
	for _, n := range rs.Names() {
		fmt.Printf("  %-14s %d %s key(s)\n", n, len(rs[n]), keys.RecipientType(rs[n][0]))
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return err
	}
	fmt.Println("encrypted files")
	if len(secrets) == 0 {
		fmt.Println("  (none tracked yet)")
	}
	for _, p := range secrets {
		s := "encrypted in git"
		if blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", ":"+p); err == nil && !crypt.IsEncrypted(blob) {
			s = "PLAINTEXT in index (run `kis sync`)"
		}
		fmt.Printf("  %-30s %s\n", p, s)
	}
	addable, ignored, err := r.UntrackedSecrets()
	if err != nil {
		return err
	}
	for _, p := range addable {
		fmt.Printf("  %-30s untracked (git add it to share)\n", p)
	}
	for _, p := range ignored {
		fmt.Printf("  %-30s ignored by .gitignore (won't be shared)\n", p)
	}
	return nil
}

func cmdSync([]string) error {
	r, err := openInitialized()
	if err != nil {
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
	if err := stageUntrackedSecrets(r); err != nil {
		return err
	}
	secrets, _ := r.TrackedSecrets()
	step("encrypted files: %s", strings.Join(secrets, ", "))
	return nil
}
