package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
	"github.com/juanfraherrero/keep-in-secret/internal/filter"
	"github.com/juanfraherrero/keep-in-secret/internal/repo"
)

// These commands are run by git through the filter and hook configuration.

var keySource = filter.Cached(func() (*crypt.Cipher, error) {
	r, err := repo.Open()
	if err != nil {
		return nil, err
	}
	return r.Cipher()
})

func argPath(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "<stdin>"
}

func cmdClean(args []string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	out, err := filter.Clean(data, argPath(args), keySource)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

func cmdSmudge(args []string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(filter.Smudge(data, argPath(args), keySource))
	return err
}

func cmdFilterProcess([]string) error {
	return filter.Process(os.Stdin, os.Stdout, keySource)
}

func cmdTextconv(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kis textconv FILE")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(filter.Smudge(data, args[0], keySource))
	return err
}

func cmdPrecommit([]string) error {
	r, err := repo.Open()
	if err != nil || !r.Initialized() {
		return nil
	}
	if !r.AttributesInSync() {
		fmt.Fprintln(os.Stderr, "kis: .gitencrypt changed but .gitattributes was not updated.\n     Run `kis sync`, then commit again.")
		return exitError{}
	}
	bad, err := r.StagedPlaintext()
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "kis: commit blocked, these files would be stored in PLAINTEXT:\n    %s\n", strings.Join(bad, "\n    "))
		fmt.Fprintln(os.Stderr, "     Run `kis sync` to encrypt them (or `kis unlock` first if this clone is locked).")
		return exitError{}
	}
	return nil
}

// cmdMerge is the git merge driver for encrypted files: kis merge %O %A %B %L %P.
// It decrypts base, ours and theirs, runs a normal text merge and writes the
// result back to %A encrypted, so conflict markers reach the working copy as
// plaintext (via smudge) while git's object store only holds ciphertext.
func cmdMerge(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("usage: kis merge BASE OURS THEIRS MARKER_SIZE PATH")
	}
	base, ours, theirs, markerSize, path := args[0], args[1], args[2], args[3], args[4]
	c, err := keySource()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kis: cannot merge %s: %v\n", path, err)
		return exitError{}
	}
	r, err := repo.Open()
	if err != nil {
		return err
	}
	tmp, err := r.TempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var plain [3]string
	for i, f := range []string{ours, base, theirs} {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if crypt.IsEncrypted(data) {
			if data, err = c.Decrypt(data); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		plain[i] = filepath.Join(tmp, []string{"ours", "base", "theirs"}[i])
		if err := os.WriteFile(plain[i], data, 0o600); err != nil {
			return err
		}
	}
	cmd := exec.Command("git", "merge-file", "--marker-size="+markerSize,
		"-L", "ours", "-L", "base", "-L", "theirs", plain[0], plain[1], plain[2])
	cmd.Stderr = os.Stderr
	mergeErr := cmd.Run()
	var exit *exec.ExitError
	if mergeErr != nil && !errors.As(mergeErr, &exit) {
		return mergeErr
	}
	merged, err := os.ReadFile(plain[0])
	if err != nil {
		return err
	}
	if err := os.WriteFile(ours, c.Encrypt(merged), 0o644); err != nil {
		return err
	}
	if mergeErr != nil {
		return exitError{} // conflicts left for the user
	}
	return nil
}
