package main

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/pem"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/crypto/ssh"
)

// End-to-end scenarios live in testdata/script/*.txtar. Each one is a terminal
// session: `exec kis …` / `exec git …` with assertions on output and files.
// The test binary doubles as the kis executable, so git's filters and hooks
// call back into the code under test. See testdata/script/README.md.

var update = flag.Bool("update", false, "rewrite expected output embedded in scripts")

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"kis": main})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		Setup:               setupScript,
		Cmds:                scriptCmds,
		Condition:           scriptCondition,
		UpdateScripts:       *update,
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
	})
}

// scriptState is per-script state shared by the custom commands.
type scriptState struct {
	mu        sync.Mutex
	snapshots map[string]map[string]string
	github    map[string]string // user -> authorized_keys content
}

type stateKey struct{}

func state(ts *testscript.TestScript) *scriptState {
	return ts.Value(stateKey{}).(*scriptState)
}

func setupScript(env *testscript.Env) error {
	st := &scriptState{snapshots: map[string]map[string]string{}, github: map[string]string{}}
	env.Values[stateKey{}] = st

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		keys, ok := st.github[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".keys")]
		st.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, keys)
	}))
	env.Defer(srv.Close)

	remote := filepath.Join(env.WorkDir, "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		return fmt.Errorf("creating remote: %v\n%s", err, out)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	env.Setenv("REAL_GIT", realGit)
	env.Setenv("REMOTE", remote)
	env.Setenv("KIS_GITHUB_KEYS_URL", srv.URL+"/%s.keys")
	env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env.Setenv("NO_COLOR", "1")
	return setPerson(env.WorkDir, env.Setenv, "owner")
}

// setPerson points HOME, git identity and key locations at $WORK/home/NAME.
func setPerson(work string, setenv func(k, v string), name string) error {
	home := filepath.Join(work, "home", name)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		return err
	}
	gitconfig := fmt.Sprintf("[user]\n\tname = %s\n\temail = %s@example.com\n[init]\n\tdefaultBranch = main\n[advice]\n\tdetachedHead = false\n[pull]\n\trebase = false\n", name, name)
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		return err
	}
	setenv("HOME", home)
	setenv("USERPROFILE", home)
	setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	setenv("APPDATA", filepath.Join(home, "AppData"))
	setenv("PERSON", name)
	// Where `kis keygen` puts this person's key on the current OS.
	key := filepath.Join(home, ".config", "kis", "identity")
	if runtime.GOOS == "windows" {
		key = filepath.Join(home, "AppData", "kis", "identity")
	}
	setenv("KIS_KEY", key)
	return nil
}

