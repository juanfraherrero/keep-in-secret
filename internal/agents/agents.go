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
- ~.env*.local~ files are local: never commit them; don't remove them from
  ~.gitignore~. They hold one developer's values, so kis doesn't encrypt them
  and blocks committing them in plaintext.
- Before every push run ~kis status --check~; for a pull request,
  ~kis status --check --since origin/main~ (or the PR's base branch). ~--json~
  gives machine-readable output. It exits 1 and prints the fix if any secret
  would leave this machine in plaintext.
- To encrypt another file, add its path to ~.gitencrypt~ and run ~kis sync~.
  Common secret files per stack: [gitencrypt-patterns.md](https://github.com/juanfraherrero/keep-in-secret/blob/main/docs/gitencrypt-patterns.md).
  Ask the developer when unsure: encrypting a file CI reads in plaintext breaks CI.
  Never edit the kis block in ~.gitattributes~ by hand.
- A secret file that is binary and starts with ~KIS~ means this clone is locked,
  or isn't in that file's group (~.gitencrypt~ lines ending in ~@GROUP~). That is
  expected: leave the file alone. ~kis unlock~ needs the developer's private key,
  or on CI and cloud agents the ~KIS_PRIVATE_KEY~ secret (~kis add NAME --new-key~).
- In a fresh clone, run ~kis unlock~ first, even without a key: it sets git up
  so secret files can't be committed in plaintext. If it says you have no
  access, leave secret files alone and ask the developer.
- Run ~kis add NAME --new-key~ only piped straight into the CI or agent
  platform's secret store: its stdout is the private key alone, so you never
  read it. If you can't pipe it there, ask the developer to run it.
- Every kis command picks that key the same way, first match wins:
  ~--identity PATH~, then ~KIS_PRIVATE_KEY~ (the key itself; key files are
  ignored), then ~$KIS_IDENTITY~, ~$HOME/.config/kis/identity~,
  ~$HOME/.ssh/id_ed25519~, ~$HOME/.ssh/id_rsa~. ~kis pubkey~ shows which. If the key is elsewhere, pass
  ~--identity PATH~ (PATH is the developer's private key file: ask them, never
  guess or search for keys), or have them set ~KIS_IDENTITY=PATH~.
- Never run ~kis keygen --force~ yourself: it replaces the developer's key, and
  their repositories still only know the old one. Ask the developer.
- Never copy secret values into code, logs, commit messages, pull requests or
  chat. If a task seems to need one, refer to the variable by name, or ask the
  developer.
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
