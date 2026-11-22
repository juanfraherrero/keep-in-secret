package repo

import (
	"fmt"
	"strings"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
	"github.com/juanfraherrero/keep-in-secret/internal/gitx"
)

// File states reported by Audit.
const (
	FileEncrypted  = "encrypted"       // ciphertext in the index and in HEAD
	FilePlainIndex = "plaintext-index" // staged in plaintext
	FilePlainHead  = "plaintext-head"  // HEAD commit holds plaintext
	FileUntracked  = "untracked"       // matches .gitencrypt, not added yet
	FileIgnored    = "ignored"         // matches .gitencrypt, hidden by .gitignore
)

// Problem severities: only errors make a repository unsafe to push.
const (
	SeverityError   = "error"   // pushing now would publish plaintext
	SeverityWarning = "warning" // this clone is not fully protected
)

type FileStatus struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

// Problem is one reason the repository is not safe to push, or a weaker
// warning about this clone. Fix is the command (or action) that resolves it.
type Problem struct {
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Message  string `json:"message"`
	Fix      string `json:"fix"`
}

type Audit struct {
	Files    []FileStatus `json:"files"`
	Problems []Problem    `json:"problems"`
}

// Safe reports whether pushing would publish no secret in plaintext.
func (a Audit) Safe() bool {
	for _, p := range a.Problems {
		if p.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Audit checks that every secret is ciphertext in the index and in HEAD and,
// when since is set, in every commit of since..HEAD (what a pull request or
// push would publish). It needs no key, so it works in CI and locked clones.
func (r *Repo) Audit(since string) (Audit, error) {
	var a Audit
	add := func(p Problem) { a.Problems = append(a.Problems, p) }

	if !r.AttributesInSync() {
		add(Problem{Severity: SeverityError, Path: ".gitattributes",
			Message: ".gitencrypt changed but .gitattributes was not updated, so new patterns are not encrypted yet",
			Fix:     "kis sync"})
	}
	secrets, err := r.TrackedSecrets()
	if err != nil {
		return a, err
	}
	hasHead := r.revExists("HEAD")
	for _, p := range secrets {
		state := FileEncrypted
		if blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", ":"+p); err == nil && !crypt.IsEncrypted(blob) {
			state = FilePlainIndex
			add(Problem{Severity: SeverityError, Path: p,
				Message: "staged in plaintext",
				Fix:     "kis sync (or kis unlock first if this clone is locked)"})
		} else if hasHead {
			if blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", "HEAD:"+p); err == nil && !crypt.IsEncrypted(blob) {
				state = FilePlainHead
				add(Problem{Severity: SeverityError, Path: p, Commit: "HEAD",
					Message: "the last commit stores it in plaintext",
					Fix:     "commit the encrypted version (git commit) and rotate the secret; older commits still contain it"})
			}
		}
		a.Files = append(a.Files, FileStatus{p, state})
	}

	if since != "" {
		leaks, err := r.plaintextCommits(since)
		if err != nil {
			return a, err
		}
		for _, l := range leaks {
			add(Problem{Severity: SeverityError, Path: l[1], Commit: l[0],
				Message: "a commit to be pushed stores it in plaintext",
				Fix:     "rewrite that commit (git rebase -i) so it never holds the plaintext, then rotate the secret if it was already pushed"})
		}
	}

	addable, ignored, err := r.UntrackedSecrets()
	if err != nil {
		return a, err
	}
	for _, p := range addable {
		a.Files = append(a.Files, FileStatus{p, FileUntracked})
	}
	for _, p := range ignored {
		a.Files = append(a.Files, FileStatus{p, FileIgnored})
	}
	return a, nil
}

func (r *Repo) revExists(rev string) bool {
	_, err := r.Git("rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}

// plaintextCommits returns {commit, path} for every secret that a commit in
// since..HEAD adds or modifies in plaintext. Attributes come from the index,
// so files later dropped from .gitencrypt are not reported.
func (r *Repo) plaintextCommits(since string) ([][2]string, error) {
	if !r.revExists(since) {
		return nil, fmt.Errorf("unknown revision %s (in CI, fetch full history: actions/checkout with fetch-depth: 0)", since)
	}
	out, err := r.Git("rev-list", since+"..HEAD")
	if err != nil || out == "" {
		return nil, err
	}
	var leaks [][2]string
	for _, c := range strings.Fields(out) {
		changed, err := gitx.RunInput(r.Root, nil, "diff-tree", "-r", "--root", "--no-commit-id", "--name-only", "-z", "--diff-filter=ACMR", c)
		if err != nil {
			return nil, err
		}
		secrets, err := r.filterMatches(gitx.SplitZ(changed), true)
		if err != nil {
			return nil, err
		}
		for _, p := range secrets {
			blob, err := gitx.RunInput(r.Root, nil, "cat-file", "blob", c+":"+p)
			if err == nil && !crypt.IsEncrypted(blob) {
				leaks = append(leaks, [2]string{c[:12], p})
			}
		}
	}
	return leaks, nil
}
