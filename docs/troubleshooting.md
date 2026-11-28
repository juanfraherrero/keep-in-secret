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

**Cause:** kis shares the repository key with age, which can only encrypt to
three kinds of keys: age (`age1…`), SSH ed25519 (`ssh-ed25519`) and SSH RSA
(`ssh-rsa`). ECDSA, security keys (`sk-…`) and DSA are refused. Nothing is
written when this happens.

**What to do:** use an ed25519 or RSA SSH key, or a kis key from
`kis keygen`.

### `".env.prod @bad group" looks like a group, but group names are one word`

**Cause:** a `.gitencrypt` line ends in `@` followed by words with spaces. A
group goes last, as one word: `PATTERN @GROUP`. Otherwise kis would read the
whole line as a file name, and the file wouldn't be in the group you meant.

**What to do:** fix the line (for example `.env.prod @prod`), then run
`kis sync`. A file belongs to one group; to share it with more people, add
them to that group.

### `kis init` stopped with an error, and running it again says "already set up"

**Cause:** `kis init` writes `.kis/` early, then wires git, syncs the files
and writes the `AGENTS.md` section. If a later step fails (for example
`creating group G: none of this machine's keys belongs to a partner`), the
steps before it are kept, so a second `kis init` sees a repository that is
already set up.

**What to do:** fix the cause in the error first. Then:

- **If you haven't committed the setup yet:** delete the `.kis/` folder and run
  `kis init` again. This is the complete fix: you get a new repository key and
  the `AGENTS.md` section.
- **If you already committed or pushed it** (partners may have the key): run
  `kis sync` instead. It finishes the files, `.gitattributes` and staging. It
  does **not** create the `AGENTS.md` section, because `kis sync` only
  refreshes a section that exists, so coding agents won't get the kis rules
  until you add it.

### `The only key that can unlock this repository is …`

**Cause:** `kis init` ran without a terminal (a coding agent, CI) and its key
is the only partner. If that key lives on a disposable machine or in a
platform secret, losing it means nobody can unlock the repository again.

**What to do:** before pushing, add another partner, usually the person who
owns the repository: `kis add NAME KEY` with their public key, then commit.

## Keys

### Which private key is kis using?

Every command that needs your private key (`init`, `unlock`, `pubkey`, and
`sync` or `add` when they create a group) applies the same rule. First match
wins:

1. `--identity PATH`: only that file.
2. `KIS_PRIVATE_KEY`: the key's text (not a path); key files are ignored.
3. The default locations: `$KIS_IDENTITY`, `~/.config/kis/identity`
   (Windows: `%APPDATA%\kis\identity`), `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`.

A source that is set but unusable is an error; kis never falls back to the
next one.

**How to see it:** `kis pubkey` prints each key it would use with its source
(`# KIS_PRIVATE_KEY (age); key files are ignored while it is set`).
`kis unlock` names it after the partner (`unlocked as ci (key from
KIS_PRIVATE_KEY)`), and `kis status` shows it on the `you` line.

**Common mistakes:**

- `KIS_PRIVATE_KEY` left set on a laptop (copied from a CI setup, exported in
  a shell profile). It hides your key files, so kis acts as that CI partner.
  Unset it in that shell.
- A key at a custom path (`~/.ssh/id_ed25519_work`) without `--identity` or
  `KIS_IDENTITY`. kis doesn't search other files: pass `--identity PATH`, or
  set `KIS_IDENTITY=PATH`.
