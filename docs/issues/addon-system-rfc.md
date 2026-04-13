# RFC: sciClaw Addon System, Webtop & Jupyter Reference Addons

Status: Draft
Date: 2026-04-13
Author: Ernie + sciClaw

## Problem

sciClaw's surface is growing past what should live in one binary. Three concrete pressures:

1. **Browser desktops for collaborators.** Scientists outside the host network need a way to *open* the files the agent edits, not just chat about them. A webtop (Ubuntu XFCE in a container) solves this — but pulling Docker orchestration, Caddy, cloudflared, and selkies-gstreamer into `sciclaw` core bloats the binary and forces every install to carry the dependency.
2. **Third-party extensions are already being asked for.** Labs want Jupyter runners, LSF submitters, MATLAB license proxies, domain-specific skills — all things that could ship as optional plugins. Today there is no contract for how an extension plugs in.
3. **Laptop vs. server installs have diverged.** Laptop users want a tight single binary with Discord and local skills. Server installs want Docker-based infrastructure, multi-user routing, and web desktops. One codebase cannot elegantly cover both unless the server-only pieces are out-of-core.

The failure mode if we don't fix this: every new capability becomes a conditional in `cmd/picoclaw/main.go`, the binary accretes 40MB of Docker SDK dependencies that 80% of users never touch, and third-party labs can't extend sciClaw without forking it.

## Goals

1. **Modular extension surface.** Optional capabilities (webtop, custom runners, specialized skills) ship as separate installable addons that core sciClaw has zero knowledge of at build time.
2. **Zero cost when unused.** Laptop users who don't install the webtop addon pay nothing — no Docker dependency, no unused CLI commands, no dead code in the binary.
3. **Uniform UX for enabled addons.** An addon's CLI commands, Web UI tabs, and config surfaces feel like part of sciClaw — not a separate tool.
4. **Path-based routing for webtop.** Single domain (`sciclaw.dev/alice`, `sciclaw.dev/bob`) not wildcard subdomains. Verified compatible with selkies-gstreamer's `SUBFOLDER` mode.
5. **Hook-based coupling, not compile-time coupling.** Addons subscribe to events (routing changes, user added, profile updated) rather than importing core packages.
6. **Third-party addons possible.** The contract (manifest, HTTP API, hook dispatch) is public and stable, so a lab can ship `sciclaw-addon-jupyter` without coordinating with sciclaw core.

## Non-Goals

- Replace the existing `pkg/skills/` system for workspace-scoped Markdown skills. Skills stay as-is — addons are a layer up, for capabilities that need processes, containers, or out-of-core dependencies.
- Hot-reload addons without process restart. Enable/disable require a sciClaw restart in v1.
- Sandbox untrusted addons. Addons run with the same privileges as sciClaw itself. Trust is the user's decision at install time, same as `brew install`.
- Solve cross-platform container orchestration. Webtop addon targets Linux + macOS (Docker Desktop / OrbStack); Windows is out of scope for now.
- Provide a plugin marketplace or addon registry. v1 addons are installed from git URLs.

## Proposed Design

Two layers, built and shipped independently:

**Layer 1: `pkg/addons/` in sciClaw core.** A small addon runtime that discovers, installs, enables, and dispatches events to addons. ~600 lines of Go. Ships with sciClaw itself, contributes zero dependencies when no addons are enabled.

**Layer 2: `sciclaw-addon-webtop` as a separate repo.** The reference addon. Proves the contract works end-to-end. Bundles Caddy, cloudflared, and the webtop container as a docker-compose stack plus a small Go sidecar that talks to sciClaw core over a Unix socket.

### 1. Addon Manifest

Every addon ships an `addon.json` at its repo root. Core sciClaw reads this; the addon itself reads nothing from core.

```json
{
  "name": "webtop",
  "version": "0.1.0",
  "description": "Per-user browser desktops with shared workspace mounts",
  "author": "sciclaw",
  "homepage": "https://github.com/sciclaw/sciclaw-addon-webtop",

  "requires": {
    "sciclaw": ">=0.3.0",
    "runtime": ["docker", "podman"],
    "platform": ["linux", "darwin"]
  },

  "sidecar": {
    "binary": "sciclaw-addon-webtop",
    "socket": "sock",
    "start_timeout_seconds": 10,
    "health_path": "/health"
  },

  "provides": {
    "ui_tab": {
      "name": "Desktops",
      "icon": "desktop",
      "path": "/ui"
    },
    "cli_group": "webtop",
    "hooks": ["routing_changed", "user_added", "user_removed", "profile_updated"],
    "config_schema": "schema.json"
  },

  "bootstrap": {
    "install": "./bin/install.sh",
    "uninstall": "./bin/uninstall.sh"
  },

  "compose": "compose.yaml"
}
```

**Required fields**: `name`, `version`, `requires.sciclaw`, `sidecar.binary`.
**Everything else is optional.** An addon that only adds CLI commands needs no `ui_tab`. An addon that needs no Docker omits `compose` and `requires.runtime`.

Manifest schema lives in `pkg/addons/manifest.go`; parser validates on install and on every startup.

### 2. Addon Lifecycle

Five states, managed by `pkg/addons/registry.go`:

```
    ┌─────────────┐  install   ┌──────────┐  enable   ┌─────────┐
    │ not_present │──────────▶ │ installed│──────────▶│ enabled │
    └─────────────┘            └──────────┘           └─────────┘
                                     ▲                     │
                                     │       disable       │
                                     └─────────────────────┘
                                                                 
         ▲                           │
         │      uninstall            │
         └───────────────────────────┘
```

**Install** (`sciclaw addon install <git-url>` or `sciclaw addon install <name>` from a curated list):
1. Clone repo to `~/sciclaw/addons/<name>/`
2. Parse and validate `addon.json`
3. Check `requires.sciclaw` against current version, `requires.runtime` against `docker`/`podman` binaries on PATH, `requires.platform` against `runtime.GOOS`
4. Run `bootstrap.install` if present (e.g., `docker pull` base images)
5. Register in `~/sciclaw/addons/registry.json` as `installed`

