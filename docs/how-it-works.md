# How kis works

This page covers how kis is built. For everyday use, see the [README](../README.md).

> **Requirement: git 2.28 or newer.** Older git resolves negated `.gitattributes`
> patterns differently (found by the differential test in §6), which could encrypt
> the wrong set of files. `kis init` and `kis unlock` check `git version` and refuse,
> saying git must be upgraded (no command: how depends on the machine); `kis status` shows the version.

kis does two jobs:

1. **Encrypt files transparently inside git**, using git's own filter mechanism.
2. **Share one repository key** with every partner, wrapped to each person's public key.

```
            OWNER                         REMOTE (GitHub, GitLab…)                  PARTNER
  ┌──────────────────────┐          ┌──────────────────────────┐          ┌──────────────────────┐
  │ .env  (plaintext)    │          │ .env        ciphertext   │          │ .env  (plaintext)    │
  │   │ git add          │   push   │ .kis/keys/owner.age      │   pull   │   ▲ smudge filter    │
  │   ▼ clean filter     │ ───────► │ .kis/keys/ana.age        │ ───────► │   │ (decrypt)        │
  │ blob = AES-256-GCM   │          │ .kis/recipients.toml     │          │ kis unlock: ana.age  │
  │                      │          │   (never a key, never    │          │  + ana's private key │
  │ .git/kis/key (local) │          │    plaintext)            │          │  → .git/kis/key      │
  └──────────────────────┘          └──────────────────────────┘          └──────────────────────┘
```

---

## 1. Git integration

### `.gitencrypt` → `.gitattributes`

You write gitignore-style patterns in `.gitencrypt`. kis compiles them into a
managed block in `.gitattributes` (`internal/gitencrypt`):

```gitattributes
# >>> kis: generated from .gitencrypt, do not edit (run `kis sync`)
.env filter=kis diff=kis merge=kis
**/.env/** filter=kis diff=kis merge=kis
.env.* filter=kis diff=kis merge=kis
**/.env.*/** filter=kis diff=kis merge=kis
.env.example !filter !diff !merge
**/.env.example/** !filter !diff !merge
.gitencrypt !filter !diff !merge
.gitattributes !filter !diff !merge
.gitignore !filter !diff !merge
.kis/** !filter !diff !merge
.kis/recipients.toml merge=union
# <<< kis
```

The goal is that a path is encrypted **exactly** when the same line in a
`.gitignore` would match it. gitattributes only ever matches files, so each
pattern is translated (`attrPatterns`):

| `.gitencrypt` | gitattributes lines | Why |
|---|---|---|
| `.env` | `.env` and `**/.env/**` | gitignore's `.env` also matches a *directory* named `.env`, at any depth |
| `secrets/` | `**/secrets/**` | a trailing `/` means "directory, anywhere" |
| `config/prod.yaml`, `/secrets/` | as is, plus `…/**` | a slash anchors to the repo root |
| `!.env.example` | `!filter !diff !merge` | negations unset the attributes |
| `\#hash`, `\!bang` | unchanged | backslash escapes mean the same in both syntaxes |

One difference is intentional: gitignore can't re-include a file inside an
excluded directory (`secrets/` then `!secrets/README.md`), but `.gitencrypt` can.

- kis's own files always come **last**, so a broad pattern like `*` can never
  encrypt the files kis needs to read.
- `.kis/recipients.toml` uses git's built-in `merge=union`, so partners added on
  two branches merge without a conflict.
- `kis sync` rewrites only the text between the markers. Anything else in
  `.gitattributes` is left alone. It then re-adds every affected tracked file:
  new matches get encrypted, and files **dropped** from `.gitencrypt` go back to
  plaintext (this needs the key, so it's refused while locked).

### Filter wiring (`.git/config`)

`kis init` / `kis unlock` register the driver locally (`internal/repo/wiring.go`):

| Key | Value | Purpose |
|---|---|---|
| `filter.kis.process` | `kis filter-process` | Long-running filter: one process per git command |
| `filter.kis.clean` / `smudge` | `kis clean %f` / `kis smudge %f` | One-shot fallback |
| `filter.kis.required` | `true` | If kis fails, git fails rather than storing plaintext |
| `diff.kis.textconv` | `kis textconv` | `git diff` shows decrypted content when unlocked |
| `merge.kis.driver` | `kis merge %O %A %B %L %P` | 3-way merge of encrypted files as text (below) |

The repository holds no executable code: this config lives in `.git/config`,
which is never committed.

### Long-running filter protocol

`internal/filter` speaks git's *Long Running Filter Process* protocol (version 2,
pkt-line framing). The handshake:

