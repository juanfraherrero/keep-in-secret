// Package gitx runs git commands.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Run executes git in dir and returns trimmed stdout.
func Run(dir string, args ...string) (string, error) {
	out, err := RunInput(dir, nil, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// RunInput executes git with stdin and returns raw stdout.
func RunInput(dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()+" "+err.Error()))
	}
	return stdout.Bytes(), nil
}

// SplitZ splits NUL-separated git output.
func SplitZ(b []byte) []string {
	parts := strings.Split(string(b), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// MinVersion is the oldest git kis supports. Older releases resolve negated
// .gitattributes patterns differently, which could encrypt the wrong files.
var MinVersion = [3]int{2, 28, 0}

// ParseVersion reads `git version` output such as "git version 2.39.5",
// "git version 2.39.5 (Apple Git-154)" or "git version 2.45.1.windows.1".
func ParseVersion(out string) ([3]int, error) {
	var v [3]int
	f := strings.Fields(out)
	if len(f) < 3 || f[0] != "git" || f[1] != "version" {
		return v, fmt.Errorf("unexpected git version output %q", out)
	}
	parts := strings.Split(f[2], ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		digits := parts[i] // keep leading digits: "0-rc1" -> "0"
		if j := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
			digits = digits[:j]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return v, fmt.Errorf("unexpected git version %q", f[2])
		}
		v[i] = n
	}
	return v, nil
}

// Version returns the installed git version.
func Version() ([3]int, error) {
	out, err := Run(".", "version")
	if err != nil {
		return [3]int{}, err
	}
	return ParseVersion(out)
}

// Supported reports whether v is at least MinVersion.
func Supported(v [3]int) bool {
	for i := range v {
		if v[i] != MinVersion[i] {
			return v[i] > MinVersion[i]
		}
	}
	return true
}

// CheckVersion fails when the installed git is older than MinVersion.
func CheckVersion() error {
	v, err := Version()
	if err != nil {
		return err
	}
	return VersionError(v)
}

// VersionError explains why v is unsupported, or returns nil when it is
// supported. It deliberately names no upgrade command: how to upgrade git
// depends on the machine (package manager, admin rights, distro release), and
// a command that may not work there must not be suggested (see CLAUDE.md).
func VersionError(v [3]int) error {
	if Supported(v) {
		return nil
	}
	return fmt.Errorf("kis needs git %d.%d or newer (found %d.%d.%d).\n"+
		"  Older git resolves `!` patterns in .gitattributes differently, which could encrypt the wrong files.\n"+
		"  Upgrade git on this machine, then run this command again.",
		MinVersion[0], MinVersion[1], v[0], v[1], v[2])
}
