# CLAUDE.md

Guidance for coding agents working on the kis source code. Product context:
[docs/vision.md](docs/vision.md). Architecture: [docs/how-it-works.md](docs/how-it-works.md).

## Decisions

### Only suggest commands that are certain to work (2026-11-25)

kis messages (errors, `kis status` fixes, hints, docs) name a command for the
user to run **only when that command is 100% certain to work on the user's
machine**. In practice that means kis's own commands (`kis sync`, `kis unlock`,
`kis add …`) and plain git commands whose effect kis controls.

When the fix depends on the machine (package manager, admin rights, distro
release, OS version, corporate policy), the message states the problem and
what has to change, and names no command.

**Why:** a suggested command that fails, or that changes the system in a way
nobody decided (`sudo`, adding a third-party package repository), creates a new
problem instead of solving one. Coding agents make this worse: kis promises
that every error names its fix, so agents treat suggested commands as
instructions and run them.

**Example:** git older than 2.28. `kis init` and `kis unlock` say which version
was found, which is needed, why, and "Upgrade git on this machine". They used
to print `brew install git`, `winget …` or `sudo add-apt-repository …` by OS,
which needs tools or permissions that may be missing, and on most Linux distros
installs the same old git (`internal/gitx/gitx.go` `VersionError`; the
`old_git.txtar` scenario asserts no such command is printed).

**How to apply:** before adding a command to any message or doc, ask "does this
work on every machine that can reach this message?". If not, describe the
outcome needed ("upgrade git to 2.28 or newer") instead of a command.