**Enable** (`sciclaw addon enable <name>`):
1. If `compose` is present, run `docker compose up -d` in the addon directory
2. Start the sidecar binary, wait for `health_path` to return 200 (timeout: `sidecar.start_timeout_seconds`)
3. Mark `enabled` in registry
4. Tell the running sciClaw gateway to reload addons (SIGHUP or reload endpoint)

**Disable** (`sciclaw addon disable <name>`):
1. Stop the sidecar (SIGTERM, then SIGKILL after 5s)
2. If `compose` is present, run `docker compose stop` (not down — preserve volumes)
3. Mark `installed` in registry
4. Gateway reloads, UI tab disappears

**Uninstall** (`sciclaw addon uninstall <name>`):
1. Must be disabled first, or refuse unless `--force`
2. Run `bootstrap.uninstall` if present
3. `docker compose down -v` to reclaim volumes
4. Remove `~/sciclaw/addons/<name>/` directory
5. Remove from registry

**Upgrade** (`sciclaw addon upgrade <name>`):
1. `git pull` in addon directory
2. Re-parse manifest, re-check requirements
3. If sidecar protocol version changed, warn and require explicit confirmation
4. Restart sidecar

### 3. Sidecar Protocol

Addon sidecars are HTTP servers bound to a Unix socket at `~/sciclaw/addons/<name>/sock`. Core sciClaw speaks to them over that socket. Benefits: no TCP ports, OS-level permissions control access, addon can be killed/restarted independently of gateway.

**Required endpoints** every addon must implement:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Readiness check. Returns 200 when addon is ready to receive hooks. |
| `GET` | `/version` | Returns addon version + sidecar protocol version for compatibility check. |
| `POST` | `/hook/<event>` | Called by core on subscribed events (body: JSON event payload). Addon returns 200 or error. |
| `POST` | `/shutdown` | Graceful shutdown. Addon should stop accepting new work, drain, exit. |