```
git → kis   git-filter-client, version=2, flush
kis → git   git-filter-server, version=2, flush
git → kis   capability=clean, capability=smudge, …, flush
kis → git   capability=clean, capability=smudge, flush
```

Then git sends one request per file (`command=clean|smudge`, `pathname=…`, then
the content in ≤65516-byte packets) and kis answers `status=success` plus the
result, or `status=error`. The key is loaded **once** per process (`filter.Cached`),
so checking out 1,000 secrets doesn't read the key 1,000 times.

### Clean and smudge rules

| | Key available | Key missing (locked) |
|---|---|---|
| **clean** (`git add`) | encrypt | **refuse**: error, nothing staged |
| **smudge** (checkout) | decrypt | pass ciphertext through unchanged |

- **clean is idempotent**: content that already starts with the kis header is
  stored as is.
- **smudge never breaks a checkout**: without the key, or with the wrong one, you
  just get ciphertext in your working tree.

### Merging encrypted files

Git can't merge ciphertext: left alone it reports a binary conflict and keeps
one side. The `merge=kis` driver fixes that:

1. git passes the three blobs as stored (base `%O`, ours `%A`, theirs `%B`), all ciphertext;
2. kis decrypts them into a private temp dir under `.git/kis/`;
3. it runs `git merge-file` on the plaintext (labels `ours` / `base` / `theirs`);
4. it writes the result back to `%A` **encrypted**, and exits non-zero on conflicts.

git then checks the result out through the smudge filter, so conflict markers
show up in the working copy as plaintext, while the object store only ever sees
ciphertext. When locked, the driver refuses and git reports a normal conflict.

### Pre-commit safety net

A `pre-commit` hook runs `kis precommit`, which blocks the commit when:

- `.gitencrypt` changed but `.gitattributes` wasn't regenerated (`kis sync`), or
- a staged blob that *should* be encrypted doesn't carry the kis header
  (`git cat-file blob :path`, checked against attributes from the **index**), or
- a staged local file (`.env*.local`, any depth, any case) that kis doesn't
  encrypt holds plaintext. The default `.gitencrypt` excludes local files, so
  the check above can't see them; without this one, a project that doesn't
  gitignore them would commit one developer's tokens in plaintext.
  `kis status` reports the same for the index, `HEAD` and `--since` commits.

An existing hook is never overwritten. kis prints the line you need to add to it.

---

## 2. Encryption (`internal/crypt`)

### Why it must be deterministic

Git identifies content by hash. If the same `.env` encrypted to different bytes
each time, `git status` would always show it as modified. kis therefore uses
**deterministic authenticated encryption**: SIV-style AES-256-GCM.

### Key derivation

```
master key (32 random bytes, one per repository)
   ├── HKDF-SHA256(info="kis file encryption v1") → encKey (AES-256-GCM key)
   └── HKDF-SHA256(info="kis file nonce v1")      → macKey (HMAC-SHA256 key)
```

### Encrypt

```
nonce      = HMAC-SHA256(macKey, plaintext)[:12]
ciphertext = AES-256-GCM.Seal(encKey, nonce, plaintext, aad = Magic)
output     = Magic ‖ nonce ‖ ciphertext ‖ tag
```

A nonce repeats only when the plaintext is identical, and then the output is
identical too. That avoids GCM's nonce-reuse problem: no two different messages
ever share a nonce.

### Decrypt

1. Check the `Magic` prefix (otherwise `ErrNotEncrypted`).
2. `GCM.Open` with `aad = Magic`. A wrong key or tampered bytes fail here.
3. Recompute `HMAC(macKey, plaintext)[:12]` and compare it to the stored nonce in
   constant time (`hmac.Equal`). On a mismatch the file is rejected.

### File format

```
offset  size  field
0       8     Magic   00 4B 49 53 00 76 31 00   "\0KIS\0v1\0"
8       12    nonce   HMAC-SHA256(macKey, plaintext)[:12]
20      n     AES-256-GCM ciphertext
20+n    16    GCM authentication tag
```

The leading NUL byte makes git treat the blob as **binary**, so it never tries
to show a line diff of ciphertext.

Files in a named group (`.env.production @prod`) use format **v2**, which
records the group so the right key is picked on checkout:

```
offset  size  field
0       8     MagicV2 00 4B 49 53 00 76 32 00   "\0KIS\0v2\0"
8       1     g = length of the group name (1–255)
9       g     group name, e.g. "prod"
9+g     12    nonce   HMAC-SHA256(macKey, plaintext)[:12]
21+g    n+16  AES-256-GCM ciphertext and tag, aad = bytes 0..9+g
```

