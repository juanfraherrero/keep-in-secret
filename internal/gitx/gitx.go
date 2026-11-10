// Package gitx runs git commands.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
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
