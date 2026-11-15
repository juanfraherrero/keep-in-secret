package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hasHook reports whether a hook script already runs the kis check, whether
// kis is invoked by name or by absolute path.
func hasHook(script string) bool {
	return strings.Contains(script, "kis") && strings.Contains(script, " precommit")
}

// Binary returns how git should invoke kis: plain "kis" when that resolves to
// the running executable, otherwise its absolute path.
func Binary() string {
	exe, err := os.Executable()
	if err != nil {
		return "kis"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if lp, err := exec.LookPath("kis"); err == nil {
		if resolved, err := filepath.EvalSymlinks(lp); err == nil && resolved == exe {
			return "kis"
		}
	}
	return "'" + strings.ReplaceAll(filepath.ToSlash(exe), "'", `'\''`) + "'"
}

// ConfigureGit registers the kis filter and diff driver in .git/config.
func (r *Repo) ConfigureGit() error {
	bin := Binary()
	for _, kv := range [][2]string{
		{"filter.kis.process", bin + " filter-process"},
		{"filter.kis.clean", bin + " clean %f"},
		{"filter.kis.smudge", bin + " smudge %f"},
		{"filter.kis.required", "true"},
		{"diff.kis.textconv", bin + " textconv"},
	} {
		if _, err := r.Git("config", "--local", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) GitConfigured() bool {
	v, err := r.Git("config", "--local", "--get", "filter.kis.process")
	return err == nil && v != ""
}

func (r *Repo) hookPath() (string, error) {
	p, err := r.Git("rev-parse", "--git-path", "hooks/pre-commit")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Root, p)
	}
	return p, nil
}

func (r *Repo) HookInstalled() bool {
	p, err := r.hookPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(p)
	return err == nil && hasHook(string(data))
}

// InstallHook writes the pre-commit safety check. An existing hook is left
// alone and a warning describing the line to add is returned instead.
func (r *Repo) InstallHook() (warning string, err error) {
	p, err := r.hookPath()
	if err != nil {
		return "", err
	}
	line := Binary() + " precommit || exit 1"
	data, err := os.ReadFile(p)
	if err == nil {
		if hasHook(string(data)) {
			return "", nil
		}
		return fmt.Sprintf("%s already exists; add this line to it so plaintext secrets are blocked:\n    %s", p, line), nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	script := "#!/bin/sh\n# Installed by kis: blocks commits that would store secrets in plaintext.\n" + line + "\n"
	return "", os.WriteFile(p, []byte(script), 0o755)
}