- An SSH key with a passphrase in `KIS_PRIVATE_KEY`. Nothing can type the
  passphrase there; see
  [`KIS_PRIVATE_KEY: passphrase-protected SSH keys can't be used here`](#kis_private_key-passphrase-protected-ssh-keys-cant-be-used-here).

### `found a key file at PATH, but it can't be opened as an age or SSH private key`

**Cause:** the file exists but is not a private key kis can read. It may be
corrupted, truncated, or in another format.

**What to do:** restore it from a backup if you have one. If the file is
kis's own key (`~/.config/kis/identity`), you can replace it with
`kis keygen --force`, which keeps the old file as `identity.bak`.
Repositories you were already in then need a partner to run
`kis add YOUR_NAME NEW_KEY --force`. That re-wraps the same repository key for
your new key; no files are re-encrypted.

### `identity.bak already exists and would be deleted for good`

**Cause:** `kis keygen --force` moves your current key to `identity.bak`, and
an `identity.bak` from an earlier `--force` is already there. Replacing it
deletes that older key, which may be the only copy of a key that still opens
a repository. At a terminal kis asks first; without one it refuses. Nothing
was changed.

**What to do:** if every repository you are in already knows your current key
(you re-wrapped them, see below), the older backup is no longer needed: run
`kis keygen --force -y`. If you are not sure, copy `identity.bak` somewhere
safe first.

### My private key leaked, or I replaced it with `kis keygen --force`

**Cause:** each repository wraps its key for your **public** key, so after
`kis keygen --force` your repositories still only know the old one, now in
`identity.bak`. kis doesn't know which repositories you are in.

**What to do:** run `kis keygen --force` once (the old key moves to
`~/.config/kis/identity.bak`). Then, in each repository you are in, in this
order:

```sh
# only if this clone is locked:
kis unlock --identity ~/.config/kis/identity.bak
kis add YOUR_NAME NEW_KEY --force   # NEW_KEY: the public key keygen printed
kis rotate                  # only if the old key leaked
git commit -m "Replace my kis key" && git push
```

`kis add --force` makes your access file open only with the new key. If the
old key leaked, `kis rotate` creates a new repository key, wrapped only for
the keys listed now, so the leaked key can't read new commits. Run it
**after** `kis add --force`, or the new repository key is wrapped for the
leaked key too.

If the key leaked, also know:

- Rotating only protects commits made after it. With the leaked key, someone
  with a copy of the repository can still decrypt everything committed
  before, so change the secret values themselves (passwords, API keys).
- Every partner has to run `kis unlock` after pulling. Until they do, the
  files they commit are still encrypted with the old repository key, which
  the leaked key opens (issue #8).

When every repository is done, delete `~/.config/kis/identity.bak`.

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

**Cause:** creating a group needs to know which partner you are, and the key
kis found ([which key](#which-private-key-is-kis-using)) belongs to nobody in
the repository. This usually happens when you set the repository up with
`--identity PATH` and didn't pass it again.

**What to do:** run the same command again with `--identity PATH` (the key you
used at init), or set `KIS_IDENTITY=PATH`. If `KIS_PRIVATE_KEY` is set, that
key is the one being used: unset it unless this machine is that partner.

### `KIS_PRIVATE_KEY: not an age or SSH private key`

**Cause:** the variable is set, but its value isn't a private key (a public
key, a path, a truncated copy). kis stops instead of using your key files.

**What to do:** put the whole private key text in it (`AGE-SECRET-KEY-1…`, or
an SSH private key including its BEGIN and END lines), or unset it. The same
applies to `--identity: no such file` and `KIS_IDENTITY: no such file`: fix the
path or drop it.

### `incorrect passphrase for your SSH key; nothing was unlocked`

**Cause:** the passphrase typed at `kis unlock` doesn't open your SSH key.

**What to do:** run `kis unlock` again with the right passphrase.

### `KIS_PRIVATE_KEY: passphrase-protected SSH keys can't be used here`

**Cause:** on CI and cloud agents nothing can type a passphrase.

**What to do:** give that machine its own key with `kis add NAME --new-key`
and store the printed key as `KIS_PRIVATE_KEY`.

### `kis add NAME --new-key` printed the key, but my pipe or log only got part of the output

**Cause:** this is by design. With `--new-key`, stdout carries only the
private key, on one line. The `✓ added` line, "shown once, saved nowhere" and
the instructions go to stderr. At a terminal both appear as one block; piped,
only the key reaches the next command, so it can go straight into the CI or
agent platform's secret store without appearing in any output. A script that
captures stdout gets the key and nothing else; one that captures stderr (or
`2>&1`) gets the messages.

**What to do:** pipe stdout into the platform's secret store as
`KIS_PRIVATE_KEY`. The command depends on the platform; for example, with
GitHub Actions and the GitHub CLI:
`kis add ci --new-key | gh secret set KIS_PRIVATE_KEY`. Never redirect it into
a file, a log or a chat.

### A private key from `kis add --new-key` was printed where it shouldn't be

**Cause:** the key reached a log, a file, a chat or an agent's transcript,
for example because stdout wasn't piped into the secret store.

**What to do:** replace it with `kis add NAME --new-key --force` and store the
new key as that machine's `KIS_PRIVATE_KEY`, **then always run `kis rotate`**,
and commit and push. `--force` alone isn't enough: the old key can still open
the repository key from an older `.kis/keys/NAME.age` in the git history, and
with it every current and future secret. Only `kis rotate` replaces the
repository key. Partners run `kis unlock` after pulling. Secrets the leaked key
could already decrypt stay readable to whoever has it: rotate the real
credentials too.

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

### `kis remove`: `NAME had no key file in any group, so no group key was rotated`

**Cause:** NAME is in `.kis/recipients.toml` but has no `.age` file under
`.kis/keys/`. Older versions of kis left this state when `kis remove` refused
because of uncommitted changes: the key file was already deleted, and the key
was not rotated (issue #10). A key file deleted by hand does the same. kis
can't tell which groups NAME was in, so it drops NAME from the partner list
without rotating anything.

**What to do:** if NAME ever ran `kis unlock`, their clone still has the
current key and can decrypt new secrets. Run `kis rotate` (it rotates every
group you are in), and ask a member of each group you are not in to run it
too. Then commit and push.

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

**Cause:** `git add` (or `git commit -a`, `git diff`) of a secret file that
holds plaintext, in a clone without the key: a locked clone, or a fresh clone
where `kis unlock` found no access (it sets git up anyway). kis refuses, so the
plaintext never reaches git. Unchanged secret files are ciphertext and pass
through, and other files are not affected.

**What to do:** if you have access, run `kis unlock`, then `git add` again.
If you don't, secret files can't be changed from this clone: put the file back
with `git checkout -- FILE` (or delete it if it is new) and ask someone with
access to make the change, or to add you (`kis add YOUR_NAME KEY`).

### `commit blocked, these files would be stored in PLAINTEXT`

**Cause:** a secret file was staged while the kis filter wasn't active.

**What to do:** run `kis sync` (or `kis unlock` first if this clone is
locked), then commit again.

### `commit blocked, these local files would be committed in PLAINTEXT`

Also in `kis status`: `FILE: a local file (.env*.local) staged in plaintext`,
`… the last commit stores in plaintext`, or with `--since`,
`a commit to be pushed stores this local file (.env*.local) in plaintext`.

**Cause:** FILE is a local file (`.env.local`, `.env.development.local`, any
`.env*.local`): one developer's values. The default `.gitencrypt` excludes
local files (`!.env*.local`), so kis doesn't encrypt them, and this project
doesn't gitignore FILE, so `git add .` staged it in plaintext. It also happens
when FILE was committed before kis, or when `!.env*.local` was added to a
`.gitencrypt` that used to encrypt it (`kis sync` then stages it in plaintext
and warns `local files tracked in PLAINTEXT`).

**What to do:** pick one.

- Keep it on this machine (the usual case): add it to `.gitignore` (for
  example the line `.env*.local`), run `git rm --cached -- FILE` (the file stays
  on disk), and commit.
- Share it with the team, encrypted: delete the `!.env*.local` line from
  `.gitencrypt`, run `kis sync`, and commit.

If the last commit, or a commit you already pushed, holds FILE in plaintext,
its values are in the history: rotate any token it holds. A commit not pushed
yet can be rewritten instead (`git commit --amend`, `git rebase -i`).

### `.gitencrypt changed but .gitattributes was not updated`

**Cause:** `.gitencrypt` was edited without regenerating `.gitattributes`, so
the new patterns aren't encrypted yet.

**What to do:** run `kis sync`, then commit again.

### A clone without `kis unlock` committed `.env` in plaintext

**Cause:** git only encrypts once kis is wired into a clone, which happens at
`kis init` or `kis unlock`. A fresh clone where nobody ran `kis unlock` (often
a CI runner or a cloud agent) has neither the filter nor the pre-commit check,
so `git add .env` stores the plaintext. `kis status` warns about such a clone
(`git filters are not configured in this clone`), and `kis status --check`
reports the commit.

**What to do:** run `kis unlock` in that clone, even without a key: when it
says you have no access, git is still set up, and from then on `git add`
refuses plaintext in secret files (`refusing to store FILE in plaintext`).
Then:

- Not pushed yet: drop that commit from your branch before pushing, and put
  `.env` back with `git checkout -- .env`.
- Pushed: the value is in the remote's history. Rotate the credentials in that
  file, and have someone with access commit the encrypted version
  (`kis status --check` then passes again). Rewriting git history is outside kis.

A clone where nobody ever runs kis can't be protected from inside it. Run
`kis status --check --since origin/main` in CI on every pull request (see the
README's [workflow](../README.md#is-it-safe-to-push)): it fails when any
commit to be merged stores a secret in plaintext.

## Merging

### A merge of an encrypted file reports a binary conflict

_Not documented yet._
