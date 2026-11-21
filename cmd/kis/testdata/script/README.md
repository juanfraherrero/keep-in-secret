# kis end-to-end scenarios

Each `.txtar` file is one user journey, written like a terminal session. They run
with `go test ./cmd/kis` (or `make test-e2e`); the test binary doubles as `kis`, so
git's filters and hooks call back into the code under test.

```
cd app                       # files below "-- app/... --" are pre-created
exec kis init -y --name owner
stdout '✓ created \.gitencrypt'
changed before +.gitencrypt ~.gitignore
```

Lines are commands; `!` in front expects failure (or negates an assertion);
`[cond]` guards a line. Files after `-- name --` are written into `$WORK` first.

## Environment

| Variable | Meaning |
|---|---|
| `$WORK` | Scratch directory for this script |
| `$REMOTE` | A bare repo every person can push to and pull from (plays GitHub) |
| `$HOME` | The current person's home: `$WORK/home/<person>` (own keys and git identity) |
| `$PERSON` | The current person's name (starts as `owner`) |
| `$KIS_KEY` | Where `kis keygen` stores this person's key on the current OS |
| `$REAL_GIT` | The real git binary (for fake-git wrappers) |

## Commands added by kis

| Command | Checks / does |
|---|---|
| `as NAME` | Become another person: separate `$HOME`, `~/.ssh`, kis key and git identity |
| `encrypted FILE...` | The working copy is kis ciphertext (`! encrypted` = plaintext) |
| `stored REV FILE...` | The blob git stores at REV is encrypted. `:` is the index: `stored : .env`, `stored HEAD .env` |
| `attr FILE ATTR VALUE` | `git check-attr` resolves ATTR (e.g. `filter`) to VALUE (`kis`, `unspecified`) |
| `snapshot NAME` | Remember every file in the repo: working tree plus `.git/config`, `.git/hooks/`, `.git/kis/` |
| `changed NAME +a ~m -d` | **Exactly** these files were added / modified / deleted since `snapshot NAME` (globs allowed) |
| `github USER FILE` | Serve FILE as `https://github.com/USER.keys` (for `kis add NAME github:USER`) |
| `pubkey VAR` | Run `kis pubkey` and store the current person's first public key in `$VAR` |

Conditions: `[ssh-keygen]` (ssh-keygen available), plus testscript's built-ins
(`[unix]`, `[windows]`, `[darwin]`, `[exec:prog]`, …).

Standard testscript commands (`exec`, `stdout`, `stderr`, `cmp`, `grep`, `exists`,
`cp`, `env`, `cd`, `ttyin`, `ttyout`, …) are documented at
https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript.

## Tips

- `go test ./cmd/kis -run TestScripts/onboard_partner -v` runs one scenario verbosely.
- `go test ./cmd/kis -run TestScripts/X -testwork` keeps `$WORK` for inspection.
- `go test ./cmd/kis -update` rewrites `cmp stdout <file>` expectations embedded in a script.
