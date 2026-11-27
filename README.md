# kis: keep-in-secret

Commit your `.env` files to git, **encrypted**, and share them with your team.

List the files in `.gitencrypt` (same syntax as `.gitignore`). From then on plain
`git add / commit / push / pull` encrypt and decrypt them for you: plaintext
never reaches git history or the remote.

```sh
kis init            # once, in your repo
kis add ana age1…   # give a partner access
kis unlock          # what partners run after cloning
kis status --check  # before every push or PR: exits 1 if a secret would leak
```

Works with any language: kis encrypts files by path, whatever they hold.
`.env` is the default; [other stacks](#other-stacks) list their own secret
files (keystores, Terraform variables, Rails keys…). Built to be driven by coding agents too:
[see below](#coding-agents).

**Website:** [juanfraherrero.github.io/keep-in-secret](https://juanfraherrero.github.io/keep-in-secret) ·
**Vision:** [docs/vision.md](docs/vision.md) ·
**Deep dive:** [docs/how-it-works.md](docs/how-it-works.md) ·
**Troubleshooting:** [docs/troubleshooting.md](docs/troubleshooting.md)

## Install

> [!IMPORTANT]
> **kis requires git 2.28 or newer.** Check with `git --version`. Older git
> resolves `!` patterns in `.gitattributes` differently, which could encrypt the
> wrong files, so `kis init` and `kis unlock` refuse to run on it and tell you to
> upgrade git. How to upgrade depends on your machine; see
> [git-scm.com/downloads](https://git-scm.com/downloads).

```sh
curl -fsSL https://raw.githubusercontent.com/juanfraherrero/keep-in-secret/main/install.sh | sh
```

That downloads the latest [release](https://github.com/juanfraherrero/keep-in-secret/releases)
for macOS or Linux, checks its checksum and installs `kis` (plus `git-kis`, so
`git kis …` works) to `/usr/local/bin`, or `~/.local/bin` when that isn't writable.
`KIS_VERSION=v0.1.0` pins a version. On Windows, download the zip from the
releases page. With Go: `go install github.com/juanfraherrero/keep-in-secret/cmd/kis@latest`.

Everyone who works on the repository installs `kis` once, like `git-lfs`.
`kis version` prints what you have.

### Requirements

| | Minimum | Notes |
|---|---|---|
| git | **2.28** | Tested in CI on 2.28, 2.30, 2.34, 2.39, 2.43, 2.45, 2.52 and 2.55 |
| Go | 1.27 | Only to build from source; release binaries need nothing |
| OS | macOS, Linux, Windows | All three run the full test suite in CI |

`kis status` shows the git version it found and flags it if it's too old.

### Upgrading kis

After upgrading kis in a repository that was set up with an older version, run
`kis sync` once and commit. The generated `.gitattributes` block gained
`merge=kis` and directory-aware patterns; the pre-commit check reminds you if
you forget.

## How it works

```
working tree (.env plaintext)
      │  git add            → kis "clean" filter: encrypts
      ▼
index / commits / remote (.env ciphertext only)
      │  git checkout/pull  → kis "smudge" filter: decrypts
      ▼
working tree (.env plaintext)
```

- `.gitencrypt` is translated into a managed block in `.gitattributes`
  (`.env filter=kis diff=kis merge=kis`), so git routes those files through kis.
  Patterns behave exactly like `.gitignore` (checked against git in the tests).
- Each repository has one random **master key**. Files are encrypted with it using
  AES-256-GCM with a synthetic nonce (HMAC-SHA256 of the content): deterministic, so
  git doesn't see changes that aren't there, and authenticated.
- The master key is committed **once per partner**, encrypted to their public key
  with [age](https://age-encryption.org): `.kis/keys/<name>.age`. Only their private
  key can open it. Public keys are listed in `.kis/recipients.toml`.
- After `kis unlock`, the master key is cached in `.git/kis/key` (never committed).
- A pre-commit check blocks any commit that would store a `.gitencrypt` file in
  plaintext, or where `.gitattributes` is out of date.
- Merges of encrypted files work like normal text merges: kis decrypts both
  sides, merges, and stores the result encrypted. Conflict markers show up in
  your working copy as plaintext.

Crypto details, file format and the git filter protocol are in
[docs/how-it-works.md](docs/how-it-works.md).

Nothing executable lives in the repo: each person installs `kis` once (like `git-lfs`),
and `kis unlock` wires it into their clone.

## Setup (owner)

```sh
cd my-repo
kis init          # age key (created for you, or reuse an existing one)
kis init --identity ~/.ssh/id_ed25519   # or: use your SSH key; partners are then told to send theirs
git commit -m "Set up kis encryption" && git push
```

`kis init` creates `.gitencrypt` (`.env`, `.env.*`, except `.env.example`), encrypts
matching files, offers to un-ignore `.env` from `.gitignore`, adds the kis section
to `AGENTS.md`, and prints the instructions to send to partners.

`--name` is how partners will see you: it is committed in `.kis/recipients.toml`
and names your key file, `.kis/keys/NAME.age`. Without it, kis suggests the part
of your git email before the `@` (asking you to confirm when there is a terminal,
using it as is when there isn't). To change it later, see
[troubleshooting](docs/troubleshooting.md#my-name-in-the-repository-is-wrong).

## Onboarding a partner

1. **Partner** installs kis and creates a key:
   ```sh
   kis keygen          # macOS/Linux: ~/.config/kis/identity   Windows: %APPDATA%\kis\identity
   ```
   It prints a public key (`age1…`) to send over. An existing SSH key also works:
   send `~/.ssh/id_ed25519.pub`.
2. **Owner** grants access, then commits and pushes:
   ```sh
   kis add ana age1…            # or: kis add ana github:ana   or: kis add ana ana.pub
   ```
3. **Partner** clones (or pulls) and runs:
   ```sh
   kis unlock
   ```

## Is it safe to push?

```
$ kis status
…
encrypted files
  .env                           encrypted in git
  .env.local                     PLAINTEXT in index (run `kis sync`)
safe to push    NO: fix the problems below before pushing
                (only files matched by .gitencrypt are checked; other files are not scanned for secrets)
  ✗ .env.local: staged in plaintext
    fix: kis sync (or kis unlock first if this clone is locked)
```

When everything is fine it names what it verified:
`safe to push    yes: .env, .env.local are encrypted in git`. kis vouches for
the files in `.gitencrypt`, not for an API key hardcoded somewhere else; pair it
with a secret scanner (gitleaks, GitHub secret scanning) if you need that.

`kis status` checks that every secret is ciphertext in the index and in the last
commit, and that `.gitattributes` matches `.gitencrypt`. It needs no key, so it
also works in CI and in locked clones.

- `--check` exits 1 unless it is safe to push.
- `--since REV` also checks every commit in `REV..HEAD`, so a secret leaked in an
  earlier commit of a pull request and "fixed" later is still caught.
- `--json` prints the same report as data: `safe_to_push`, `encrypted_files`, each file's `state`
  (`encrypted`, `plaintext-index`, `plaintext-head`, `wrong-group`, `untracked`,
  `ignored`), its `group` and whether you can read it, `groups` with members,
  `you` (the partner this machine acts as) with `your_key` (its file, or
  `KIS_PRIVATE_KEY`), and
  `problems`, each with a `severity` (`error` blocks, `warning` doesn't) and a `fix`.

Run it on every pull request (GitHub Actions):

```yaml
# .github/workflows/secrets.yml
name: secrets
on: pull_request
jobs:
  kis:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }   # --since needs the base branch
      - run: curl -fsSL https://raw.githubusercontent.com/juanfraherrero/keep-in-secret/main/install.sh | sh
      - run: kis status --check --since origin/${{ github.base_ref }}
```

That check needs no key. To let CI read the secrets, see
[CI and cloud agents](#ci-and-cloud-agents).

## Other stacks

kis does not care what is inside a file; `.gitencrypt` decides. Go, Rust,
Python, Node, PHP and Ruby projects usually load `.env` (godotenv, dotenvy,
python-dotenv, dotenv…), which the default covers. Other ecosystems keep
secrets elsewhere: signing keystores, Rails master keys, Terraform variables.
Look at your project, add those paths to `.gitencrypt`, and run `kis sync`:

```
*.jks
**/config/master.key
```

[docs/gitencrypt-patterns.md](docs/gitencrypt-patterns.md) lists the common
ones per stack, ready to copy, with why each is secret and what it means for CI
(a file CI reads in plaintext needs `kis unlock` there once it is encrypted).
Binary files (keystores, certificates) work too; they just can't be merged line
by line.

## Groups: secrets for some people only

By default everyone you add can read every encrypted file. When some files are
only for some people (production credentials, say), name a group at the end of
their `.gitencrypt` line:

```
.env
.env.*
.env.production @prod
```

```sh
kis sync                          # creates the prod group, with you in it
kis add deploy age1… --group prod # a partner who only gets prod
kis add ana --group prod          # an existing partner joins prod
kis remove ana --group prod       # leaves prod; only the prod key rotates
```

People outside a group still clone, pull, edit and commit as usual: that
group's files just stay ciphertext in their clone. `kis status` shows each
file's group and whether you can read it, and blocks (like the pre-commit
check) a file encrypted with the wrong group's key. A file belongs to one
group; to share it with more people, add them to that group. Without any
`@group`, kis never mentions groups.

## CI and cloud agents

A CI runner or a cloud agent (Codex, Claude Code on the web, Copilot coding
agent…) starts from a fresh clone with no key, so its secret files stay
ciphertext. Most jobs don't need more: building, testing code and
`kis status --check` work locked.

A job or agent that may commit should still run `kis unlock` first, even with no
key: it fails with "no key found" or "none of your keys has access", but sets
git up anyway, so `git add` refuses a secret file holding plaintext
(`refusing to store .env in plaintext: repository is locked`) instead of
committing it. Unchanged secret files and all other files commit, pull and push
as usual. A clone where nobody runs kis can't be protected from inside it; the
`kis status --check --since` workflow [above](#is-it-safe-to-push) catches its
commits in the pull request.

When a job needs the real values (a deploy, integration tests), make the
machine a partner of its own:

```sh
kis add ci --new-key              # or: kis add deploy --new-key --group prod
git commit -m "ci access" && git push
```

`--new-key` creates the key pair in memory and prints the private key once; it
is saved nowhere. Store it as a `KIS_PRIVATE_KEY` secret on the platform, then
unlock in the job or the agent's setup script:

```yaml
- run: curl -fsSL https://raw.githubusercontent.com/juanfraherrero/keep-in-secret/main/install.sh | sh
- run: kis unlock
  env:
    KIS_PRIVATE_KEY: ${{ secrets.KIS_PRIVATE_KEY }}
- run: npm run deploy             # .env is plaintext from here on
```

- kis reads the key from the variable, never from a file. It is only needed by
  `kis unlock`; commits, pulls and checkouts after that use the repository key
  git already has.
- While `KIS_PRIVATE_KEY` is set, it is the only key kis uses, in every command
  ([which key kis uses](#which-private-key-kis-uses)), and `kis unlock` says so
  (`unlocked as ci (key from KIS_PRIVATE_KEY)`). `--identity` still wins over
  it. It also takes an SSH private key without a passphrase.
- An agent can run the first `kis init` itself with `KIS_PRIVATE_KEY` set: that
  key becomes the first partner and no key file is written. Without a terminal,
  init warns when its key is the only partner. Never let the only key live on a
  disposable machine: add a person (`kis add NAME KEY`) before pushing.
- Several agents: an unlocked agent or person runs `kis add agent-1 --new-key`
  once per agent, and each printed key goes straight into that agent's
  `KIS_PRIVATE_KEY` secret. One agent can then be revoked
  (`kis remove agent-1`) without touching the others.
- `kis rotate` and removing other partners don't affect it. `kis remove ci`
  revokes it; `kis add ci --new-key --force` replaces a lost or leaked key.
- Give each machine its own partner, in the narrowest group that works: a leak
  then costs one `kis remove` instead of your own key.

## Coding agents

Agents are trained to never commit `.env`, so left alone they may "fix" a kis
setup by ignoring or deleting the encrypted files. `kis init` therefore adds a
short section to `AGENTS.md`, read by most agent tools, and to `CLAUDE.md`,
`GEMINI.md` and `.github/copilot-instructions.md` when they exist, saying the
files are encrypted, that `kis status --check` must pass before a push or PR,
and not to copy secret values around. `kis sync` keeps it up to date; delete
the section and it stays deleted, or skip it with `kis init --no-agents`.

Everything an agent needs is non-interactive: without a terminal, prompts take
their defaults (`-y` forces that), `kis status --json` reports state as data,
and every error names the command that fixes it. A summary for LLMs lives at
[`llms.txt`](https://juanfraherrero.github.io/keep-in-secret/llms.txt).

## Commands

| Command | What it does |
|---|---|
| `kis init [--identity PATH] [--name NAME] [--no-agents] [-y]` | Set up encryption in the repo |
| `kis keygen [--force]` | Create your personal age key, print the public key |
| `kis pubkey [--identity PATH]` | Print the public keys kis uses here, and where each comes from |
| `kis add NAME KEY\|github:USER\|FILE.pub… [--group G] [--identity PATH]` | Give someone access (to a group; omit the key for an existing partner) |
| `kis add NAME --new-key [--group G]` | Add a CI runner or cloud agent; prints its private key once, for `KIS_PRIVATE_KEY` |
| `kis remove NAME [--group G] [--no-rotate]` | Revoke access and rotate the affected keys |
| `kis rotate [--group G]` | New master key(s), re-encrypt those files |
| `kis unlock [--identity PATH]` | Decrypt after cloning; wires git filters and hook. Uses `KIS_PRIVATE_KEY` when set |
| `kis lock [--force]` | Forget the key here and turn files back into ciphertext |
| `kis status [--check] [--json] [--since REV]` | State, partners, encrypted files, and whether it is safe to push |
| `kis sync [--identity PATH]` | Apply `.gitencrypt` edits to `.gitattributes` and re-encrypt |

## Keys

kis accepts **only three kinds of keys**, because it shares the repository key
with [age](https://age-encryption.org), and these are the only ones age can
encrypt to:

| Key | Public key starts with | Where it comes from |
|---|---|---|
| age X25519 | `age1…` | `kis keygen` (recommended) |
| SSH ed25519 | `ssh-ed25519 …` | `ssh-keygen -t ed25519`, or `github:USER` |
| SSH RSA | `ssh-rsa …` | `ssh-keygen -t rsa`, or `github:USER` |

Not supported: **ECDSA** (`ecdsa-sha2-…`), **security keys** (`sk-ssh-…`,
`sk-ecdsa-…`) and **DSA**. kis refuses them with
`unsupported key type`, before writing anything; `github:USER` skips them.
If your only key is one of these, use `kis keygen`.

- **age keys** (`kis keygen`): modern X25519, single-purpose. The file is
  unencrypted at rest, so protect your laptop.
- **SSH keys** (ed25519 or RSA): reuse what you already have; `github:USER` pulls
  public keys from GitHub. kis reads the private key file directly; **no ssh-agent
  needed**. Passphrase-protected keys prompt once, at `kis unlock`. Keys that live only
  in 1Password, a YubiKey or the Secure Enclave can't be used (no file to read): use
  `kis keygen` instead.

- **`KIS_PRIVATE_KEY`**: the text of an age key, or an SSH key without a
  passphrase, in an environment variable. For CI and cloud agents; while it is
  set, key files are ignored.

### `github:USER`: every key on the account

`kis add ana github:ana` downloads `https://github.com/ana.keys`, the public SSH
keys GitHub publishes for every account, and gives access to **all** the
ed25519 and RSA keys there. That is what makes it seamless: Ana can unlock from
any machine she pushes code from. The trade-offs:

- Access is only as safe as the **least protected** of those keys (an old
  laptop, a key copied to a server).
- kis reads them **once**. Removing a key on GitHub doesn't revoke it here:
  run `kis add ana github:ana --force` to pick up her current keys, then
  `kis rotate`, because the removed key could already open the repository key.
- GitHub doesn't publish key titles, so `recipients.toml` can't tell which key
  belongs to which machine.

When kis gets more than one key it says so. For narrow access, ask the person
for one public key and use that instead: `kis add ana KEY --force`.

### Which private key kis uses

Every command that needs your private key (`init`, `unlock`, `pubkey`, and
`sync` or `add` when they create a group) finds it the same way. First match
wins:

1. `--identity PATH`: only that file.
2. `KIS_PRIVATE_KEY`: the key's text, not a path; key files are ignored.
3. The default locations: `$KIS_IDENTITY`, `~/.config/kis/identity`
   (Windows: `%APPDATA%\kis\identity`), `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`.

A source that is set but unusable (a missing file, a `KIS_PRIVATE_KEY` that
isn't a key) is an error; kis never falls back to the next one. Each command
says which key it used (`unlocked as ana (key /home/ana/.ssh/id_ed25519)`), and
`kis pubkey` prints every key it would use with its source.

For a key anywhere else (`~/.ssh/id_ed25519_work`, one key per client…), pass
`--identity PATH`. `PATH` is always your **private** key file, also at
`kis init` (kis derives the public key from it; a `.pub` file is rejected).
kis does not remember that path ([#9](https://github.com/juanfraherrero/keep-in-secret/issues/9)):
pass it again to each of those commands, or set `KIS_IDENTITY=PATH` in your
shell profile so kis tries that file first.

Each partner can use either type; `default_key` in `.kis/config.toml` only decides
what kis suggests to new partners.

## Security notes

- Anyone who could unlock the repo can read every secret in **past commits**.
  `kis remove` rotates the key for future commits; also rotate the real credentials.
- **Secrets committed before kis stay in history.** `kis init` only checks the
  last commit: it warns about files listed in `.gitencrypt` that are tracked
  and stored there in plaintext. A secret committed earlier, on another branch,
  under another name, or already removed from git is not detected. If a secret
  was ever committed in plaintext, it is still in the history: rotate it.
- File names and sizes are visible, and anyone can see *when* an encrypted file
  changed (not what changed).
- Secrets are only as safe as the least protected machine holding a key.
- Without `kis unlock`, a clone just has ciphertext, but git there doesn't
  encrypt either: an edited or new secret file would be committed in plaintext.
  Run `kis unlock` in every clone that may commit, even without a key; it sets
  git up so that `git add` refuses instead.

## Development

```sh
make build        # ./bin/kis and ./bin/git-kis (so `git kis …` works)
make test         # vet, unit tests and every end-to-end scenario
make test-e2e     # just the scenarios, verbose
make test-update  # rewrite expected output after an intentional change
make test-docker  # whole suite across git 2.28 → 2.55 and Linux distros (needs Docker / Colima)
make fuzz         # fuzz decryption and the git filter protocol (FUZZTIME=30s)
```

End-to-end scenarios live in [`cmd/kis/testdata/script/`](cmd/kis/testdata/script/):
each `.txtar` file is one user journey written like a terminal session, with
assertions on output and on exactly which files every command changed. See its
[README](cmd/kis/testdata/script/README.md) for the scenario language.

The website is a single static file, `docs/index.html` (GitHub Pages: deploy from
the `/docs` folder on `main`).
