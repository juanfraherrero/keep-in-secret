// Command kis (keep-in-secret) transparently encrypts selected files in a git
// repository. Files matching .gitencrypt are encrypted when staged and
// decrypted on checkout, so secrets like .env can be committed and shared.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
)

// version is set by the release build (-X main.version); `go install …@v0.1.0`
// builds get it from the module version instead.
var version = "dev"

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

type command struct {
	name, usage, summary string
	help                 string // extra explanation shown by `kis NAME -h`
	run                  func(args []string) error
	hidden               bool
}

var commands []command

func init() {
	commands = []command{
		{name: "init", usage: "init [--identity PATH] [--no-agents] [-y]", summary: "Set up encryption in this repository", run: cmdInit},
		{name: "keygen", usage: "keygen [--force]", summary: "Create your personal key and print the public key to share", run: cmdKeygen},
		{name: "pubkey", usage: "pubkey [--identity PATH]", summary: "Print the public keys kis uses here, and where each comes from", run: cmdPubkey},
		{name: "add", usage: "add NAME [KEY|github:USER|FILE.pub|--new-key] [--group G] [--identity PATH]", summary: "Give a partner access", run: cmdAdd,
			help: "KEY is the partner's public key, in any of these forms:\n" +
				"  age1…              an age key; they create one with `kis keygen`\n" +
				"  \"ssh-ed25519 …\"    an SSH public key (ed25519 or RSA), quoted\n" +
				"  github:USER        every ed25519/RSA key on that GitHub account\n" +
				"  FILE.pub           a file with one or more public keys\n" +
				"Several KEYs can be given. Without KEY, NAME must already be a partner (use with --group).\n" +
				"--new-key creates the key pair instead, for CI and cloud agents.\n" +
				"--identity PATH is your own private key, used when --group creates a new group."},
		{name: "remove", usage: "remove NAME [--group G] [--no-rotate]", summary: "Revoke access and rotate the affected keys", run: cmdRemove},
		{name: "rotate", usage: "rotate [--group G]", summary: "Replace the key(s) and re-encrypt", run: cmdRotate},
		{name: "unlock", usage: "unlock [--identity PATH]", summary: "Get access after cloning: decrypts the files", run: cmdUnlock},
		{name: "lock", usage: "lock [--force]", summary: "Forget the key on this machine and re-encrypt the files", run: cmdLock},
		{name: "status", usage: "status [--check] [--json] [--since REV]", summary: "Show encryption state, partners and encrypted files", run: cmdStatus},
		{name: "sync", usage: "sync [--identity PATH]", summary: "Apply .gitencrypt changes to .gitattributes and re-encrypt", run: cmdSync},
		{name: "version", usage: "version", summary: "Print the kis version", run: func([]string) error { fmt.Println("kis", version); return nil }},
		// Invoked by git, not by people.
		{name: "clean", run: cmdClean, hidden: true},
		{name: "smudge", run: cmdSmudge, hidden: true},
		{name: "filter-process", run: cmdFilterProcess, hidden: true},
		{name: "textconv", run: cmdTextconv, hidden: true},
		{name: "precommit", run: cmdPrecommit, hidden: true},
		{name: "merge", run: cmdMerge, hidden: true},
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "kis (keep-in-secret): commit secrets to git, encrypted.\n\nUsage:\n")
	for _, c := range commands {
		if !c.hidden {
			fmt.Fprintf(os.Stderr, "  kis %-58s %s\n", c.usage, c.summary)
		}
	}
	fmt.Fprintf(os.Stderr, "\nList the files to encrypt in .gitencrypt (same syntax as .gitignore).\n")
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		os.Exit(2)
	}
	for _, c := range commands {
		if c.name == os.Args[1] {
			if err := c.run(os.Args[2:]); err != nil {
				var exit exitError
				if !errors.As(err, &exit) {
					fmt.Fprintf(os.Stderr, "kis: %v\n", err)
				}
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "kis: unknown command %q\n\n", os.Args[1])
	usage()
	os.Exit(2)
}

// exitError signals failure after the message was already printed.
type exitError struct{}

func (exitError) Error() string { return "exit" }

// parseFlags parses flags that may appear anywhere among positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		_ = fs.Parse(args)
		args = fs.Args()
		if len(args) == 0 {
			return pos
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// identityPath is the running command's --identity flag. Every command that
// needs your private key passes it to keys.Resolve, so they all agree on who
// you are.
var identityPath string

func identityFlag(fs *flag.FlagSet) {
	fs.StringVar(&identityPath, "identity", "", "your private key file (age or SSH); wins over KIS_PRIVATE_KEY and the default locations")
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("kis "+name, flag.ExitOnError)
	for _, c := range commands {
		if c.name == name {
			fs.Usage = func() {
				fmt.Fprintf(os.Stderr, "Usage: kis %s\n", c.usage)
				if c.help != "" {
					fmt.Fprintf(os.Stderr, "\n%s\n\n", c.help)
				}
				fs.PrintDefaults()
			}
		}
	}
	return fs
}
