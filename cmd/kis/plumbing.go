package main

import (
	"fmt"
	"io"
	"os"
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
		fmt.Fprintln(os.Stderr, "     Run `kis unlock` (or `kis sync`) and `git add` them again.")
		return exitError{}
	}
	return nil
}