**Optional endpoints**:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/ui/*` | Serves the addon's Web UI. Core proxies requests from `web/addons/<name>/*` here. |
| `POST` | `/cli` | Receives CLI subcommand invocations. Body: `{args: string[], stdin: string}`. Returns `{stdout, stderr, exit_code}`. |
| `GET` | `/status` | Returns addon-specific status for the `sciclaw addon status` command. |

**Sidecar protocol version**: every sidecar declares `protocol_version` in `/version` response. Core refuses to enable addons with incompatible protocols. Major version bumps are breaking.

### 4. Hook Dispatch

sciClaw core emits events at well-defined points. Addons subscribe by listing event names in `provides.hooks`. Core POSTs the event payload to every subscribed addon's `/hook/<event>` endpoint, in parallel, with a 5-second per-addon timeout. Failures are logged but do not block the core operation.

**v1 hook catalog**:

| Event | Payload | Fired when |
|---|---|---|
| `routing_changed` | `{rules: RoutingRule[]}` | Routing rules add/remove/modify |
| `user_added` | `{sender_id, display_name, channels: []}` | New identity first seen |
| `user_removed` | `{sender_id}` | Identity removed from all routing |
| `profile_updated` | `{sender_id, profile: UserProfile}` | `/theme` or other profile change |
| `workspace_changed` | `{path, op: "created"|"modified"|"deleted"}` | Files change in workspace (debounced) |
| `session_ended` | `{session_id, sender_id}` | Chat session ends |

Hooks are **fire-and-forget from core's perspective**. If the webtop addon wants to restart a container when routing changes, that's its problem — core returns to the user immediately.

Hook handlers must be idempotent: core may replay events on startup if the addon was down when they fired.

### 5. UI Integration

When an addon declares `provides.ui_tab`, core sciClaw's Web UI dynamically adds a tab to the top navigation. Clicking the tab loads an iframe pointing at `/addons/<name>/ui/`, which core reverse-proxies over the Unix socket to the addon's `/ui/*` endpoint.

```
Browser
   │
   ▼ GET /addons/webtop/ui/index.html
sciclaw web (port 8080)
   │
   ▼ proxy over unix socket
~/sciclaw/addons/webtop/sock
   │
   ▼ GET /ui/index.html
sidecar (sciclaw-addon-webtop)
   │
   ▼ serves its own React bundle
```

The addon owns its own frontend entirely — React, Vue, plain HTML, whatever. It ships pre-built static assets in its repo. Core sciClaw's Web UI has zero build-time knowledge of addon UIs.

**Tab injection** happens at web UI load time. The React app calls `GET /api/addons/enabled` on mount, gets back a list of `{name, ui_tab}` entries, and renders tabs dynamically. Disabled addons vanish on next reload.

**Cross-tab communication**: addons can read core's identity list, routing rules, and profile data via authenticated API calls back to core. Contract: `GET /api/core/routing`, `GET /api/core/users`, `GET /api/core/profile/:id`. Core never pushes data to addons except via hooks.

### 6. CLI Integration

When an addon declares `provides.cli_group`, core sciClaw registers a top-level CLI subcommand group. `sciclaw webtop ...` dispatches to the webtop addon's sidecar via `POST /cli`.

```
$ sciclaw webtop list
   │
   ▼
cmd/picoclaw reads registry, finds webtop enabled
   │
   ▼ POST /cli {args: ["list"], stdin: ""}
sidecar handles, returns {stdout: "alice  running  2h\nbob  idle  10m", exit_code: 0}
   │
   ▼ core prints stdout, exits with code
```

The addon's CLI completion is its own responsibility — it ships shell completion scripts in its repo if desired.

### 7. Security Model

**Threat model**: addons are trusted code the user chose to install. sciClaw does not sandbox them. But we want to prevent accidents and misconfiguration.

**Enforced**:

- Addons run under the same user as sciClaw (no privilege escalation)
- Unix socket permissions: `0600`, owned by the sciClaw user
- Addon bootstrap scripts must not be setuid
- Core refuses to enable an addon whose directory is world-writable
- Addon manifest must declare `requires` explicitly; core refuses to install if any required binary is missing
- Core logs every addon hook invocation with timestamp, event, latency, outcome

**Not enforced**:

- Addons can read/write any file sciClaw can
- Addons can issue Docker commands if Docker is installed
- Addons can open network connections freely

**Install trust**: `sciclaw addon install` from a git URL prints the manifest and the bootstrap script contents, requires `yes` confirmation, and logs the SHA of the installed commit. Upgrades show a diff of bootstrap scripts and require re-confirmation if they changed.

### 8. Version Pinning and Integrity

**Policy summary**: pin to exact commit SHA at install time, verify integrity on every startup, require explicit confirmation on upgrades. This is Go modules' approach with Homebrew's signed-tag preference and Nix's content-hash verification.

#### Install resolution

The `sciclaw addon install` command resolves a user-supplied reference to an exact commit SHA, then records that SHA and cryptographic hashes of critical files in the registry. Subsequent operations verify against the recorded values.

Resolution order when the user runs `sciclaw addon install <ref>`:

1. **`--commit=<sha>`** — pin directly to that commit. No resolution.
2. **`--version=v0.1.0`** — look for a matching tag in the repo. Prefer signed tags (verify signature if GPG key is known). Pin to the tag's resolved commit SHA.
3. **No version specified** — fetch the latest signed tag on the default branch. If none exists, refuse with:
   ```
   Error: no signed tags found in <repo>. Specify --version, --commit,
   or --track=main to track an unsigned branch.
   ```
4. **`--track=<branch>`** — opt-in to branch tracking. Records `"track": "main"` in the registry but still pins a SHA at install time. Upgrades follow the branch head.

The `--track` path is flagged as "not recommended for production" in CLI output, same as `cargo` flags path-based and git-branch dependencies.

#### Registry lockfile fields

Every installed addon's registry entry includes:

```json
{
  "webtop": {
    "version": "0.1.0",
    "installed_at": "2026-04-13T14:22:00Z",
    "installed_commit": "abc123def456789...",
    "manifest_sha256": "a1b2c3...",
    "bootstrap_sha256": "d4e5f6...",
    "sidecar_sha256": "9876ab...",
    "state": "enabled",
    "source": "https://github.com/sciclaw/sciclaw-addon-webtop",
    "track": null,
    "signed_tag": "v0.1.0",
    "signature_verified": true,
    "previous_commit": null
  }
}
```

- `installed_commit` — exact git SHA of the installed addon directory
- `manifest_sha256` — SHA256 of `addon.json` contents at install time
- `bootstrap_sha256` — SHA256 of `bootstrap.install` script contents (or the directory hash if it's a path)
- `sidecar_sha256` — SHA256 of the sidecar binary if prebuilt; null if built from source
- `track` — `"main"` if opted-in to branch tracking, otherwise `null`
- `signed_tag` — name of the signed tag this install was resolved from, or `null`
- `signature_verified` — whether the tag signature was verified at install (requires GPG keyring)
- `previous_commit` — for rollback; one level of history kept

#### Integrity verification on startup

Every time sciClaw core starts, before enabling any addon:

1. Read registry entry for the addon
2. Verify `.git/HEAD` resolves to `installed_commit`
3. Compute SHA256 of the addon's `addon.json` and compare to `manifest_sha256`
4. Compute SHA256 of the bootstrap script and compare to `bootstrap_sha256`
5. Compute SHA256 of the sidecar binary and compare to `sidecar_sha256` if set
6. If any check fails, **refuse to enable the addon** and log:
   ```
   error: addon "webtop" integrity check failed
     expected installed_commit: abc123def456
     actual HEAD:                999zzzyyy888
   Run 'sciclaw addon verify webtop' for details or
   'sciclaw addon upgrade webtop' to accept changes.
   ```

This catches three failure modes cheaply:
- User ran `git pull` in the addon directory manually (accidental drift)
- Malicious local actor modified the sidecar binary
- Supply-chain attack that rewrote history after install (detected on next start)

#### Upgrade flow

`sciclaw addon upgrade <name>`:

1. `git fetch` in the addon directory (does not modify working tree)
2. Resolve new target SHA using the same rules as install:
   - If installed with `--version`, look for the latest signed tag newer than current
   - If installed with `--track`, use branch head
   - If installed with `--commit`, require explicit new `--commit` or `--version`
3. If a signed tag is the target, verify signature; refuse on failure
4. Compute new hashes; compare to registry
5. Print a diff:
   ```
   webtop 0.1.0 → 0.2.0  (commit abc123 → def456)
     Signed: yes (signature verified)

     manifest: changed
       + provides.hooks: "session_ended"
       - requires.sciclaw: ">=0.3.0" → ">=0.4.0"

     bootstrap: unchanged
     compose.yaml: changed (+3 -1 lines)
     sidecar binary: rebuilt (sha256: 9876ab → 5432cd)

   Proceed? [y/N]
   ```
6. On confirm:
   - Record current `installed_commit` as `previous_commit` for rollback
   - `git checkout <new-sha>`
   - Re-run requirement checks (`requires.sciclaw`, `requires.runtime`)
   - Run `bootstrap.upgrade` if present
   - Update all `*_sha256` fields atomically in registry
   - If addon was enabled, restart sidecar

#### Rollback

`sciclaw addon rollback <name>`:

1. Look up `previous_commit` in registry
2. Refuse if `null`
3. `git checkout <previous_commit>`
4. Re-verify integrity against a saved snapshot of the previous registry entry
5. Restart sidecar

Only one level of rollback history is kept. Users who need deeper history can use git directly (`cd ~/sciclaw/addons/<name> && git log`).

#### Trust-on-first-install (TOFI)

The first install of an addon establishes the trust baseline. Every subsequent upgrade is compared against that baseline and requires explicit confirmation. This is the same model as SSH host keys and TOFU in general — not perfect, but strictly better than never pinning.

For users who want stronger assurance:
- **Signed tags** with GPG keys in a known keyring (`~/sciclaw/trusted-keys/`) — if the addon repo signs releases, sciClaw verifies signatures against the keyring. Unsigned tags get a warning; tags with untrusted keys get a hard error unless `--allow-untrusted`.
- **Pinned keyrings per addon** — `addon.json` can declare `signing_key: "A1B2C3D4..."` and sciClaw refuses to upgrade unless tags are signed by that key.
- **SBOM export** — `sciclaw addon sbom <name>` prints a JSON manifest of the installed addon (name, version, commit, all `*_sha256` values) suitable for auditing.

SBOM export and pinned keyrings are stretch goals for v1 but flagged here so the data model supports them from day one.

### 9. State and Storage

```
~/sciclaw/
├── addons/
│   ├── registry.json              # enabled/disabled state + installed versions
│   └── webtop/                    # addon install directory
│       ├── addon.json             # manifest
│       ├── compose.yaml           # infrastructure
│       ├── bin/
│       │   ├── sciclaw-addon-webtop   # sidecar binary
│       │   ├── install.sh
│       │   └── uninstall.sh
│       ├── ui/                    # pre-built web assets
│       ├── data/                  # addon-owned persistent state (e.g., webtop.json)
│       └── sock                   # unix socket (created at enable)
```

**`registry.json` schema**:

```json
{
  "version": 1,
  "addons": {
    "webtop": {
      "version": "0.1.0",
      "installed_at": "2026-04-13T14:22:00Z",
      "installed_commit": "abc123def456",
      "state": "enabled",
      "source": "https://github.com/sciclaw/sciclaw-addon-webtop"
    }
  }
}
```

Updates are atomic (temp file + rename), same pattern as `pkg/profile/`.

## Reference Addons

Two addons ship as reference implementations. The purpose is not just to solve each addon's problem, but to stress the core contract in *different* ways so any leaky abstractions show up before third parties hit them.

| Addon | Primary purpose | Contract aspect it stresses |
|---|---|---|
| **webtop** | Per-user browser desktops (XFCE via selkies) | Dynamic container spawn, path-based routing, mount derivation from hooks, auth via forward-proxy |
| **jupyter** | Per-user or shared notebook servers | Own-UI iframing (Jupyter serves its own web app), token-based auth, lifecycle without path rewriting, kernel-scoped permissions |

Both use Docker, `compose.yaml`, and the same `sciclaw-net` network. They coexist in one install — a scientist can use the Desktops tab for full GUI work and the Notebooks tab for fast iteration without running two orchestration stacks.

## Webtop Reference Addon

Separate repo: `github.com/sciclaw/sciclaw-addon-webtop`. Built and released independently from sciClaw core.

### Architecture

```
┌────────────────────────────────────────────────────────────┐
│  Host (data3, lab server, etc.)                            │
│                                                            │
│  ┌──────────┐         ┌──────────────────────────────┐     │
│  │ sciclaw  │◄───────►│ sciclaw-addon-webtop sidecar │     │
│  │ (core)   │  unix   │  - manages docker containers │     │
│  └──────────┘  socket │  - hooks: routing_changed    │     │
│                       │  - UI: /ui serves React      │     │
│                       │  - state: data/webtop.json   │     │
│                       └──────────────────────────────┘     │
│                                      │                     │
│                                      │ docker commands     │
│                                      ▼                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ cloudflared  │─▶│ caddy        │─▶│ webtop-alice │     │
│  │              │  │ (docker-     │  │ SUBFOLDER=   │     │
│  │              │  │  proxy)      │  │ /alice/      │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│                           │          ┌──────────────┐     │
│                           └─────────▶│ webtop-bob   │     │
│                                      │ SUBFOLDER=   │     │
│                                      │ /bob/        │     │
│                                      └──────────────┘     │
└────────────────────────────────────────────────────────────┘
```

### compose.yaml (infrastructure containers)

```yaml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    command: tunnel run sciclaw
    volumes:
      - ~/sciclaw/addons/webtop/data/cloudflared:/etc/cloudflared
    restart: unless-stopped

  caddy:
    image: lucaslorentz/caddy-docker-proxy:latest
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - caddy-data:/data
    environment:
      - CADDY_INGRESS_NETWORKS=sciclaw-net
    networks:
      - sciclaw-net
    restart: unless-stopped

networks:
  sciclaw-net:
    external: true

volumes:
  caddy-data:
```

Webtop containers are **not** in compose — they are spawned dynamically by the sidecar in response to `user_added` hooks or explicit admin UI actions, using `docker run --network sciclaw-net --label ...`.

### Dynamic container spawn

When the sidecar gets a `routing_changed` hook or an admin clicks "Add user" in the Desktops tab:

```go
func (w *Webtop) spawn(user string, mounts []Mount) error {
    subfolder := "/" + user + "/"
    args := []string{
        "run", "-d",
        "--name", "webtop-" + user,
        "--network", "sciclaw-net",
        "--restart", "unless-stopped",
        "--label", "caddy=sciclaw.dev",
        "--label", "caddy.@" + user + ".path", subfolder + "*",
        "--label", "caddy.handle.@" + user + ".reverse_proxy", "{{upstreams 3000}}",
        "--label", "caddy.handle.@" + user + ".forward_auth", "authelia:9091",
        "-e", "SUBFOLDER=" + subfolder,
        "-e", "TITLE=sciClaw (" + user + ")",
    }
    for _, m := range mounts {
        mode := "rw"
        if m.Mode == ReadOnly { mode = "ro" }
        args = append(args, "-v", m.Source+":"+m.Target+":"+mode)
    }
    args = append(args, "ghcr.io/sciclaw/sciclaw-webtop:latest")
    return exec.Command("docker", args...).Run()
}
```

Caddy auto-discovers the labels, adds a path-based route, reloads itself. cloudflared is never touched after initial setup.

### webtop.json (sidecar state)

```json
{
  "version": 1,
  "users": {
    "alice": {
      "container_id": "abc123",
      "subfolder": "/alice/",
      "mounts": [
        {"source": "/home/ubuntu/sciclaw/projects/als-rct", "target": "/config/workspace/als-rct", "mode": "rw", "origin": "routing:#als-rct"},
        {"source": "/home/ubuntu/sciclaw/shared/templates", "target": "/config/workspace/templates", "mode": "ro", "origin": "routing:#shared"}
      ],
      "last_seen": "2026-04-13T14:32:00Z",
      "state": "running",
      "created_at": "2026-04-13T14:22:00Z"
    }
  }
}
```

### Mount derivation from routing

The sidecar's `routing_changed` hook handler reads the new rules, computes each user's allowed paths, diffs against current mounts, and restarts containers whose mount set changed. Manual mounts added via the admin UI are tagged `"origin": "manual"` and preserved across routing changes.

## Jupyter Reference Addon

Separate repo: `github.com/sciclaw/sciclaw-addon-jupyter`. Validates that the contract works for an addon that ships its own full web UI and doesn't need a reverse-proxy auth forwarder.

### Why Jupyter is a good second addon

- **Different UI pattern.** Jupyter Lab serves its own full-featured web app. The addon's admin tab shows a *control* UI (list notebooks, start/stop, rotate tokens, resource usage) — clicking "Open" launches Jupyter Lab in a new tab at its own URL. This exercises the "addon has two UIs: control + direct" pattern, which webtop doesn't hit.
- **Different auth model.** Jupyter uses token auth natively. sciClaw doesn't proxy the session — it mints a Jupyter token per user via the Jupyter REST API, stores it in the addon's state file, and hands it out in the "Open" URL. Forces us to answer "how do addons handle auth that doesn't flow through Authelia?"
- **Different hook semantics.** `routing_changed` affects which directories the notebook kernel should see, not a mount set per container. `user_added` triggers optional notebook provisioning. `profile_updated` can toggle extensions per user (e.g., scientific theme, keyboard shortcuts).
- **Smaller blast radius.** Scoped to notebook lifecycle. If the addon contract has a leak (e.g., hook race conditions, UI proxy corner cases), Jupyter exposes it faster than webtop's more complex stack.

### Architecture

```
┌────────────────────────────────────────────────────────────┐
│  Host                                                      │
│                                                            │
│  ┌──────────┐         ┌───────────────────────────────┐    │
│  │ sciclaw  │◄───────►│ sciclaw-addon-jupyter sidecar │    │
│  │ (core)   │  unix   │  - manages docker containers  │    │
│  └──────────┘  socket │  - mints jupyter tokens       │    │
│                       │  - tab: Notebooks (control)   │    │
│                       │  - state: data/jupyter.json   │    │
│                       └───────────────────────────────┘    │
│                                      │                     │
│                                      │ docker commands     │
│                                      ▼                     │
│  (shared with webtop addon)  ┌──────────────┐              │
│  ┌──────────────┐            │ jupyter-alice│              │
│  │ cloudflared  │◄───────────│ /jupyter/    │              │
│  └──────────────┘   same     │   alice/     │              │
│  ┌──────────────┐   caddy    └──────────────┘              │
│  │ caddy        │            ┌──────────────┐              │
│  │ (docker-     │◄───────────│ jupyter-bob  │              │
│  │  proxy)      │            │ /jupyter/bob/│              │
│  └──────────────┘            └──────────────┘              │
└────────────────────────────────────────────────────────────┘
```

Cloudflared and Caddy are **shared** between webtop and jupyter — the same two containers route for both. Each addon only spawns its own per-user containers. The `sciclaw-net` Docker network is the shared coordination point.

### compose.yaml

```yaml
services:
  # Nothing here — jupyter addon piggybacks on webtop addon's caddy/cloudflared.
  # If webtop is not installed, the jupyter addon's install.sh brings up
  # a minimal caddy-only stack.

networks:
  sciclaw-net:
    external: true
```

The installer detects whether `sciclaw-net` exists (i.e., webtop is already installed) and either reuses the existing Caddy or spins up its own minimal one.

### Dynamic container spawn

```go
func (j *Jupyter) spawn(user string, mounts []Mount) error {
    token := generateToken(32)
    j.state.SetToken(user, token)

    subpath := "/jupyter/" + user + "/"
    args := []string{
        "run", "-d",
        "--name", "jupyter-" + user,
        "--network", "sciclaw-net",
        "--restart", "unless-stopped",
        "--label", "caddy=sciclaw.dev",
        "--label", "caddy.handle.@jup_" + user + ".path", subpath + "*",
        "--label", "caddy.handle.@jup_" + user + ".reverse_proxy", "{{upstreams 8888}}",
        "-e", "JUPYTER_TOKEN=" + token,
        "-e", "JUPYTER_ENABLE_LAB=yes",
        "-e", "NB_USER=" + user,
    }
    for _, m := range mounts {
        args = append(args, "-v", m.Source+":/home/jovyan/work/"+m.Label+":"+modeFlag(m.Mode))
    }
    args = append(args,
        "--",
        "jupyter/scipy-notebook:latest",
        "start-notebook.sh",
        "--NotebookApp.base_url="+subpath,
        "--NotebookApp.allow_origin=*",
    )
    return exec.Command("docker", args...).Run()
}
```

Note the `--NotebookApp.base_url` flag — Jupyter has first-class subpath support, mirroring `SUBFOLDER` on the webtop side. Same principle: serve the app under a prefix, client constructs its URLs relative to that prefix.

### "Open Notebook" URL generation

The Notebooks tab shows a list of users with their container state. The "Open" button generates a URL with the token embedded as a query string (Jupyter's standard auth flow):

```
https://sciclaw.dev/jupyter/alice/lab?token=<JUPYTER_TOKEN>
```

Clicking navigates in a new tab. Jupyter Lab accepts the token, persists it in localStorage for the origin, and subsequent requests are authenticated.

**Token rotation**: the Notebooks tab has a "Rotate token" button per user. This:
1. Generates a new token
2. Updates container env via `docker exec` (or restart)
3. Invalidates the old token on the Jupyter server
4. Surfaces the new "Open" URL

### jupyter.json (sidecar state)

```json
{
  "version": 1,
  "users": {
    "alice": {
      "container_id": "xyz789",
      "subpath": "/jupyter/alice/",
      "token": "sha256-hashed-token-not-plaintext",
      "mounts": [
        {"source": "/home/ubuntu/sciclaw/projects/als-rct", "label": "als-rct", "mode": "rw", "origin": "routing:#als-rct"},
        {"source": "/home/ubuntu/sciclaw/shared/datasets", "label": "datasets", "mode": "ro", "origin": "routing:#shared"}
      ],
      "kernel": "python3",
      "last_seen": "2026-04-13T14:32:00Z",
      "state": "running",
      "created_at": "2026-04-13T14:22:00Z"
    }
  }
}
```

Tokens are stored as SHA256 hashes, not plaintext. To retrieve a clickable URL, the admin clicks "Show link" which rotates the token on demand (new token → hash stored → plaintext shown exactly once in the UI).

### Hook usage

| Hook | Handler behavior |
|---|---|
| `routing_changed` | Recompute each user's mounts. Diff against current container. If changed, restart container with new `-v` flags. |
| `user_added` | If config's `auto_provision: true`, spawn a notebook container for the new user. Otherwise just register them in the Notebooks tab as "not started". |
| `user_removed` | Stop and remove the user's container, delete state entry, rotate-revoke token. |
| `profile_updated` | If profile toggled `jupyter_kernel` preference (e.g., `r-notebook` vs `scipy-notebook`), recreate the container with the new image. |

### UI integration

The Notebooks admin tab is a control panel: list of users, states, token rotation, resource usage, kernel selector. It does NOT embed Jupyter Lab itself — Jupyter Lab opens in a new browser tab via the token URL. This is the "two UIs" pattern: addons can provide a control UI that core embeds in an iframe, AND external URLs that launch in new tabs.

The webtop addon does this too (the admin control UI lives in the Desktops tab iframe; clicking "Open" launches the full selkies desktop in a new tab). Both addons validate the same pattern from different angles.

### Differences that stress the contract

Writing both addons in parallel surfaces these contract questions (which the RFC should answer before third parties hit them):

1. **Shared infrastructure across addons.** Two addons both want Caddy and cloudflared. Should core provide these as a "platform" that addons can declare as a dependency? For v1: no, each addon's installer detects and reuses. Revisit if we get to three addons.
2. **Inter-addon coordination.** If a user is removed in webtop, should jupyter also clean up? For v1: each addon handles `user_removed` independently. Both will stop their own containers. Clean, no coupling.
3. **Naming collisions.** Webtop labels a container `webtop-alice`, jupyter labels `jupyter-alice`. Caddy routes are `/alice/` (webtop) and `/jupyter/alice/` (jupyter). Addons should prefix their container names with the addon name; the core contract requires this.
4. **UI "Open" launcher.** Core should provide a standard pattern for "addon wants to open a URL in a new browser tab." For v1, addons embed regular `<a target="_blank">` links in their own UI. Core doesn't mediate.

## Path-Based Routing: Verification

Earlier I was skeptical that selkies-gstreamer would work under a subpath. **I was wrong**, and the fix is documented here so future work doesn't re-litigate:

**Finding 1**: `SUBFOLDER` is a first-class, documented environment variable in linuxserver's webtop image. From the [docker-webtop README](https://github.com/linuxserver/docker-webtop):
> `SUBFOLDER` — Subfolder for the application if running a subfolder reverse proxy, need both slashes IE `/subfolder/`

**Finding 2**: The nginx config template lives at `root/defaults/default.conf` in `linuxserver/docker-baseimage-selkies`. It contains literal `SUBFOLDER` placeholders:

```nginx
location SUBFOLDER {
    alias /usr/share/selkies/web/;
    index  index.html index.htm;
}
location SUBFOLDERwebsocket {
    proxy_pass http://127.0.0.1:CWS;
    # ... websocket upgrade headers
}
```

Substituted at container start by `root/etc/s6-overlay/s6-rc.d/init-nginx/run`:

```bash
SFOLDER="${SUBFOLDER:-/}"
sed -i "s|SUBFOLDER|$SFOLDER|g" ${NGINX_CONFIG}
```

**Finding 3**: The client-side HTML (`addons/selkies-dashboard/index.html` in `selkies-project/selkies`) uses relative URLs:

```html
<link rel="apple-touch-icon" href="icon.png">
<link rel="manifest" href="manifest.json" crossorigin="use-credentials">
<script type="module" src="src/main.jsx"></script>
```

No leading slashes. The browser resolves these against the current document URL, so a page loaded from `/alice/` fetches `/alice/icon.png`.

**Finding 4**: The WebSocket signaling URL in `addons/selkies-web-core/selkies-wr-core.js:1569` is constructed from `window.location.pathname`, not hardcoded:

```js
var pathname = window.location.pathname;
pathname = pathname.slice(0, pathname.lastIndexOf("/") + 1);
var protocol = (location.protocol == "http:" ? "ws://" : "wss://");
var url = new URL(protocol + window.location.host + pathname + appName + "/signaling/");
```

**Conclusion**: path-based proxying is supported end-to-end by selkies + linuxserver webtop. No image patches required. Setting `SUBFOLDER=/alice/` on the container and routing `sciclaw.dev/alice/*` to it in Caddy works out of the box.

## Migration Path

sciClaw does not have an addon system today. Introducing one is additive — no existing user is affected until they opt in.

### Phase 1: Core addon runtime (sciclaw core release)

1. Add `pkg/addons/` package: registry, manifest parser, lifecycle commands, hook dispatcher, UI proxy
2. Add `cmd/picoclaw/addon_cmd.go` for `sciclaw addon {install,enable,disable,uninstall,upgrade,list,status}`
3. Add `web/src/addons/AddonTabs.tsx` for dynamic tab injection
4. Fire hooks at existing event sites in `pkg/channels/`, `pkg/routing/`, `pkg/profile/`
5. Ship in sciClaw 0.3.0. No addons exist yet — the runtime is dormant.

### Phase 2: Webtop addon v0.1

1. Create `github.com/sciclaw/sciclaw-addon-webtop` repo
2. Implement sidecar: HTTP server on Unix socket, container lifecycle via `docker` CLI shellout, mount derivation from `routing_changed` hooks
3. Build React UI for Desktops tab
4. Ship `compose.yaml` for caddy-docker-proxy + cloudflared
5. Write `install.sh` that validates Docker presence, pulls base images, walks user through cloudflared tunnel setup
6. Release v0.1.0. Document install on data3.

### Phase 3: Jupyter addon (`sciclaw-addon-jupyter`)

Ship the Jupyter addon as the second reference. The explicit goal is to discover leaky abstractions in the addon contract before third parties do. The RFC's "Differences that stress the contract" section in the Jupyter section lists the specific questions we expect to surface:

1. Shared infrastructure across addons (Caddy + cloudflared reuse)
2. Inter-addon coordination (user removal cascade)
3. Naming collision policy (container names, Caddy labels, paths)
4. "Open in new tab" UX pattern for addons with external URLs

If any of these require core contract changes, they land in sciClaw 0.3.1 or 0.4.0 before the contract goes stable.

### Phase 4: Publish the contract

Move the addon runtime docs (manifest spec, hook catalog, sidecar protocol, security model) from this RFC into `docs/addon-contract.md` under a stable URL. Version it. Third parties can now build addons with confidence the interface won't break.

## Alternatives Considered

**Alt 1: Compile-time build tags.** Use Go build tags to exclude webtop code from default builds. Users who want webtop compile sciClaw with `-tags webtop`.

Rejected: breaks binary distribution (Homebrew, GitHub releases). Users expect one binary, not "build from source if you want this feature." Also conflates compile-time and runtime concerns.

**Alt 2: Separate binaries, no coordination.** Ship `sciclaw-webtop` as a standalone CLI that happens to live next to `sciclaw`. No addon runtime at all.

Rejected: no UI integration, no hook dispatch, no shared identity model. Users end up managing two tools that should feel like one. Reinvents coordination at each seam.

**Alt 3: gRPC plugins (hashicorp/go-plugin).** Use Hashicorp's battle-tested plugin system — subprocesses communicating via gRPC over stdio.

Rejected for v1: heavier dep (adds ~5MB), requires generating proto files, more ceremony than the problem needs. The addon surface is small (four required endpoints), HTTP-over-unix-socket is enough. Revisit in v2 if the contract grows.

**Alt 4: Native Go plugins (`plugin` package).** Load `.so` files at runtime.

Rejected: `.so` plugins are Linux-only in practice, forbid any dependency skew between plugin and host, and are widely considered a mistake by the Go community. Does not solve the "different release cadences" problem.

**Alt 5: Put webtop in core behind a feature flag.** Keep the code in `pkg/webtop/` with a `--enable-webtop` flag at startup.

Rejected: doesn't remove the Docker SDK dependency from the binary. Doesn't give third parties anything. Just delays the real design.

**Alt 6: Subdomain-per-user webtop instead of path-based.** Use `alice-desktop.sciclaw.dev` with wildcard DNS.

Rejected after verification: path-based is supported by selkies out of the box, avoids wildcard DNS and wildcard cert provisioning, and needs only one cloudflared ingress rule forever. Subdomain approach was my initial recommendation; dropping it here because the verified facts changed my mind.

## Open Questions

1. **Sidecar resource limits.** Should core enforce CPU/memory limits on addon sidecars via cgroups? This would protect gateway stability from a runaway addon. Not implementing in v1 — revisit if it becomes a real problem.

2. **Cross-addon hook ordering.** If two addons both subscribe to `routing_changed`, in what order do they fire? Parallel with no ordering guarantee in v1. If ordering matters, addons must coordinate via dependencies or a hook priority field (v2).

3. **Web UI authentication for addon routes.** Core's Web UI already has session auth. Do addon UIs inherit that session, or do they need their own? Plan: core forwards the authenticated user's session token in a header to the addon's `/ui/*`, addon trusts the header. Documented in the sidecar protocol.

4. **Addon → addon communication.** Can one addon call another? v1 answer: no, only via hooks dispatched through core. If tight coupling is needed, they should be one addon.

5. **Shared-infrastructure addons.** Webtop and Jupyter both want Caddy + cloudflared. In v1 each addon's installer detects and reuses. If we reach three container-based addons, promote the Caddy/cloudflared stack to a "platform" addon they all declare as a dependency.

6. **Signed-tag enforcement level.** Should `signed_tag` + `signature_verified` be *required* for production installs, or just preferred? v1 answer: preferred (signed tags are chosen when available, unsigned tags warn, `--allow-untrusted` overrides). Tightening to required is a v2 option once the signing infrastructure is proven.

7. **Multipass / VM-based addons.** Should the contract support addons that spawn VMs instead of containers? Deferred — current sciclaw already has some VM-based code in `executor_vm.go`, but making VMs a first-class addon surface is a separate design.

### Resolved during drafting

- **Addon versioning policy** → Pin to exact commit SHA at install, track-a-branch is opt-in, integrity verified on every startup. See section 8.
- **Webtop subdomain vs. path-based routing** → Path-based. Verified selkies' `SUBFOLDER` support end-to-end. Single domain, one cert, no wildcards, no cloudflared restarts. See Verification section.
- **Second validation addon** → Jupyter. Different enough (own-UI, token auth, different hook semantics) to stress the contract; similar enough (Docker-based, shares Caddy/cloudflared) to reuse infrastructure. See Jupyter Reference Addon section.

## Implementation Checklist

### Core (`sciclaw` repo)

- [ ] `pkg/addons/manifest.go` — `addon.json` parser + schema validation
- [ ] `pkg/addons/registry.go` — `~/sciclaw/addons/registry.json` read/write, atomic updates
- [ ] `pkg/addons/lifecycle.go` — install/enable/disable/uninstall/upgrade state machine
- [ ] `pkg/addons/sidecar.go` — Unix socket HTTP client, health check, shutdown
- [ ] `pkg/addons/hooks.go` — event dispatch to subscribed addons, parallel with per-addon timeout
- [ ] `pkg/addons/ui_proxy.go` — reverse proxy from `web/addons/<name>/*` to sidecar
- [ ] `pkg/addons/cli_proxy.go` — CLI invocation forwarding
- [ ] `cmd/picoclaw/addon_cmd.go` — `sciclaw addon` subcommand group
- [ ] Hook emission sites: `pkg/routing/store.go` (routing_changed), `pkg/profile/profile.go` (profile_updated), `pkg/channels/discord.go` (user_added, user_removed)
- [ ] `web/src/addons/AddonTabs.tsx` — dynamic tab registration
- [ ] `web/src/api/addons.ts` — `GET /api/addons/enabled` client
- [ ] Tests: manifest parsing, registry state transitions, hook dispatch with failing addon, socket protocol
- [ ] Docs: `docs/addon-contract.md`, brief section in `README.md`

### Pinning and integrity (core)

- [ ] `pkg/addons/resolver.go` — resolve user reference (`--commit`, `--version`, `--track`, default) to exact SHA
- [ ] `pkg/addons/integrity.go` — compute and verify `*_sha256` fields; refuse enable on mismatch
- [ ] `pkg/addons/signing.go` — GPG tag verification (gated by presence of keys in `~/sciclaw/trusted-keys/`)
- [ ] `pkg/addons/rollback.go` — one-level rollback using `previous_commit`
- [ ] `pkg/addons/sbom.go` — JSON SBOM export for audit
- [ ] CLI: `sciclaw addon install --commit <sha>`, `--version <tag>`, `--track <branch>`
- [ ] CLI: `sciclaw addon verify <name>`, `sciclaw addon rollback <name>`, `sciclaw addon sbom <name>`
- [ ] Tests: resolver edge cases (no signed tags, malformed tags), integrity tampering detection, rollback round-trip

### Webtop addon (`sciclaw-addon-webtop` repo, separate)

- [ ] `addon.json` manifest
- [ ] `cmd/sidecar/main.go` — HTTP server on Unix socket
- [ ] `pkg/containers/runtime.go` — docker/podman CLI shellout
- [ ] `pkg/containers/mounts.go` — derive mounts from routing rules
- [ ] `pkg/state/webtop.json` — state store
- [ ] `compose.yaml` — Caddy + cloudflared stack
- [ ] `bin/install.sh` — validates runtime, pulls images, walks user through tunnel setup
- [ ] `bin/uninstall.sh` — teardown
- [ ] `ui/` — React app for Desktops tab (list, add user modal, detail panel, mounts editor)
- [ ] Hook handlers: `routing_changed` (recompute mounts, restart affected containers), `user_added` (optional auto-provision), `profile_updated` (no-op initially)
- [ ] Tests: spawn/stop, mount derivation, registry state
- [ ] Integration test against real webtop image in CI
- [ ] Release v0.1.0 with tag + GitHub release notes

### Jupyter addon (`sciclaw-addon-jupyter` repo, separate)

- [ ] `addon.json` manifest (declares same runtime requirements as webtop, hooks: `routing_changed`, `user_added`, `user_removed`, `profile_updated`)
- [ ] `cmd/sidecar/main.go` — HTTP server on Unix socket
- [ ] `pkg/containers/runtime.go` — reuse webtop's pattern for docker CLI shellout (could be extracted to a shared Go module later, not in v1)
- [ ] `pkg/tokens/rotate.go` — generate, hash, store, rotate Jupyter tokens
- [ ] `pkg/state/jupyter.json` — state store with hashed tokens
- [ ] `compose.yaml` — empty (reuses webtop's Caddy/cloudflared) or minimal fallback
- [ ] `bin/install.sh` — detects `sciclaw-net`, installs minimal Caddy if absent, pulls `jupyter/scipy-notebook:latest`
- [ ] `bin/uninstall.sh` — teardown
- [ ] `ui/` — React app for Notebooks tab (list, start/stop, token rotate, "Open" launcher, kernel selector)
- [ ] Hook handlers: `routing_changed` (recompute mounts), `user_added` (optional auto-provision), `user_removed` (stop container + revoke token), `profile_updated` (kernel image swap)
- [ ] Integration test: spawn, open URL, execute cell, verify workspace mount, rotate token, revoke old
- [ ] Release v0.1.0 AFTER webtop v0.1.0 ships — proves contract independence

### Deployment checklist (data3 as reference install)

- [ ] Install sciClaw 0.3.0
- [ ] `docker network create sciclaw-net`
- [ ] `cloudflared tunnel create sciclaw-data3` and save credentials
- [ ] `sciclaw addon install github.com/sciclaw/sciclaw-addon-webtop --version v0.1.0`
- [ ] `sciclaw addon enable webtop`
- [ ] Configure DNS `sciclaw.dev → tunnel` via Cloudflare dashboard (single hostname, not wildcard)
- [ ] Test end-to-end: add user in Desktops tab, click URL, Selkies desktop loads at `sciclaw.dev/<user>/`
- [ ] `sciclaw addon install github.com/sciclaw/sciclaw-addon-jupyter --version v0.1.0`
- [ ] `sciclaw addon enable jupyter`
- [ ] Test end-to-end: add user in Notebooks tab, click Open, Jupyter Lab loads at `sciclaw.dev/jupyter/<user>/`
- [ ] Verify `sciclaw addon verify webtop` and `sciclaw addon verify jupyter` both pass (integrity baseline recorded)
- [ ] Verify `sciclaw addon sbom webtop` exports a complete manifest for audit

## Verification Steps

For the core addon system:

1. `go test ./pkg/addons/... -race` — all pass
2. Manifest parser rejects malformed JSON, missing required fields, bad version constraints
3. Registry survives crash mid-write (atomic rename works)
4. Hook dispatch: failing addon does not block other addons, core operation completes in <100ms regardless of addon latency
5. UI proxy: addon UI loads, browser session token forwarded correctly
6. CLI proxy: `sciclaw <addon-group> ...` returns addon output, propagates exit code
7. Install + enable + disable + uninstall cycle leaves no stale files

For the webtop addon:

1. Fresh install on clean Linux host: bootstrap script passes
2. `sciclaw addon enable webtop` brings up Caddy + cloudflared
3. Add user via Desktops tab → container spawns → Selkies desktop reachable at `sciclaw.dev/<user>/`
4. Edit a file in Thunar → file appears on host at `~/sciclaw/...` with correct ownership
5. Agent in Discord references the same file → sees the edit
6. Routing rule change in core → webtop container restarts with new mount set
7. Disable addon → all webtop containers stopped, mounts released, Caddy route gone
8. Uninstall → no residual containers, volumes, or config

## References

- [linuxserver/docker-webtop README](https://github.com/linuxserver/docker-webtop/blob/ubuntu-xfce/README.md) — documents `SUBFOLDER` env var
- [linuxserver/docker-baseimage-selkies/root/defaults/default.conf](https://github.com/linuxserver/docker-baseimage-selkies/blob/master/root/defaults/default.conf) — nginx template with `SUBFOLDER` placeholders
- [selkies-project/selkies/addons/selkies-web-core/selkies-wr-core.js](https://github.com/selkies-project/selkies/blob/main/addons/selkies-web-core/selkies-wr-core.js) — pathname-aware WebSocket URL construction
- [lucaslorentz/caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) — label-based dynamic Caddy config
- [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin) — alternative architecture (rejected for v1)
- CLIProxy PR #2586 — related streaming fix context (unrelated module, similar shape)
