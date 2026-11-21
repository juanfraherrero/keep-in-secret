# kis: keep-in-secret

Commit your `.env` files to git, **encrypted**, and share them with your team.

List the files in `.gitencrypt` (same syntax as `.gitignore`). From then on plain
`git add / commit / push / pull` encrypt and decrypt them for you: plaintext
never reaches git history or the remote.

```sh
kis init            # once, in your repo
kis add ana age1…   # give a partner access
kis unlock          # what partners run after cloning
```

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
matching files, offers to un-ignore `.env` from `.gitignore`, and prints the
instructions to send to partners.

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

## Commands

| Command | What it does |
|---|---|
| `kis init [--ssh] [--identity PATH] [--name NAME] [-y]` | Set up encryption in the repo |
| `kis keygen [--force]` | Create your personal age key, print the public key |
| `kis pubkey` | Print the public keys found on this machine |
| `kis add NAME KEY\|github:USER\|FILE.pub…` | Give someone access |
| `kis remove NAME [--no-rotate]` | Revoke access and rotate the master key |
| `kis rotate` | New master key, re-encrypt everything |
| `kis unlock [--identity PATH]` | Decrypt after cloning; wires git filters and hook |
| `kis lock [--force]` | Forget the key here and turn files back into ciphertext |
| `kis status` | State, partners, encrypted files |
| `kis sync` | Apply `.gitencrypt` edits to `.gitattributes` and re-encrypt |

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
