# Troubleshooting

Each entry starts from what you see: a message or a symptom. Entries marked
_not documented yet_ are known symptoms whose fix still has to be written.

Something missing? Open an issue with the exact message and what you ran.

## Setup

### `kis needs git 2.28 or newer (found X)`

**Cause:** older git resolves `!` patterns in `.gitattributes` differently,
which could encrypt the wrong files, so `kis init` and `kis unlock` refuse.

**What to do:** upgrade git on this machine. How depends on the machine
(package manager, admin rights, distro release); see
[git-scm.com/downloads](https://git-scm.com/downloads).

### `kis is already set up here (.kis/ exists)`

**Cause:** someone already ran `kis init` in this repository, for example
because you cloned it. Running it again is refused so nobody's access is lost.

**What to do:** to get access, run `kis unlock`. To give someone access,
run `kis add NAME KEY`.

### `kis was set up in this repository, but .kis/config.toml is missing`

**Cause:** `.kis/` is there, but `config.toml`, which every `kis init`
writes, was deleted or lost in a merge. Every kis command refuses until it is
back.

**What to do:** restore `.kis/config.toml` from git history and commit it.
Don't run `kis init` again: it would replace the repository key and drop the
partners listed in `.kis/recipients.toml`.

### My name in the repository is wrong

**Cause:** `kis init` used the part of your git email before the `@`, or a
`--name` you want to change. There is no rename command.

**What to do:** commit or stash your changes to secret files first (`kis remove`
refuses otherwise). Then add yourself under the new name with your own public
key (`kis pubkey` prints it), and remove the old name. The order matters: kis
refuses to remove the last partner.

```sh
kis add NEW_NAME YOUR_PUBLIC_KEY
kis remove OLD_NAME
```

Commit and push. Removing a name rotates the repository key, so if there are
other partners, each one runs `kis unlock` after pulling.

### `unsupported key type "ecdsa-…"` or `"sk-…"`

**Cause:** age, which kis uses to share the repository key, can only encrypt
to ed25519 and RSA SSH keys.

**What to do:** use an ed25519 or RSA SSH key, or a kis key from
`kis keygen`.

## Keys

### `found a key file at PATH, but it can't be opened as an age or SSH private key`

**Cause:** the file exists but is not a private key kis can read. It may be
corrupted, truncated, or in another format.

**What to do:** restore it from a backup if you have one. If the file is
kis's own key (`~/.config/kis/identity`), you can replace it with
`kis keygen --force`, which keeps the old file as `identity.bak`.
Repositories you were already in then need a partner to run
`kis add YOUR_NAME NEW_KEY --force`. That re-wraps the same repository key for
your new key; no files are re-encrypted.

### `PATH is a public key; pass the private key file instead`

**Cause:** `--identity` points at a `.pub` file. kis needs the private half,
which stays on your machine.

**What to do:** pass the same path without `.pub`.

### `no key found on this machine`

**Cause:** kis looked in `$KIS_IDENTITY`, `~/.config/kis/identity`,
`~/.ssh/id_ed25519` and `~/.ssh/id_rsa` and found none of them.

**What to do:** if you have no key yet, run `kis keygen` and send the public
key it prints to someone with access. If your key lives somewhere else, run
`kis unlock --identity PATH`, or set `KIS_IDENTITY=PATH`.

### `creating group G: none of this machine's keys belongs to a partner`

**Cause:** creating a group needs to know which partner you are, and kis
only looks for your key in the standard places listed above. This happens
when you set the repository up with `--identity PATH`.

**What to do:** set `KIS_IDENTITY=PATH` to the key you used with
`--identity`, and run the command again.

### `incorrect passphrase for your SSH key; nothing was unlocked`

**Cause:** the passphrase typed at `kis unlock` doesn't open your SSH key.

**What to do:** run `kis unlock` again with the right passphrase.

### `KIS_PRIVATE_KEY: passphrase-protected SSH keys can't be used here`

**Cause:** on CI and cloud agents nothing can type a passphrase.

**What to do:** give that machine its own key with `kis add NAME --new-key`
and store the printed key as `KIS_PRIVATE_KEY`.

## Access

### `none of your keys has access to this repository yet`

**Cause:** your key is valid, but nobody has added you to this repository.

**What to do:** send one of the public keys kis printed to someone with
access, and ask them to run `kis add YOUR_NAME KEY` and push. Then pull and
run `kis unlock`.

### `not in group G: FILE stays encrypted`

**Cause:** that file belongs to a group (`@G` in `.gitencrypt`) you are not a
member of. This is expected: you can keep working and committing around it.

**What to do:** if you need it, ask a member of the group to run
`kis add YOUR_NAME --group G`, then run `kis unlock`.

### `.env` stays binary after `git pull`, with `kis: cannot decrypt .env`

**Cause:** this clone has an old repository key cached. Usually someone ran
`kis remove` or `kis rotate`, which replaced the key. kis does not pick up the
new key on its own (issue #8).

**What to do:** run `kis unlock`. Until you do, don't commit secret files
from this clone: they would be encrypted with the old key.

### A commit made with the old key can't be decrypted by the others

**Cause:** someone committed a secret file before running `kis unlock` after
a rotation (issue #8). Partners with the new key can't read it, and the
removed partner can.

_Not documented yet._

## Committing

### `these were already committed in PLAINTEXT before: FILE`

**Cause:** at `kis init`, FILE is tracked, listed in `.gitencrypt`, and the
last commit stores it in plaintext. From now on it is committed encrypted, but
the old commits still contain the plaintext.

**What to do:** rotate the credentials in that file (new passwords, new API
keys). Rewriting git history is outside kis.

### No warning, but a secret was committed in plaintext before kis

**Cause:** `kis init` only checks the last commit, and only files tracked
right now. A secret committed in an earlier commit, on another branch, under
another name, or removed from git before kis (`git rm --cached` plus
`.gitignore`) is not detected.

**What to do:** if you know or suspect a secret was ever committed in
plaintext, rotate it. It is still in the history, and on the remote if it was
pushed.

### `refusing to store FILE in plaintext: repository is locked`

**Cause:** `git add` of a secret file in a clone without the key. kis
refuses, so the plaintext never reaches git.

**What to do:** run `kis unlock`, then `git add` again.

### `commit blocked, these files would be stored in PLAINTEXT`

**Cause:** a secret file was staged while the kis filter wasn't active.

**What to do:** run `kis sync` (or `kis unlock` first if this clone is
locked), then commit again.

### `.gitencrypt changed but .gitattributes was not updated`

**Cause:** `.gitencrypt` was edited without regenerating `.gitattributes`, so
the new patterns aren't encrypted yet.

**What to do:** run `kis sync`, then commit again.

### A clone without `kis unlock` committed `.env` in plaintext

**Cause:** git only encrypts once kis is wired into a clone, which happens at
`kis init` or a successful `kis unlock`. A fresh clone without a key (often a
CI runner or a cloud agent) has neither the filter nor the pre-commit check.
`kis status --check` reports the commit.

_Not documented yet._

## Merging

### A merge of an encrypted file reports a binary conflict

_Not documented yet._
