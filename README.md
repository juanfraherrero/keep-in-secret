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
`.env` is the default; [presets](#other-stacks-presets) cover keystores,
Terraform variables, Rails and Spring. Built to be driven by coding agents too:
[see below](#coding-agents).

**Website:** [juanfraherrero.github.io/keep-in-secret](https://juanfraherrero.github.io/keep-in-secret) ·
**Deep dive:** [docs/how-it-works.md](docs/how-it-works.md)

## Install

> [!IMPORTANT]
> **kis requires git 2.28 or newer.** Check with `git --version`. Older git
> resolves `!` patterns in `.gitattributes` differently, which could encrypt the
> wrong files, so `kis init` and `kis unlock` refuse to run on it and print the
> upgrade command for your OS.

```sh
go install github.com/juanfraherrero/keep-in-secret/cmd/kis@latest
```

Or from a clone: `make install` (also links `git-kis`, so `git kis …` works).
Everyone who works on the repository installs `kis` once, like `git-lfs`.

### Requirements

| | Minimum | Notes |
|---|---|---|
| git | **2.28** | Tested in CI on 2.28, 2.30, 2.34, 2.39, 2.43, 2.45, 2.52 and 2.55 |
| Go | 1.27 | Only to build kis; not needed at runtime |
| OS | macOS, Linux, Windows | All three run the full test suite in CI |

### Upgrading git

| OS | Command |
|---|---|
| macOS | `brew install git`, then open a new terminal |
| Windows | `winget install --id Git.Git -e` or [git-scm.com](https://git-scm.com/download/win) |
| Ubuntu | `sudo add-apt-repository ppa:git-core/ppa && sudo apt update && sudo apt install git` |
| Debian | `sudo apt install git` (Debian 11 and newer ship a supported git) |
| Fedora / RHEL | `sudo dnf install git` |
| Alpine | `apk add git` |

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
kis init --ssh    # or: use ~/.ssh/id_ed25519 and make SSH the default for partners
git commit -m "Set up kis encryption" && git push
```

`kis init` creates `.gitencrypt` (`.env`, `.env.*`, except `.env.example`), encrypts
matching files, offers to un-ignore `.env` from `.gitignore`, adds the kis section
to `AGENTS.md`, and prints the instructions to send to partners.

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
  (`encrypted`, `plaintext-index`, `plaintext-head`, `untracked`, `ignored`) and
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
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - run: go install github.com/juanfraherrero/keep-in-secret/cmd/kis@latest
      - run: kis status --check --since origin/${{ github.base_ref }}
```

## Other stacks (presets)

kis does not care what is inside a file; `.gitencrypt` decides. Not every
ecosystem keeps secrets in `.env`, so `kis init` detects common stacks and
offers a preset (without a terminal it prints the command instead, because
encrypting files your CI reads in plaintext must be a deliberate choice):

```sh
kis init --preset terraform,android   # at setup
kis sync --preset spring              # or any time later
```

| Preset | Encrypts | Detected by |
|---|---|---|
| `android` | `*.jks`, `*.keystore`, `keystore.properties` | `*/src/main/AndroidManifest.xml` |
| `rails` | `config/master.key`, `config/credentials/*.key` | `config/application.rb` |
| `spring` | `application-local.{properties,yml,yaml}` | `pom.xml`, `build.gradle(.kts)` |
| `terraform` | `*.tfvars`, `*.tfvars.json` (not `*.tfvars.example`) | `*.tf` |

Go, Rust, Python, Node, PHP and Ruby projects usually load `.env` (godotenv,
dotenvy, python-dotenv, dotenv…), which the default covers. Anything else, such
as `config/secrets.json` or `appsettings.Development.json`, is one line in
`.gitencrypt` plus `kis sync`. Binary files (keystores, certificates) work too;
they just can't be merged line by line.

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
| `kis init [--ssh] [--identity PATH] [--name NAME] [--preset LIST] [--no-agents] [-y]` | Set up encryption in the repo |
| `kis keygen [--force]` | Create your personal age key, print the public key |
| `kis pubkey` | Print the public keys found on this machine |
| `kis add NAME KEY\|github:USER\|FILE.pub…` | Give someone access |
| `kis remove NAME [--no-rotate]` | Revoke access and rotate the master key |
| `kis rotate` | New master key, re-encrypt everything |
| `kis unlock [--identity PATH]` | Decrypt after cloning; wires git filters and hook |
| `kis lock [--force]` | Forget the key here and turn files back into ciphertext |
| `kis status [--check] [--json] [--since REV]` | State, partners, encrypted files, and whether it is safe to push |
| `kis sync [--preset LIST]` | Apply `.gitencrypt` edits to `.gitattributes` and re-encrypt |

## Keys

- **age keys** (`kis keygen`): modern X25519, single-purpose. The file is
  unencrypted at rest, so protect your laptop.
- **SSH keys** (ed25519 or RSA): reuse what you already have; `github:USER` pulls
  public keys from GitHub. kis reads the private key file directly; **no ssh-agent
  needed**. Passphrase-protected keys prompt once, at `kis unlock`. Keys that live only
  in 1Password, a YubiKey or the Secure Enclave can't be used (no file to read): use
  `kis keygen` instead.

Each partner can use either type; `default_key` in `.kis/config.toml` only decides
what kis suggests to new partners.

## Security notes

- Anyone who could unlock the repo can read every secret in **past commits**.
  `kis remove` rotates the key for future commits; also rotate the real credentials.
- File names and sizes are visible, and anyone can see *when* an encrypted file
  changed (not what changed).
- Secrets are only as safe as the least protected machine holding a key.
- Without `kis unlock`, a clone just has ciphertext; nothing leaks.

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