The whole header is the GCM additional data, so relabelling a file's group
fails authentication. Each group has its own random master key (same HKDF
derivation). Default-group files stay **v1**, so a repository that never uses
groups keeps byte-identical ciphertext.

---

## 3. Sharing the key (`internal/keys`, `internal/repo`)

### Identities

Each person brings one private key:

| Type | Source | Notes |
|---|---|---|
| age X25519 | `kis keygen` → `~/.config/kis/identity` (Windows: `%APPDATA%\kis\identity`) | Recommended; file mode `0600` |
| SSH ed25519 / RSA | `~/.ssh/id_ed25519`, `~/.ssh/id_rsa` | Read directly; no ssh-agent. Passphrases are prompted for at unlock |

**One key rule** (`keys.Resolve`). Every command that needs your private key
(`init`, `unlock`, `pubkey`, and `sync` / `add` when they create a group;
`status` uses it too, without the flag) calls the same function, so given the
same flags and environment they all identify you the same way. First match wins:

| # | Source | Holds | Uses |
|---|---|---|---|
| 1 | `--identity PATH` | a path to a private key file | only that file |
| 2 | `KIS_PRIVATE_KEY` | the key's text (age, or SSH without a passphrase) | only that key; key files are ignored |
| 3 | default locations | paths | `$KIS_IDENTITY`, then `~/.config/kis/identity` (Windows `%APPDATA%\kis\identity`), `~/.ssh/id_ed25519`, `~/.ssh/id_rsa` |