var scriptCmds = map[string]func(ts *testscript.TestScript, neg bool, args []string){
	// as NAME: act as another person (own HOME, keys and git identity).
	"as": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 1 {
			ts.Fatalf("usage: as NAME")
		}
		ts.Check(setPerson(ts.Getenv("WORK"), ts.Setenv, args[0]))
	},

	// encrypted FILE...: the working copy holds kis ciphertext.
	"encrypted": func(ts *testscript.TestScript, neg bool, args []string) {
		for _, f := range args {
			data, err := os.ReadFile(ts.MkAbs(f))
			ts.Check(err)
			if crypt.IsEncrypted(data) == neg {
				ts.Fatalf("%s: expected %s working copy, got %q", f, want(neg), preview(data))
			}
		}
	},

	// stored REV FILE...: the blob git stores at REV (":" = index, HEAD, a
	// branch…) is encrypted. Negate to require plaintext.
	"stored": func(ts *testscript.TestScript, neg bool, args []string) {
		if len(args) < 2 {
			ts.Fatalf("usage: stored REV FILE...")
		}
		for _, f := range args[1:] {
			spec := args[0] + ":" + f
			if args[0] == ":" {
				spec = ":" + f
			}
			out, err := exec.Command("git", "-C", ts.MkAbs("."), "cat-file", "blob", spec).Output()
			if err != nil {
				ts.Fatalf("git cat-file %s: %v", spec, err)
			}
			if crypt.IsEncrypted(out) == neg {
				ts.Fatalf("%s: expected %s, got %q", spec, want(neg), preview(out))
			}
		}
	},

	// group REV FILE GROUP: the blob at REV (":" = index) is encrypted for GROUP.
	"group": func(ts *testscript.TestScript, neg bool, args []string) {
		if len(args) != 3 {
			ts.Fatalf("usage: group REV FILE GROUP")
		}
		spec := args[0] + ":" + args[1]
		if args[0] == ":" {
			spec = ":" + args[1]
		}
		out, err := exec.Command("git", "-C", ts.MkAbs("."), "cat-file", "blob", spec).Output()
		if err != nil {
			ts.Fatalf("git cat-file %s: %v", spec, err)
		}
		g, ok := crypt.GroupOf(out)
		if !ok {
			ts.Fatalf("%s is not encrypted: %q", spec, preview(out))
		}
		if (g == args[2]) == neg {
			ts.Fatalf("%s is encrypted for group %q, want %s%q", spec, g, map[bool]string{true: "not ", false: ""}[neg], args[2])
		}
	},

	// attr FILE ATTR VALUE: git check-attr resolves ATTR to VALUE.
	"attr": func(ts *testscript.TestScript, neg bool, args []string) {
		if len(args) != 3 {
			ts.Fatalf("usage: attr FILE ATTR VALUE")
		}
		out, err := exec.Command("git", "-C", ts.MkAbs("."), "check-attr", args[1], "--", args[0]).Output()
		ts.Check(err)
		got := strings.TrimSpace(string(out))
		got = got[strings.LastIndex(got, ": ")+2:]
		if (got == args[2]) == neg {
			ts.Fatalf("%s: %s is %q, want %s%q", args[0], args[1], got, map[bool]string{true: "not ", false: ""}[neg], args[2])
		}
	},

	// snapshot NAME: remember every file in the current repo (working tree
	// plus .git/config, .git/hooks and .git/kis).
	"snapshot": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 1 {
			ts.Fatalf("usage: snapshot NAME")
		}
		st := state(ts)
		st.mu.Lock()
		defer st.mu.Unlock()
		st.snapshots[args[0]] = treeHashes(ts, ts.MkAbs("."))
	},

	// changed NAME [+added] [~modified] [-deleted]...: exactly these files
	// changed since `snapshot NAME`. Paths may use path.Match globs.
	"changed": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) < 1 {
			ts.Fatalf("usage: changed NAME [+added|~modified|-deleted]...")
		}
		st := state(ts)
		st.mu.Lock()
		before, ok := st.snapshots[args[0]]
		st.mu.Unlock()
		if !ok {
			ts.Fatalf("no snapshot %q", args[0])
		}
		diff := diffTrees(before, treeHashes(ts, ts.MkAbs(".")))
		var problems []string
		for _, exp := range args[1:] {
			if len(exp) < 2 || !strings.ContainsRune("+~-", rune(exp[0])) {
				ts.Fatalf("bad expectation %q: prefix with + ~ or -", exp)
			}
			matched := false
			for i, d := range diff {
				if d != "" && d[0] == exp[0] {
					if ok, _ := path.Match(exp[1:], d[1:]); ok {
						diff[i], matched = "", true
					}
				}
			}
			if !matched {
				problems = append(problems, "expected but did not happen: "+exp)
			}
		}
		for _, d := range diff {
			if d != "" {
				problems = append(problems, "unexpected change: "+d)
			}
		}
		if len(problems) > 0 {
			ts.Fatalf("files changed since %s:\n\t%s", args[0], strings.Join(problems, "\n\t"))
		}
	},

	// github USER FILE: serve FILE's content as https://github.com/USER.keys.
	"github": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 2 {
			ts.Fatalf("usage: github USER FILE")
		}
		st := state(ts)
		st.mu.Lock()
		defer st.mu.Unlock()
		st.github[args[0]] = ts.ReadFile(args[1])
	},

	// sshkey [-rsa] [-passphrase P] COMMENT PATH: write an OpenSSH key pair
	// (PATH and PATH.pub) without depending on ssh-keygen being installed.
	"sshkey": func(ts *testscript.TestScript, neg bool, args []string) {
		rsaKey, passphrase := false, ""
		for len(args) > 0 && strings.HasPrefix(args[0], "-") {
			switch args[0] {
			case "-rsa":
				rsaKey, args = true, args[1:]
			case "-passphrase":
				if len(args) < 2 {
					ts.Fatalf("-passphrase needs a value")
				}
				passphrase, args = args[1], args[2:]
			default:
				ts.Fatalf("unknown flag %s", args[0])
			}
		}
		if neg || len(args) != 2 {
			ts.Fatalf("usage: sshkey [-rsa] [-passphrase P] COMMENT PATH")
		}
		comment, path := args[0], ts.MkAbs(args[1])
		var priv crypto.Signer
		var err error
		if rsaKey {
			priv, err = rsa.GenerateKey(rand.Reader, 2048)
		} else {
			_, priv, err = ed25519.GenerateKey(rand.Reader)
		}
		ts.Check(err)
		var block *pem.Block
		if passphrase != "" {
			block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
		} else {
			block, err = ssh.MarshalPrivateKey(priv, comment)
		}
		ts.Check(err)
		pub, err := ssh.NewPublicKey(priv.Public())
		ts.Check(err)
		ts.Check(os.MkdirAll(filepath.Dir(path), 0o700))
		ts.Check(os.WriteFile(path, pem.EncodeToMemory(block), 0o600))
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + comment + "\n"
		ts.Check(os.WriteFile(path+".pub", []byte(line), 0o644))
	},

	// pubkey VAR: store the current person's first public key in $VAR.
	// binfile PATH: write a binary file holding every byte value, NULs and
	// CR/LF included, as a keystore would.
	"binfile": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 1 {
			ts.Fatalf("usage: binfile PATH")
		}
		data := make([]byte, 0, 512)
		for i := 0; i < 512; i++ {
			data = append(data, byte(i*7))
		}
		ts.Check(os.WriteFile(ts.MkAbs(args[0]), data, 0o644))
	},

	"pubkey": func(ts *testscript.TestScript, neg bool, args []string) {
		if neg || len(args) != 1 {
			ts.Fatalf("usage: pubkey VAR")
		}
		ts.Check(ts.Exec("kis", "pubkey"))
		for _, l := range strings.Split(ts.ReadFile("stdout"), "\n") {
			if l != "" && !strings.HasPrefix(l, "#") {
				ts.Setenv(args[0], l)
				return
			}
		}
		ts.Fatalf("kis pubkey printed no key")
	},
}

