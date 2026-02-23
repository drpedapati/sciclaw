# Brew-Only Deployment Contract (macOS)

This contract keeps sciClaw deployments stable and prevents path drift.

## Non-Negotiable Rules

1. Install and upgrade only through Homebrew.
2. Never copy `sciclaw` binaries into `~/.local/bin` for production services.
3. Never symlink `/opt/homebrew/bin/sciclaw` to a non-Homebrew path.
4. Always (re)install the service using the currently active Brew binary.
5. Treat launchd `Running: yes` as necessary but not sufficient; verify channel/socket readiness too.

## Golden Commands (Fresh Host)

```bash
brew tap drpedapati/tap
brew install sciclaw
which -a sciclaw
sciclaw --version
sciclaw service install
sciclaw service start
```

## Service Binding Verification (Required)

```bash
# 1) Binary provenance
which -a sciclaw
ls -l "$(command -v sciclaw)"

# 2) launchd program path must match active sciclaw
launchctl print gui/"$(id -u)"/io.sciclaw.gateway | egrep "program =|path =|state ="

# 3) Runtime sanity
sciclaw service status
sciclaw doctor
```

Pass criteria:
- `command -v sciclaw` resolves to Homebrew-managed path.
- launchd `program =` points to that same binary.
- `sciclaw doctor` reports no blocking gateway/tool errors.

## Upgrade Procedure (No Drift)

```bash
brew update
brew upgrade sciclaw
sciclaw service install   # refresh launchd plist to current binary/PATH
sciclaw service restart
sciclaw service status
```

## Operational Readiness Check (Gateway)

After any install/upgrade/restart:

```bash
# confirm bot connection in logs
sciclaw service logs --lines 120 | egrep -i "Discord bot connected|Telegram bot connected|Starting channel"

# optional: confirm process has outbound sockets
pid=$(pgrep -f "sciclaw gateway" | head -n1)
lsof -n -P -a -p "$pid" -i
```

## Forbidden Patterns

Do **not** do any of these in production:

- `cp ./sciclaw ~/.local/bin/sciclaw`
- `ln -sf ~/.local/bin/sciclaw /opt/homebrew/bin/sciclaw`
- Running mixed binaries (`sciclaw` from Brew, service using another path)
- Manual launchd plist edits that bypass `sciclaw service install`

## Recovery Playbook (If Drift Is Suspected)

```bash
# stop/uninstall current service binding
sciclaw service stop || true
sciclaw service uninstall || true

# reinstall from Brew source of truth
brew reinstall sciclaw

# rebind service and start
sciclaw service install
sciclaw service start
sciclaw service status
```

If `sciclaw` is missing after cleanup, install via Brew first, then run the playbook.

## Data1 Cleanup Rule (Requested)

When asked to "clean up sciclaw except config":
- Preserve only `~/.picoclaw/config.json`.
- Remove service/plists/logs/workspace/auth/cache/binaries.
- Reinstall from Brew and rebind service afterward.

