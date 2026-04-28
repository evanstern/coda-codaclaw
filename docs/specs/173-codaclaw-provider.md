# Card #173 — CodaClaw provider plugin

**Status:** design draft, pre-implementation
**Owner:** Kit (codaclaw orch)
**Reviewer:** Ash (coda orch), Zach (architect) for cross-repo questions
**Tracks:** `evanstern/coda` card #173 → cuts to focus cards in
`evanstern/coda-codaclaw` once approved

## Purpose

Implement coda's `session.Provider` interface against the CodaClaw
container runtime, so coda can drive CodaClaw sessions through the
same surface it uses for any other provider plugin.

## Background

- Coda v3 plugin contract is locked at
  [`evanstern/coda` `docs/plugin-contracts/providers.md`](https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/providers.md)
  and [`docs/plugin-contracts/plugins.md`](https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/plugins.md).
- Six subcommands: `start`, `stop`, `deliver`, `health`, `output`,
  `attach`.
- coda spawns this binary via `os/exec` once per method; non-zero
  exit is an error; stdout is JSON (or session ID string for
  `start`).
- CodaClaw runs a long-lived host process (`pnpm run dev`) that
  manages Docker containers, two SQLite DBs per session
  (`inbound.db`, `outbound.db`), and a delivery loop. The plugin
  binary is a thin client; per-method execs talk to the host.

## Scope

In scope (v0):

1. Six-subcommand executable matching the provider exec contract.
2. ProviderConfig key set CodaClaw needs.
3. Wire-format mapping between coda's typed messages and CodaClaw's
   `inbound.db` / `outbound.db` rows.
4. Lifecycle FSM mapping (coda's
   `created → started → running → stopped` ↔ CodaClaw container
   states).
5. `Output()` cursor-ownership invariant.
6. Attach via inherited TTY.

Out of scope (post-v0, separate cards):

- Typed message round-trip on the wire (v0 flattens to `note`).
- A2A bridging (CodaClaw agent ↔ CodaClaw agent stays inside
  CodaClaw).
- HTTP transport for the host endpoint (v0 is Unix socket only).
- Hot reload, multiple host instances, or cross-host routing.

## Architecture

```
┌─────────┐  os/exec   ┌──────────────────┐   unix sock   ┌──────────────────┐
│  coda   │──────────▶ │  coda-codaclaw   │──────────────▶│ CodaClaw host    │
│  (host) │  argv+stdin│   (this binary)  │   JSON-RPC    │   (long-lived)   │
└─────────┘◀───────────└──────────────────┘◀──────────────└──────────────────┘
            stdout JSON                                              │
                                                                     │ docker/SQLite
                                                                     ▼
                                                            ┌──────────────────┐
                                                            │ container + 2 DBs│
                                                            └──────────────────┘
```

The plugin binary is stateless. Each subcommand opens a fresh
connection to the CodaClaw host's IPC endpoint, sends one request,
reads one response, exits. The host owns all session state; the
plugin is a translator between coda's exec contract and CodaClaw's
internal API.

## Subcommand contract

The host-side contract is fully specified by `providers.md` — this
section maps each subcommand onto CodaClaw's host API.

### `start --agent=<name>` (stdin: ProviderConfig JSON)

1. Plugin parses `ProviderConfig` from stdin.
2. Plugin calls CodaClaw host: `createAgentGroup` →
   `initGroupFilesystem` → `resolveSession` → `wakeContainer`. This
   is the same path `scripts/validate-165.ts` exercised end-to-end.
3. CodaClaw host marks the session row as `coda_managed=true` (or
   equivalent — see Output() invariant below).
4. Plugin prints CodaClaw's session ID to stdout, exit 0.

Session ID format: CodaClaw's native ID (group slug + session
sub-id, e.g. `kit-default-01HXXX`). Returned as-is to coda. No
namespacing, no translation layer.

`start` may take seconds (Docker spawn). Per the contract, the
plugin returns as soon as it has the session ID; the actual
container readiness is observed via `Health()`.

**Failure modes:** if the CodaClaw host is unreachable at
`host_endpoint` (Unix socket missing, host process not running),
the plugin exits non-zero with stderr `host unreachable: <path>`.
coda surfaces this as a transient `Provider.Start` error; the
agent stays in `created` state. Same shape applies to `stop`,
`deliver`, `health`, and `output`: host-unreachable → exit
non-zero, stderr identifies the missing endpoint, coda treats it
as transient. This contract matters for #174 — other plugins
(focus, soul) won't have a host process at all, and #173 should
not implicitly establish "plugin always assumes a host" as a
pattern.

### `stop <sessionID>`

1. Plugin calls CodaClaw host: stop container + mark session
   stopped + cleanup mounts.
2. Plugin exits 0 on success.

