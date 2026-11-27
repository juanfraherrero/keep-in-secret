package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

var assumeYes bool

var stdinReader = bufio.NewReader(os.Stdin)

func interactive() bool {
	return !assumeYes && hasTerminal()
}

// hasTerminal reports whether a person could be typing: false for coding
// agents and CI, whatever -y says.
func hasTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func readLine() string {
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

// ask prompts for a value, returning def when not interactive or left empty.
func ask(question, def string) string {
	if !interactive() {
		return def
	}
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", question)
	}
	if v := readLine(); v != "" {
		return v
	}
	return def
}

func confirm(question string, def bool) bool {
	if !interactive() {
		return def
	}
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	fmt.Fprintf(os.Stderr, "%s [%s]: ", question, hint)
	switch strings.ToLower(readLine()) {
	case "":
		return def
	case "y", "yes", "s", "si", "sí":
		return true
	}
	return false
}

// choose shows a numbered menu and returns the selected index.
func choose(question string, options []string, def int) int {
	if !interactive() {
		return def
	}
	fmt.Fprintln(os.Stderr, question)
	for i, o := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, o)
	}
	for {
		fmt.Fprintf(os.Stderr, "Choose [%d]: ", def+1)
		v := readLine()
		if v == "" {
			return def
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
	}
}

// passphrasePrompt reads an SSH key passphrase from the terminal.
func passphrasePrompt(path string) func() ([]byte, error) {
	return func() ([]byte, error) {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			tty = os.Stdin
		} else {
			defer tty.Close()
		}
		fmt.Fprintf(os.Stderr, "Passphrase for %s: ", path)
		defer fmt.Fprintln(os.Stderr)
		return term.ReadPassword(int(tty.Fd()))
	}
}

// msgOut is where step, warn and note write: stdout, unless a command's
// stdout carries data (kis add --new-key writes only the private key there,
// so it can be piped straight into a secret store).
var msgOut io.Writer = os.Stdout

func step(format string, a ...any) { fmt.Fprintf(msgOut, "  ✓ "+format+"\n", a...) }
func warn(format string, a ...any) { fmt.Fprintf(msgOut, "  ! "+format+"\n", a...) }
func note(format string, a ...any) { fmt.Fprintf(msgOut, "  → "+format+"\n", a...) }
