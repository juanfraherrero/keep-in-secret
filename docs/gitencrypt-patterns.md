# .gitencrypt patterns

`.gitencrypt` lists the files kis encrypts, in `.gitignore` syntax. The default
covers `.env` files. This page lists the secret files other stacks use, ready
to copy. Look at your own project first: these are common cases, not a
complete list, and your layout may differ.

To use an entry:

1. Copy its lines into `.gitencrypt`.
2. Run `kis sync`. It updates `.gitattributes`, encrypts the matching files in
   git and offers to stage the ones not tracked yet.
3. Run `kis status` and check that the files you expect are listed as
   encrypted.

**Before encrypting a file, check who reads it.** Once a file is encrypted, a
clone without the key (a CI runner, a build server, a new teammate) sees
ciphertext in its place. If CI reads the file, CI has to run `kis unlock` first
(see [CI and cloud agents](../README.md#ci-and-cloud-agents)), or the build
breaks. Coding agents: ask the developer when unsure.

- [Node, Python, Go, Ruby, PHP, Rust (.env)](#node-python-go-ruby-php-rust-env)
- [Android](#android)
- [Rails](#rails)
- [Spring](#spring)
- [Terraform](#terraform)
- [Your own files](#your-own-files)

## Node, Python, Go, Ruby, PHP, Rust (.env)

```gitignore
.env
.env.*
!.env.example
!.env.sample
!.env.template
!.env*.local
```

**Matches:** `.env`, `.env.production`, `.env.staging` and the like, in any
folder. `.env.example`, `.env.sample` and `.env.template` stay readable.

**Why secret:** dotenv loaders (dotenv, python-dotenv, godotenv, dotenvy,
phpdotenv…) read API keys, database passwords and tokens from these files.
The example files hold the variable names with empty or fake values, so new
teammates know what to fill in; they are meant to be read by everyone.

**Local files stay local:** `.env.local`, `.env.development.local`,
`apps/web/.env.local` and every other `.env*.local` hold one developer's
values (their own API URL, personal tokens, debug flags). Next.js and Vite
gitignore them on purpose, so kis doesn't share them, encrypted or not, and
leaves their `.gitignore` rules alone. If a project doesn't gitignore them, the
pre-commit check blocks committing one in plaintext and `kis status` reports
it as an error. To share them encrypted on purpose, delete the `!.env*.local`
line and run `kis sync`.

Names that only look local are still encrypted: `.env.local.bak` (a copy of a
secret), `.env.localhost`, and `.env.local.example` (a template that ends in
`.example`, not `.local`; add `!.env.local.example` to keep one readable).
Case follows git's `core.ignorecase`: `.env.LOCAL` is excluded on macOS and
Windows but encrypted on Linux; the pre-commit check covers it either way.

**CI:** this is the default `.gitencrypt`. Tests that load `.env` need
`kis unlock` on CI; tests that set their variables another way don't.

## Android

```gitignore
*.jks
*.keystore
keystore.properties
```

**Matches:** every Java keystore (`release.jks`, `upload.keystore`) and the
`keystore.properties` file that holds its passwords, in any folder.

**Why secret:** the release keystore signs your app. Whoever has it and its
passwords can sign updates that devices accept as yours.

**CI:** release builds need `kis unlock`. `*.keystore` also matches a
`debug.keystore` committed to the repository, so debug builds on CI need
`kis unlock` too. If your team commits a shared debug keystore on purpose and
it protects nothing, keep it readable by adding a line after the others:

```gitignore
!debug.keystore
```

Keystores are binary: kis encrypts and decrypts them byte for byte, but git
can't merge two edits of one line by line.

## Rails

```gitignore
**/config/master.key
**/config/credentials/*.key
```

**Matches:** `config/master.key` and per-environment keys such as
`config/credentials/production.key`, at the repository root or in any app
folder (`apps/web/config/master.key` in a monorepo).

**Why `**/`:** in gitignore syntax a pattern with a slash in it only matches
from the repository root. `config/master.key` alone would protect a Rails app
at the root and silently skip one in `apps/web/`. `**/` matches at any depth.

**Why secret:** these keys decrypt `config/credentials.yml.enc` (and
`config/credentials/*.yml.enc`), which Rails already commits encrypted. Anyone
with the key reads every credential in them.

**Gitignore:** `rails new` adds `/config/master.key` to `.gitignore`. `kis sync`
offers to un-ignore the key in the repository's root `.gitignore`. In a
monorepo the app usually has its own `.gitignore` (`apps/web/.gitignore`), which
wins over the root one: kis then warns that the key is still ignored, and you
remove the line from that file.

**CI:** anything that boots Rails and reads credentials (tests, asset
precompilation in production mode) needs `kis unlock`.

## Spring

There is no standard file for local secrets in Spring; it depends on your
team's convention. Two common ones:

```gitignore
application-local.properties
application-local.yml
application-local.yaml
```

```gitignore
application-prod.properties
application-prod.yml
application-prod.yaml
```

**Matches:** those profile files in any folder, usually
`src/main/resources/`. The first block is for teams that keep developer
secrets in a `local` profile; the second for teams that keep production
secrets in a `prod` profile. Use the profile names your project actually has
(`application-staging.yml`, `bootstrap-prod.yml`…).

**Why secret:** profile files often hold database passwords, OAuth client
secrets and API keys for that environment.

**CI:** `application.properties` / `application.yml` usually holds the shared,
non-secret defaults that tests read; leave it readable. A profile that CI
activates (`SPRING_PROFILES_ACTIVE`) needs `kis unlock` on CI once its file is
encrypted.

## Terraform

```gitignore
*.tfvars
*.tfvars.json
!*.tfvars.example
```

**Matches:** every variables file (`prod.tfvars`, `terraform.tfvars`,
`secrets.auto.tfvars`, `*.tfvars.json`) in any folder, except
`*.tfvars.example`.

**Why secret:** variables files often hold provider credentials, database
passwords and tokens passed to modules.

**CI:** this covers every tfvars file, also ones with nothing secret in them,
so a pipeline running `terraform plan` or `apply` with them needs `kis unlock`.
To encrypt only some, list them by name instead (`prod.tfvars`,
`**/prod.tfvars`).

State files (`*.tfstate`) also hold secrets in plaintext. They usually live in
a remote backend, not in git; if yours are committed, add `*.tfstate` and
`*.tfstate.*` as well.

## Your own files

Any path works. Examples:

```gitignore
config/secrets.json
appsettings.Development.json
secrets/
!secrets/README.md
```

The gitignore rules that matter:

| Rule | Example | Matches |
|---|---|---|
| No slash: any folder | `secrets.json` | `secrets.json`, `api/secrets.json` |
| A slash at the start or in the middle: from the repository root only | `config/secrets.json` | `config/secrets.json`, **not** `api/config/secrets.json` |
| `**/` matches any depth | `**/config/secrets.json` | both of the above |
| A trailing slash: a folder and everything in it | `secrets/` | `secrets/db.json`, `secrets/a/b.key` |
| `*` within one path segment | `*.pem` | `cert.pem`, `tls/cert.pem` |
| `!` excludes; later lines win | `!secrets/README.md` | keeps that file readable |
| `#` starts a comment | `# prod only` | nothing |

Unlike `.gitignore`, `.gitencrypt` can exclude a file inside a listed folder
(`secrets/` then `!secrets/README.md`).

To encrypt files for some people only, end the line with `@GROUP`. Those files
use that group's key, and partners outside the group keep them as ciphertext
(see [Groups](../README.md#groups-secrets-for-some-people-only)):

```gitignore
.env.production @prod
**/config/credentials/production.key @prod
```

A line takes one group; a `!` line takes none.

---

Missing a stack, or a file that should be here? Open a pull request that adds
a section to this page.
