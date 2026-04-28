# coda-codaclaw

CodaClaw provider plugin for [coda](https://github.com/evanstern/coda).
Implements coda's `session.Provider` interface against the
[CodaClaw](https://github.com/evanstern/codaclaw) container runtime.

## Status

**v0 — scaffolding.** Implementation tracked under coda card #173.
Design spec lives at `docs/specs/173-codaclaw-provider.md`.

## What this provides

A single Go executable (`coda-codaclaw`) that the coda host spawns
once per provider method:

| Subcommand | Maps to |
|------------|---------|
| `start`    | Create CodaClaw session + spawn container |
| `stop`     | Stop container, mark session stopped |
| `deliver`  | Write inbound message to session's `inbound.db` |
| `health`   | Report container + session health |
| `output`   | Drain coda-mediated rows from session's `outbound.db` |
| `attach`   | Inherit TTY, run `pnpm run chat <agent>` |

Full contract: [`evanstern/coda` provider exec contract](https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/providers.md).
Design: [`docs/specs/173-codaclaw-provider.md`](docs/specs/173-codaclaw-provider.md).

## Install

Pre-#173: not yet usable. Once #173 lands, the install path will
mirror other coda plugins:

```bash
coda plugin install git@github.com:evanstern/coda-codaclaw.git
```

## Build

```bash
go build -o bin/coda-codaclaw ./cmd/coda-codaclaw
```

Requires Go 1.22+.

## Repo layout

```
plugin.json                   v3 plugin manifest
cmd/coda-codaclaw/main.go     entry point — switches on argv[1]
docs/specs/                   design specs per focus card
bin/coda-codaclaw             built artifact (gitignored)
```
