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

Requires Go and git.

```sh
go install github.com/juanfraherrero/keep-in-secret/cmd/kis@latest
```

Or from a clone: `make install` (also links `git-kis`, so `git kis …` works).
Everyone who works on the repository installs `kis` once, like `git-lfs`.

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
  (`.env filter=kis diff=kis`), so git routes those files through kis.
- Each repository has one random **master key**. Files are encrypted with it using
  AES-256-GCM with a synthetic nonce (HMAC-SHA256 of the content): deterministic, so
  git doesn't see changes that aren't there, and authenticated.
- The master key is committed **once per partner**, encrypted to their public key
  with [age](https://age-encryption.org): `.kis/keys/<name>.age`. Only their private
  key can open it. Public keys are listed in `.kis/recipients.toml`.
- After `kis unlock`, the master key is cached in `.git/kis/key` (never committed).
- A pre-commit check blocks any commit that would store a `.gitencrypt` file in
  plaintext, or where `.gitattributes` is out of date.

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
make test     # unit + end-to-end tests (drive real git repos)
make build    # ./bin/kis and ./bin/git-kis (so `git kis …` works)
```

The website is a single static file, `docs/index.html` (GitHub Pages: deploy from
the `/docs` folder on `main`).