CodaClaw's existing teardown already reaps orphans on next host
boot (per #165 finding); stop is best-effort synchronous.

### `deliver <sessionID>` (stdin: session.Message JSON)

1. Plugin parses `session.Message` from stdin.
2. Plugin asks CodaClaw host to write one row to the session's
   `inbound.db`. Body shape — see "Message body encoding" below.
3. Plugin prints `{"delivered": true}` on success, exit 0.

Errors: session not found → exit non-zero with stderr message.
Container not running → still write to `inbound.db` but return
`{"delivered": true}` (CodaClaw's poll-loop picks it up on next
wake; matches CodaClaw's existing semantics).

### `health <sessionID>`

1. Plugin calls CodaClaw host for session + container status.
2. Plugin prints `{"State": "...", "Healthy": <bool>, "Detail": "..."}`,
   exit 0.

Mapping:

| CodaClaw state | coda State | Healthy | Detail |
|---|---|---|---|
| session created, no container | `created` | `false` | `"awaiting wakeContainer"` |
| `wakeContainer` called, Docker spawn pending | `started` | `false` | `"container starting"` |
| container running, heartbeat current | `running` | `true` | `""` |
| container exited normally | `stopped` | `false` | `"exited normally"` |
| container crashed | `stopped` | `false` | `"crashed: <reason>"` (see taxonomy) |
| host process unreachable | (last known) | `false` | `"host unreachable"` |

`Detail` is user-facing — coda surfaces it in `coda agent ls`. Keep
strings short and stable.

**Crash reason taxonomy** (canonical set, not freeform):

| `<reason>` | Meaning |
|---|---|
| `oom` | Container killed by OOM-killer |
| `exit-code-N` | Container exited non-zero with code `N` (e.g. `exit-code-137`) |
| `host-killed` | Host process killed the container outside `Stop()` (sweep, manual) |
| `unknown` | Container died without a recognized signal |

Plugin maps Docker exit info onto these strings; CodaClaw host's
container-runtime layer is the source of truth.

### `output <sessionID> [--since=<RFC3339>]`

1. Plugin calls CodaClaw host: read `outbound.db` rows for this
   session where:
   - `created_at > since` (or all rows if `--since` omitted)
   - row is **coda-mediated** (see Output() invariant)
2. Plugin returns JSON array of `session.Message`.

**Output() cursor-ownership invariant** (load-bearing):

> For any session running under the coda provider, coda owns the
> cursor on `outbound.db` for coda-mediated rows. CodaClaw host-side
> delivery MUST NOT also drain those rows.

If both drain, double-delivery is guaranteed.

The invariant is implemented by **two orthogonal flags** that
answer different questions:

- `channel_type='coda'` (per-row, on `inbound.db`/`outbound.db`):
  **wire-level row discriminant.** Identifies an individual
  message as coda-mediated. Sits in the same taxonomy as
  CodaClaw's existing `agent`/`slack`/`discord` values.
- `coda_managed=true` (per-session, on the session row):
  **host-side delivery filter.** Tells CodaClaw's own delivery
  loop to defer to coda for this session's `channel_type='coda'`
  rows.

The two compose. A `coda_managed=true` session can still receive
A2A messages (`channel_type='agent'`) from another CodaClaw agent;
those rows are drained normally by CodaClaw's delivery loop. coda's
`Output()` filters strictly on `channel_type='coda'`, never on
`coda_managed`. Different lifetimes, different scopes, both needed.

A2A traffic (`channel_type='agent'`, CodaClaw agent ↔ CodaClaw
agent) is **invisible to coda by design** (per Ash). Two reasons:

1. coda has no routing context for A2A endpoints — both sides may
   not be coda-registered agents.
2. Bridging A2A through coda turns the provider into a proxy for
   runtime-internal traffic, which is exactly the boundary the
   provider abstraction exists to keep clean.

If a coda-managed agent wants to talk to another coda-managed agent
that happens to be CodaClaw-hosted, the path is:
`coda send` → coda routes via recipient's agent record →
`Provider.Deliver` on recipient → `inbound.db`. Same wire, just
through coda's routing.

### `attach <sessionID>`

Maps to CodaClaw's existing chat REPL: `pnpm run chat <agent>`.

**Required mechanism: inherited TTY.** The plugin process inherits
stdin/stdout/stderr from coda's controlling terminal and execs a
child process with those streams attached, same pattern `git
commit` uses to launch `$EDITOR`. Block until the user disconnects.

**Coda-side dependency: resolved.** Earlier drafts of this spec
flagged `evanstern/coda` `internal/plugin/provider.go:162-165` as
a blocker — the original `SubprocessProvider.Attach` used
`runJSON(...)` which captured stdout/stderr into `bytes.Buffer`
and swallowed the chat REPL output. That was tracked as
`evanstern/coda` #199, merged at `fc5c94b` on main: Attach now
inherits stdio unconditionally (no `tty.IsTerminal` fallback,
matching the `git commit` → `$EDITOR` pattern). Step 4 of the
implementation plan is unblocked.

## ProviderConfig keys

`session.ProviderConfig` is `map[string]string` (per
`evanstern/coda` `internal/session/provider.go:43`). All values are
strings; structured types serialize to comma-separated strings.

| Key | Required | Default | Description |
|---|---|---|---|
| `image` | no | `nanoclaw-agent-v2:latest` | Docker image tag for the agent container |
| `host_endpoint` | no | `~/.codaclaw/host.sock` | Unix socket path to the CodaClaw host. v0 is Unix-socket-only; HTTP override is a v1 follow-up. |
| `mount_allowlist` | no | `""` | Comma-separated host paths the agent is allowed to mount (CodaClaw enforces) |
| `personality_dir` | no | `""` | Path to personality config directory mounted into the container at boot (e.g. `~/.config/coda/personalities/<agent>`) |
| `agents_md_path` | no | `""` | Optional path to project AGENTS.md, mounted into the container |
| `anthropic_base_url` | no | inherited from CodaClaw host env | URL for CLIProxyAPI or direct Anthropic API |
| `anthropic_api_key_env` | no | `ANTHROPIC_API_KEY` | **Name of the env var** holding the API key on the host. Never the key itself — secrets do not belong in `ProviderConfig`, which serializes into `coda.db`. |
| `group_slug` | no | derived from agent name | Override for the CodaClaw group slug |

Format notes:

- `mount_allowlist`: comma-separated. Spaces around commas
  ignored. Empty string = no extra mounts.
- All paths support `~/` expansion at the plugin layer.

## Installation

Per `evanstern/coda` `docs/plugin-contracts/plugins.md:74-78`:

> `install` is a path (relative to plugin root) the user can run
> after dropping the plugin directory in place. **The host does
> not run it automatically.**

So the plugin ships its own install script. `plugin.json` declares
`"install": "scripts/install.sh"`; the user runs `bash
scripts/install.sh` after placing the plugin directory under
`$XDG_CONFIG_HOME/coda/plugins/coda-codaclaw/` (or after `coda
plugin install` clones it — that command is a v1 follow-up; v0 is
manual clone).

`scripts/install.sh` responsibilities:

1. `go build -o bin/coda-codaclaw ./cmd/coda-codaclaw`
2. Optional: check that Docker is on `PATH` and the CodaClaw host
   is reachable at the default socket path. Warn-only; do not
   block install.
3. Exit 0 on success, non-zero on build failure.

The build artifact lives at `bin/coda-codaclaw`, which the
`provides.providers.codaclaw.exec` field in `plugin.json` already
points at.

## Message body encoding

### Storage vs wire

- **Storage (coda side):** coda's typed messages
  (`note/brief/completion/status/escalation`) persist with their
  real type in `coda.db`. `coda send -type brief` from ash to a
  CodaClaw-hosted agent stores `type=brief`.
- **Wire (across the provider boundary):** v0 flattens everything
  to `type=note`. Body is the raw text bytes (base64-encoded as
  required by `session.Message.Body []byte` over JSON).
- **Asymmetry is accepted:** an `Output()` response on the return
  path becomes `type=note` in `coda.db`. Wire is lossy in v0; store
  is not.

This matches the #165 substrate-first pattern: prove the round-trip
on plain text first; layer typed semantics in v1.

### v1 typed-message plan (out of scope, sketched for context)

The agent-runner emits structured response blocks:

```
<coda type="completion">…</coda>
<coda type="escalation">…</coda>
```

The plugin parses those blocks in `output` and sets the appropriate
`Message.Type`. This is a CodaClaw-side card (agent-runner change)
plus a plugin-side parser; both follow #173.

### Inbound (coda → CodaClaw)

`Deliver` writes one row to `inbound.db` with:

- `channel_type = "coda"` (new value, distinct from
  `agent`/`slack`/`discord`)
- body = JSON envelope: `{"type": "<coda type>", "from": "<sender>",
  "body": "<utf-8 text>"}`. The agent-runner prompt template is
  extended (separate CodaClaw card) to recognize `channel_type=coda`
  and surface sender/type to the agent.

### Outbound (CodaClaw → coda)

`Output` reads `outbound.db` rows where:

- `channel_type = "coda"` (set by the agent-runner when responding
  to a coda-channel inbound message)

Returns `session.Message` array with:

- `From`: agent name
- `To`: original sender (parsed from inbound envelope context)
- `Type`: `"note"` (v0)
- `Body`: raw text bytes
- `CreatedAt`: row's `created_at`

## Lifecycle FSM mapping

Coda's session FSM (`internal/session/session.go:17`):
`created → started → running → stopped`. Transitions enforced by
`Store.TransitionSession`.

CodaClaw container states: `pending → running → exited|crashed`.

Mapping:

| coda state | CodaClaw state | When coda's FSM advances |
|---|---|---|
| `created` | session row exists, no container | after `Provider.Start` returns the ID |
| `started` | `wakeContainer` called, Docker spawn pending | first `Health()` call after Start |
| `running` | container reports healthy heartbeat | `Health()` returns `Healthy=true` |
| `stopped` | container exited / crashed / `Stop` called | `Health()` returns `State=stopped` or `Stop()` called |

**Transitions emit via `Health()` polling, not `Output()` events.**
This keeps the message stream and lifecycle stream separate. coda
calls `health` on its own cadence and advances the FSM based on the
returned state.

`StopReason` (in coda's session row) takes the value from
`Status.Detail` on the `stopped` transition: `"exited normally"`,
`"crashed: <reason>"`, `"stopped via Provider.Stop"`. coda surfaces
this in `coda agent ls` and elsewhere — treat strings as
user-facing and stable.

## Repo location

Decision: standalone `evanstern/coda-codaclaw` repo (not a
subdirectory of `evanstern/codaclaw`). Recorded by Zach in
`wiki/decisions/coda-codaclaw-repo-location.md` (in zach's orch
config dir). Four reasons in Zach's weight order: convention
(every coda plugin is its own repo), toolchain hygiene (Go +
TypeScript in one repo = permanent friction), interface churn cuts
the right way, release independence.

This repo is `evanstern/coda-codaclaw`. Initial scaffold lands on
main; this spec lands on branch `173-codaclaw-provider-spec`.

## Implementation plan (post-spec-approval)

Cut into focus cards once ash signs off. Suggested sequence:

1. **Host-side IPC endpoint** (CodaClaw repo): expose
   `start/stop/deliver/health/output` over Unix socket. Agent-runner
   stays unchanged.
2. **Plugin scaffold subcommands** (this repo): wire each subcommand
   to the IPC client. Hardcoded host-endpoint default; ProviderConfig
   keys threaded through.
3. **Coda-side attach patch — DONE.** Tracked as `evanstern/coda`
   #199, merged at `fc5c94b` on main. `SubprocessProvider.Attach`
   now inherits stdio (no `runJSON` capture). Unblocks step 4.
4. **Plugin attach** (this repo): `exec.Command("pnpm", "run",
   "chat", agent)` with TTY inherit.
5. **Cursor-ownership flag** (CodaClaw repo): `coda_managed` field
   on session row + delivery-loop filter that skips
   `channel_type='coda'` rows for those sessions. **Precondition,
   not contingency** — double-delivery is invisible until it
   isn't, and adding the flag is cheaper than chasing duplicate-
   message reports later.
6. **End-to-end smoke**: register coda-codaclaw via manual install,
   create an agent with `provider=codaclaw`, run `coda send` and
   verify round-trip. Mirror of #165 round-trip but through the
   provider abstraction. Depends on step 5 landing first.

## Resolved questions

All three opens from the first draft are now closed (ash, msg #45
+ #46):

1. **`channel_type='coda'` per-row + `coda_managed` per-session
   are orthogonal and both stay.** Wire-level row discriminant vs
   host-side delivery filter; different lifetimes, different
   scopes. Documented under Output() invariant.
2. **ProviderConfig key naming: unprefixed.** `host_endpoint`,
   `image`, etc. ProviderConfig is per-agent and an agent has
   exactly one provider, so collision is impossible by
   construction. Ash is adding a one-paragraph naming convention
   to coda's `docs/plugin-contracts/providers.md` as a small
   follow-up — not a blocker.
3. **Plugin install script builds the binary.** Per
   `plugins.md:74-78` the host does not run install automatically.
   See "Installation" section above.

## References

- coda provider exec contract: [`docs/plugin-contracts/providers.md`](https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/providers.md)
- coda plugin manifest: [`docs/plugin-contracts/plugins.md`](https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/plugins.md)
- coda Provider interface: `internal/session/provider.go`
- coda SubprocessProvider: `internal/plugin/provider.go`
- coda attach patch (#199, merged at `fc5c94b`): unblocks attach
  implementation in step 4
- CodaClaw round-trip prior art: `scripts/validate-165.ts`
  (in `evanstern/codaclaw` main)
- Bus thread: msg #33 (ash → kit), #34 (kit reply), #36 (ash
  review), #39 (zach Q4 verdict), #41 (kit URGENT re attach
  gap), #42 (ash takes #199), #44 (kit pings spec ready), #45
  + #46 (ash review of spec), #47 (#199 merged)