A source that is set but unusable (missing file, a variable that isn't a key)
is an error and never falls through to the next step. Commands report the
source (`unlocked as ci (key from KIS_PRIVATE_KEY)`, `(key from --identity
PATH)`, `(key /home/ana/.ssh/id_ed25519)`); `kis pubkey` prints each key with its
source. `kis init` follows the rule too: with `--identity` or
`KIS_PRIVATE_KEY` it uses that key as the first partner (and with the
variable writes no key file); otherwise it offers the default-location keys
or a new age key. Paths are not stored anywhere; only the public key is
([#9](https://github.com/juanfraherrero/keep-in-secret/issues/9)).
Only age X25519, SSH ed25519 and SSH RSA keys are accepted (`keys.ParseRecipient`).
ECDSA, `sk-` (security key) and DSA keys are rejected, because age can't encrypt
to them; `kis init` checks this before writing anything.

### Wrapping

The master key is encrypted with [age](https://age-encryption.org) to each
partner's public key(s), ASCII-armored, and committed:

```
.kis/
├── config.toml        # version, default_key (age | ssh) suggested to new partners
├── recipients.toml    # name → [public keys]
└── keys/
    ├── juan.age       # master key, age-encrypted to juan's key(s)
    └── ana.age        # master key, age-encrypted to ana's key(s)
```

`kis add ana github:ana` fetches `https://github.com/ana.keys` and keeps only the
ed25519 and RSA keys. All of them become ana's recipients, so any one unlocks
(one age stanza per key in `ana.age`). The download is capped at 1 MiB and
happens only at `kis add`; later changes on GitHub are not picked up.

**Groups.** Each group's master key is wrapped the same way, under its own
folder; membership is just which files exist:

```
.kis/keys/
├── juan.age           # default group (everything without @group)
├── ana.age
└── prod/
    ├── juan.age       # prod master key, for juan
    └── deploy.age     # a member of prod only
```

`.gitencrypt` lines ending in `@prod` become `kis-group=prod` in
`.gitattributes`. The clean filter asks git for that attribute to choose the
key; smudge, textconv and the merge driver read the group from the v2 header.
Without a group's key, its files stay ciphertext in your clone, pass through
`git add` unchanged, and never block your other work. `kis sync` creates a
group the first time `.gitencrypt` names it, with you as its member.

### Unlock

`kis unlock` loads the identities the key rule selects, tries each `.kis/keys/*.age`
(and each group's folder), and on success:

1. caches the raw master keys at `.git/kis/key` and `.git/kis/groups/GROUP`
   (mode `0600`, never committed, shared across worktrees through the common
   git dir), dropping keys of groups you are no longer in,
2. wires the filters, merge driver and hook,
3. re-checks-out tracked secrets so the smudge filter turns them into plaintext,
4. re-encrypts any secret that was **staged as plaintext** while the filter was
   missing. A plain `git add` can't be trusted to do it: git skips files whose
   stat info is unchanged since they were staged.

`kis lock` does the reverse: it deletes the cache and checks the files out again
as ciphertext. It refuses if you have uncommitted changes, unless you pass `--force`.

### Revoke and rotate

`kis remove ana` rotates every group ana was in (`--group prod` limits it to
one), and for each:

1. it makes sure working copies are plaintext and have no uncommitted edits,
2. it generates a new master key and re-wraps it for each remaining partner,
3. it runs `git add --renormalize` on the group's secrets (re-encrypting with the new key),
4. it deletes ana's `.age` file there; once she is in no group, also her `recipients.toml` entry.

Other groups' keys and ciphertext are untouched.

Partners who pull get the new `.age` file and the re-encrypted blobs. Their
cached key is now stale, so they run `kis unlock` again to pick up the new one.
The removed partner's keys can't open any `.age` file, so new commits stay
ciphertext for them.

---

## 4. Threat model

**Protected**

- Plaintext never enters git objects, history or the remote.
- Ciphertext is authenticated: tampering is detected on decrypt.
- A stray `git add` while locked fails instead of leaking (`filter.kis.required`,
  clean refuses, plus the pre-commit check).

**Not protected**

- **Past commits.** Anyone who could ever unlock can decrypt old history.
  After `kis remove`, rotate the real credentials too.
- **Metadata.** File names, sizes and *when* a secret changed are visible.
  Determinism also shows whether two versions are identical.
- **Endpoints.** The age identity file and `.git/kis/key` are unencrypted at rest.
  Secrets are only as safe as the least protected laptop that holds a key.

---

## 5. Code map

```
cmd/kis/
  main.go           command table and dispatch
  commands.go       init, keygen, add, remove, rotate, unlock, lock, status, sync
  plumbing.go       hidden commands git calls: clean, smudge, filter-process, textconv, merge, precommit
  ui.go             prompts and output helpers
  script_test.go    end-to-end harness (testscript) and its custom commands
  testdata/script/  one .txtar scenario per user journey
internal/
  agents/           the kis section of AGENTS.md / CLAUDE.md
  crypt/            deterministic AES-256-GCM (SIV-style), file format, v1 fixture
  filter/           clean/smudge rules and the pkt-line filter protocol
  gitencrypt/       .gitencrypt → .gitattributes managed block
  keys/             age/SSH identities, the key rule (Resolve), recipients, github:USER, wrap/unwrap
  repo/             .kis/ state, key cache, file discovery, git wiring and hook,
                    the "safe to push" audit behind `kis status`
  gitx/             git exec helpers and the git version check
test/docker/        test image + runner for the git version / distro matrix
```

Dependencies: `filippo.io/age`, `golang.org/x/crypto/ssh`, `golang.org/x/term`,
`BurntSushi/toml`. Everything else is the Go standard library (`crypto/aes`,
`crypto/cipher`, `crypto/hkdf`, `crypto/hmac`).

---

## 6. Testing

Every layer has its own tests, and CI runs all of them on macOS, Linux and Windows.

| Layer | Where | What it proves |
|---|---|---|
| End-to-end scenarios | `cmd/kis/testdata/script/*.txtar` | Real `kis` + `git` sessions, several people, one shared remote. Each command's output and **exact** file changes are asserted |
| Differential | `internal/gitencrypt/semantics_test.go` | `.gitencrypt` matches what real `git check-ignore` would match |
| Fuzzing | `FuzzDecrypt`, `FuzzRoundTrip`, `FuzzProcess` | Decrypt never accepts forged input; the filter protocol never panics or hangs on malformed input |
| Frozen fixtures | `internal/*/testdata/v1/` | Future versions still decrypt v1 files **byte for byte** and unwrap v1 keys (age and SSH) |
| Git matrix | `test/docker/`, `make test-docker` | The whole suite on git 2.28 → 2.55 (built from source) and on Ubuntu, Debian, Alpine, Fedora |

A scenario reads like a terminal session:

```
as owner                          # own HOME, keys and git identity
cd app
snapshot before
exec kis init -y --name owner
stdout '✓ created \.gitencrypt'
changed before +.gitencrypt +.gitattributes ~.gitignore +.kis/config.toml …
stored HEAD .env                  # what git stores is ciphertext
```

The test binary doubles as `kis`, so git's filters, merge driver and hook call
back into the code under test. Interactive prompts and SSH passphrases run
through a real pseudo-terminal (`ttyin`). The scenario language is documented in
[`cmd/kis/testdata/script/README.md`](../cmd/kis/testdata/script/README.md).