func scriptCondition(cond string) (bool, error) {
	return false, fmt.Errorf("unknown condition %q", cond)
}

func want(neg bool) string {
	if neg {
		return "plaintext"
	}
	return "encrypted"
}

func preview(b []byte) string {
	if len(b) > 40 {
		return string(b[:40]) + "…"
	}
	return string(b)
}

// treeHashes maps slash paths to content hashes for a repo's files.
func treeHashes(ts *testscript.TestScript, root string) map[string]string {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || strings.HasPrefix(rel, ".git/") {
				switch rel {
				case ".git", ".git/hooks", ".git/kis":
					return nil
				}
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, ".git/") && rel != ".git/config" &&
			!strings.HasPrefix(rel, ".git/hooks/") && !strings.HasPrefix(rel, ".git/kis/") {
			return nil
		}
		if strings.HasSuffix(rel, ".sample") && strings.HasPrefix(rel, ".git/hooks/") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	ts.Check(err)
	return out
}

func diffTrees(before, after map[string]string) []string {
	var d []string
	for p, h := range after {
		if old, ok := before[p]; !ok {
			d = append(d, "+"+p)
		} else if old != h {
			d = append(d, "~"+p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			d = append(d, "-"+p)
		}
	}
	sort.Strings(d)
	return d
}
