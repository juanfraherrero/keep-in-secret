// Package agents writes the kis section of AGENTS.md (and CLAUDE.md…), the
// files coding agents read before working in a repository. Without it, an
// agent trained to never commit .env tends to "fix" a kis setup by ignoring
// or deleting the encrypted files.
package agents

import "strings"

// FileName is always given a kis section; Optional files only when they exist.
const FileName = "AGENTS.md"

// Tools that read their own file instead of (or besides) AGENTS.md.
var Optional = []string{"CLAUDE.md", "GEMINI.md", ".github/copilot-instructions.md"}

const (
	beginMark = "<!-- >>> kis: managed by `kis init` / `kis sync`, edits inside are overwritten -->"
	endMark   = "<!-- <<< kis -->"
)

// Block is the managed section, markers included. (~ stands for a backtick.)
var Block = strings.ReplaceAll(beginMark+`
## Secrets are committed encrypted (kis)

This repository commits its secret files (~.env~ and everything listed in
~.gitencrypt~) **encrypted**, using [kis](https://github.com/juanfraherrero/keep-in-secret).
Git encrypts them on ~git add~ and decrypts them on checkout: the working copy
is plaintext, git history and the remote only hold ciphertext.

- Committing these files is safe and expected. Do not add them to ~.gitignore~,
  ~git rm~ them, or move their values elsewhere to "protect" them.
- Before every push or pull request run ~kis status --check~ (~--json~ for
  machine-readable output). It exits 1 and prints the fix if any secret would
  leave this machine in plaintext.
- To encrypt another file, add its path to ~.gitencrypt~ and run ~kis sync~.
  Never edit the kis block in ~.gitattributes~ by hand.
- A secret file that is binary and starts with ~KIS~ means this clone is locked:
  ~kis unlock~ needs the developer's private key, so ask them instead of working around it.
- Do not copy secret values into code, logs, commit messages, pull requests or
  chat unless the task requires it.
`, "~", "`") + endMark + "\n"

// Merge replaces the managed section in content, or appends it.
func Merge(content string) string {
	start := strings.Index(content, beginMark)
	if start >= 0 {
		if end := strings.Index(content[start:], endMark); end >= 0 {
			end += start + len(endMark)
			if end < len(content) && content[end] == '\n' {
				end++
			}
			return content[:start] + Block + content[end:]
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + Block
}

// Has reports whether content carries a kis section, of any version.
func Has(content string) bool { return strings.Contains(content, beginMark) }
