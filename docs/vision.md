# Vision

> Secrets live with the code, encrypted. Only public keys ever travel.
> Nobody — human or agent — has to send, paste or hand-copy a secret again.

## The problem

Every project has secrets: database passwords, API tokens, signing keys. The
code that uses them lives in git, but the secrets themselves live nowhere in
particular:

- **They travel through the wrong channels.** A new teammate asks for the
  `.env` and gets it over Slack, email, a DM or a screenshot. Those messages are
  stored, indexed, backed up and forwarded, long after anyone remembers them.
- **They drift.** Everyone has a slightly different `.env`. Someone adds a
  variable, forgets to tell the team, and the app breaks on every other laptop
  with an error nobody can explain.
- **Onboarding is a ritual.** "Clone the repo" takes a minute; "get the
  secrets" takes a day of asking around.
- **Offboarding is guesswork.** When someone leaves, nobody knows which secrets
  they had, so nobody knows what to rotate.

Coding agents make all of this worse:

- Agents are trained to **never commit `.env`**, so they `.gitignore`, delete or
  "fix" secret files, and the team's only shared copy disappears.
- Cloud agents (Codex, Claude Code on the web, Copilot coding agent) start from
  a **fresh clone with nothing in it**. The easiest way to give them secrets is
  pasting values into a prompt or a platform's variable screen, one more copy in
  one more place.
- Agents read files, print logs and write PR descriptions. A secret that is
  lying around in plaintext will eventually be copied somewhere it shouldn't be.

## The idea

Put the secrets **in the repository, encrypted**, and let git do the rest.

- You list secret files in `.gitencrypt`, with the same syntax as `.gitignore`.
- `git add` encrypts them and `git checkout` decrypts them. Your working copy is
  plaintext, so your app, your tools and your agents just read `.env`, while git
  history and the remote only ever hold ciphertext.
- Access is granted with a **public key**. A public key is safe to send over any
  channel, post in a PR or fetch from `github.com/USER.keys`. The private key
  never leaves the machine that owns it.
- Revoking access is one command, and it rotates the key for everyone else.

The repository becomes the single source of truth for code *and* configuration.
Clone, unlock, run.

## Why it helps everyone's workflow

| Who | Today | With kis |
|---|---|---|
| **New teammate** | Asks around for the `.env`, gets an outdated one | `kis keygen`, sends the public key, `git pull && kis unlock` |
| **Owner / lead** | Copies secrets into chats, loses track of who has what | `kis add ana github:ana`; `.kis/recipients.toml` is the list of who has access, reviewed like code |
| **Whole team** | `.env` drift, "works on my machine" | One versioned `.env`; changes arrive with `git pull`, merge like text, show in `git log -p` |
| **Offboarding** | Hope nobody kept a copy | `kis remove ana` rotates the key; the warning says which credentials to change |
| **CI** | Secrets duplicated into each platform's settings | One `KIS_PRIVATE_KEY` per machine; everything else comes from the repo |
| **Coding agents** | Delete or ignore `.env`, or get secrets pasted into prompts | `AGENTS.md` tells them the files are safe to commit; `kis status --check --json` tells them, as data, whether a push is safe |
| **Security** | Secrets scattered across chats, wikis and laptops | Plaintext never reaches git; access is a reviewable list of public keys |

## Principles

1. **Git-native, no server.** No vault to run, no SaaS account, no network call
   at runtime. If you have git, you have the whole system. Each person installs
   `kis` once, like `git-lfs`.
2. **Only public keys travel.** Every onboarding step can happen over an
   insecure channel, because nothing secret is in it.
3. **Fail closed.** When kis can't encrypt, git fails rather than storing
   plaintext (`filter.kis.required`, the clean filter refusing while locked, the
   pre-commit check). A locked clone holds only ciphertext.
4. **Invisible when it works.** Plain `git add / commit / push / pull`. No new
   workflow to learn, no wrapper commands for daily work.
5. **Built for agents as well as people.** Every command works without a
   terminal, every state can be read as JSON, and every error names the command
   that fixes it. An agent should never have to guess, and never have to work
   around kis.
6. **Least privilege by default.** Groups keep production secrets for the
   people who need them; each CI runner or cloud agent is its own partner, so a
   leak costs one `kis remove`.
7. **Nothing executable in the repository.** Git wiring lives in each clone's
   `.git/config`, which is never committed. Cloning a kis repo can't run code.
8. **Honest about limits.** kis vouches for the files in `.gitencrypt`, not for
   a token hardcoded elsewhere. It protects secrets at rest in git, not on an
   unlocked laptop. Past commits stay readable to anyone who once had access,
   so revoking access also means rotating the real credentials. kis says so
   every time it matters.

## What kis is not

- **Not a runtime secret manager.** It does not inject secrets into processes,
  issue short-lived credentials or audit reads. Pair it with a vault when you
  need those.
- **Not a secret scanner.** Use gitleaks or GitHub secret scanning for secrets
  in files `.gitencrypt` doesn't list.
- **Not a sandbox for agents.** An agent working in an unlocked clone can read
  the plaintext there. kis keeps agents from leaking secrets through git, and
  groups plus per-machine keys limit what an agent can unlock in the first
  place.

## Where it is going

The north star is that, in a repository using kis, **the safe path is the
only path**: for a person who forgets a step, for an agent that has never seen
kis before, and for a cloud machine that clones without any key. Every gap
found where plaintext can still slip through, or where a stale key keeps
working after a rotation, is a bug against this vision, not an edge case.
