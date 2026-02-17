<div align="center">

<img src="docs/og-image.jpg" alt="sciClaw" width="480" />

<br />

**A paired-scientist agent for reproducible research workflows.**

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![Platforms](https://img.shields.io/badge/Platforms-macOS%20%C2%B7%20Linux%20%C2%B7%20Windows-blue)](https://github.com/drpedapati/sciclaw/releases)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

[Website](https://sciclaw.dev) ·
[Documentation](https://sciclaw.dev/docs.html) ·
[Releases](https://github.com/drpedapati/sciclaw/releases) ·
[Discussions](https://github.com/drpedapati/sciclaw/discussions)

</div>

---

sciClaw is a paired-scientist agent for rigorous research work. It connects to major LLM providers, proposes and executes hypothesis-driven loops, runs real tools (literature, documents, shell), and keeps an auditable evidence trail in your workspace.

Built on the [PicoClaw](https://github.com/sipeed/picoclaw) runtime (a Go rewrite of [nanobot](https://github.com/HKUDS/nanobot)), sciClaw keeps a single-binary footprint while adding a paired-scientist operating model: plan, evidence, review, iterate.

## Install

### Homebrew (recommended)

```bash
# One-line bootstrap (installs sciclaw + dependencies, initializes workspace, and verifies)
brew tap drpedapati/tap && brew install sciclaw && sciclaw onboard --yes && sciclaw doctor
```

macOS only:
```bash
brew install --cask quarto
```

### Download a binary

Pre-compiled binaries for macOS (arm64), Linux (amd64, arm64, riscv64), and Windows (amd64) are on the [releases page](https://github.com/drpedapati/sciclaw/releases).

### From source

```bash
git clone https://github.com/drpedapati/sciclaw.git
cd sciclaw
make deps && make install
```

### Companion tools

If you install via Homebrew, sciClaw pulls companion tools automatically (IRL, ripgrep, docx-review, pubmed-cli; and on Linux also Quarto).
If you install via downloaded binary/source, see `sciclaw doctor` for install hints.

## Quick Start

**1. Initialize**

```bash
sciclaw onboard
```

Non-interactive (CI / scripts):
```bash
sciclaw onboard --yes
```

**2. Configure** (`~/.picoclaw/config.json`)

```json
{
  "agents": {
    "defaults": {
      "model": "gpt-5.2",
      "max_tokens": 8192,
      "temperature": 0.7
    }
  },
  "providers": {
    "openai": {
      "api_key": "sk-..."
    }
  }
}
```

Or authenticate without editing config:

```bash
sciclaw auth login --provider openai          # OAuth (device code; works on local + headless)
sciclaw auth login --provider anthropic       # Token paste
```

Optional: PubMed rate limits
```bash
# Either set via the onboard wizard, or set manually:
export NCBI_API_KEY="your-key"
```

**3. Chat**

```bash
# One-shot
sciclaw agent -m "What pathways are implicated in ALS?"

# Interactive
sciclaw agent

# Override model for one invocation
sciclaw agent --model gpt-5.2 -m "Review this manuscript draft"

# Control reasoning effort
sciclaw agent --effort high -m "Analyze the statistical methods in this paper"
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `sciclaw onboard` | Initialize config, workspace, and baseline skills |
| `sciclaw agent -m "..."` | One-shot message |
| `sciclaw agent` | Interactive chat |
| `sciclaw agent --model <model>` | Override model for this invocation |
| `sciclaw agent --effort <level>` | Set reasoning effort level |
| `sciclaw models list` | Show current model and configured providers |
| `sciclaw models set <model>` | Persistently change the default model |
| `sciclaw models effort <level>` | Persistently change reasoning effort |
| `sciclaw models status` | Show model, provider, auth, and effort |
| `sciclaw status` | Show system status (config, providers, IRL runtime) |
| `sciclaw doctor` | Verify deployment: config, tools, skills, auth, gateway, service |
| `sciclaw doctor --fix` | Auto-fix: sync baseline skills, remove legacy names |
| `sciclaw gateway` | Start chat channel gateway (Telegram, Discord, etc.) |
| `sciclaw service <subcommand>` | Manage background gateway service |
| `sciclaw auth login` | Authenticate with a provider |
| `sciclaw auth status` | Show stored credentials |
| `sciclaw skills list` | List installed skills |
| `sciclaw skills install <source>` | Install a skill from GitHub or local path |
| `sciclaw cron list` | List scheduled jobs |
| `sciclaw cron add` | Add a scheduled job |

## Providers

sciClaw auto-detects the provider from the model name. Set credentials via `config.json` or `sciclaw auth login`.
For production use, sciClaw is optimized around OpenAI `gpt-5.2`; other providers remain available for compatibility.

| Provider | Models | Auth |
|----------|--------|------|
| **OpenAI** | gpt-5.2 (primary), gpt-5.2-chat-latest, gpt-5.2-pro | API key or device-code OAuth |
| **Anthropic** | claude-opus-4-6, claude-sonnet-4-5 | API key or token paste |
| **Gemini** | gemini-2.5-pro, gemini-2.5-flash | API key |
| **OpenRouter** | All models via `openrouter/` prefix | API key |
| **DeepSeek** | deepseek-chat, deepseek-reasoner | API key |
| **Groq** | Fast inference + Whisper voice transcription | API key |
| **Zhipu** | GLM models | API key |

> Groq provides free voice transcription via Whisper. When configured, Telegram voice messages are automatically transcribed.

## Reasoning Effort

The `--effort` flag controls how deeply `gpt-5.2` thinks before answering. This directly affects quality, latency, and cost.

| Provider | Valid levels | Default |
|----------|-------------|---------|
| OpenAI (`gpt-5.2`) | `none` · `minimal` · `low` · `medium` · `high` · `xhigh` | provider default (use `medium` as practical baseline) |

```bash
sciclaw agent --model gpt-5.2 --effort medium -m "Summarize this section"
sciclaw agent --model gpt-5.2 --effort high -m "Complex analysis"
sciclaw agent --model gpt-5.2 --effort xhigh -m "Prove this theorem"

# Save a default
sciclaw models effort high
```

## Built-in Skills

Twelve skills are installed during `sciclaw onboard`:

### Research & Literature
- **scientific-writing** — Manuscript drafting with claim-evidence alignment
- **pubmed-cli** — PubMed search, article fetch, citation graphs, MeSH lookup ([CLI tool](https://github.com/drpedapati/pubmed-cli))
- **biorxiv-database** — bioRxiv/medRxiv preprint surveillance

### Authoring & Visualization
- **quarto-authoring** — Loop-driven `.qmd` authoring and rendering
- **beautiful-mermaid** — Publication-grade Mermaid diagrams

### Evidence & Provenance
- **experiment-provenance** — Reproducible experiment metadata capture
- **benchmark-logging** — Benchmark records with acceptance criteria

### Office & Documents
- **docx-review** — Word documents with tracked changes, comments, and semantic diff ([CLI tool](https://github.com/drpedapati/docx-review))
- **pptx** — PowerPoint creation and editing
- **pdf** — PDF creation, merging, splitting, and extraction
- **xlsx** — Spreadsheet creation, analysis, and conversion

### Polish
- **humanize-text** — Final-pass language polishing for natural tone

### Optional Bundled Skill (Manual Install)
- **phi-cleaner** — Clinical text de-identification helper for PHI-safe sharing workflows (`phi-clean` CLI).
- Bundled with sciClaw and available to the agent; install the companion CLI only if needed:

```bash
brew tap drpedapati/tools
brew install drpedapati/tools/phi-cleaner
```

Additional skills are available from the [skills catalog](https://github.com/drpedapati/sciclaw-skills):

```bash
sciclaw skills install drpedapati/sciclaw-skills/<skill-name>
```

### Companion Tool Ownership Migration

Office tool repositories moved from `henrybloomingdale/*` to `drpedapati/*`.
If you install Office tools directly (outside sciClaw's `sciclaw-*` aliases), use:

```bash
brew tap drpedapati/tools
brew install drpedapati/tools/docx-review
brew install drpedapati/tools/pptx-review
brew install drpedapati/tools/xlsx-review
```

## Skills Installer

The skills installer validates all content before writing to disk:

- **Size limits** — 256KB per skill, 1MB for catalog downloads
- **Binary rejection** — NUL byte detection prevents non-text content
- **Frontmatter validation** — Skills must have valid YAML frontmatter with a `name` field
- **Provenance logging** — Every install writes a `.provenance.json` with source URL, SHA-256, timestamp, and size
- **Pinned catalog ref** — catalog fetch uses an immutable commit ref (not mutable `main`) for supply-chain hardening

### Catalog Pin Rotation

To refresh the skills catalog pin:

1. Verify the target commit in `drpedapati/sciclaw-skills`.
2. Update `skillsCatalogPinnedRef` in `pkg/skills/installer.go`.
3. Run tests and release.

## IRL Integration

sciClaw integrates with [IRL](https://github.com/drpedapati/irl-template) (Idempotent Research Loop) for project lifecycle management. IRL is installed automatically as a Homebrew dependency.

The agent mediates IRL operations — creating projects, adopting existing directories, discovering past work — through natural conversation:

```bash
sciclaw agent -m "Create a new project for ERP correlation analysis"
sciclaw agent -m "What projects do I have?"
sciclaw agent -m "Adopt my old als-biomarker folder as a managed project"
```

Every IRL command is recorded in `~/sciclaw/irl/commands/` for auditability (workspace path is configurable).

## Chat Channels

Talk to sciClaw through messaging apps by running `sciclaw gateway`.
Scientist-friendly walkthrough: [Scientist Setup Guide](https://drpedapati.github.io/sciclaw/docs.html#scientist-setup).

Recommended setup:
```bash
sciclaw channels setup telegram
sciclaw gateway
```

Background service (recommended for always-on bots):
```bash
sciclaw service install
sciclaw service start
sciclaw service status
```

Lifecycle controls:
```bash
sciclaw service stop
sciclaw service restart
sciclaw service logs --lines 200
sciclaw service uninstall
```

Platform notes:
- **macOS**: uses per-user `launchd` (`~/Library/LaunchAgents/io.sciclaw.gateway.plist`)
- **Linux**: uses `systemd --user` (`~/.config/systemd/user/sciclaw-gateway.service`)
- **WSL**: service mode works when systemd is enabled; otherwise run `sciclaw gateway` in a terminal

| Channel | Setup |
|---------|-------|
| **Telegram** | Easy — just a bot token from @BotFather |
| **Discord** | Easy — bot token + MESSAGE CONTENT INTENT |
| **Slack** | Medium — app + bot token |
| **QQ** | Easy — AppID + AppSecret |
| **DingTalk** | Medium — client credentials |

<details>
<summary>Telegram setup</summary>

1. Open Telegram, search `@BotFather`, send `/newbot`, copy the token
2. Run `sciclaw channels setup telegram` (pairs your account and writes config)
3. Manual config (advanced) in `~/.picoclaw/config.json`:

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "YOUR_BOT_TOKEN",
      "allow_from": ["YOUR_USER_ID"]
    }
  }
}
```

4. Run `sciclaw gateway`

</details>

<details>
<summary>Discord setup</summary>

1. Create app at [discord.com/developers](https://discord.com/developers/applications)
2. Enable **MESSAGE CONTENT INTENT** in Bot settings
3. Copy bot token, get your User ID (Developer Mode → right-click → Copy User ID)
4. Add to config:

```json
{
  "channels": {
    "discord": {
      "enabled": true,
      "token": "YOUR_BOT_TOKEN",
      "allow_from": ["YOUR_USER_ID"]
    }
  }
}
```

5. Generate invite URL (scopes: `bot`; permissions: Send Messages, Read Message History)
6. Run `sciclaw gateway`

</details>

## Docker

```bash
git clone https://github.com/drpedapati/sciclaw.git
cd sciclaw
cp config/config.example.json config/config.json
# Edit config.json with your credentials

# Gateway mode
docker compose --profile gateway up -d

# One-shot agent
docker compose run --rm sciclaw-agent -m "Hello"

# Logs
docker compose logs -f sciclaw-gateway
```

## Workspace Layout

```
~/sciclaw/
├── sessions/          # Conversation history
├── memory/            # Long-term memory (MEMORY.md)
├── state/             # Persistent state
├── cron/              # Scheduled jobs
├── skills/            # Installed skills
├── hooks/             # Hook audit log (JSONL)
├── irl/commands/      # IRL command audit records
├── AGENTS.md          # Agent behavior guide
├── HEARTBEAT.md       # Periodic task prompts
├── HOOKS.md           # Hook policy (plain-language)
├── IDENTITY.md        # sciClaw identity
├── SOUL.md            # Agent values & guardrails
├── TOOLS.md           # Tool descriptions
└── USER.md            # User preferences
```

## Doctor

Run `sciclaw doctor` to verify your deployment — config, workspace, auth credentials, companion tools, baseline skills, gateway health, service health, and Homebrew update status:

```bash
sciclaw doctor            # Human-readable report
sciclaw doctor --json     # Machine-readable output
sciclaw doctor --fix      # Auto-fix: sync baseline skills, remove legacy skill names
```

The doctor checks:
- **Config & workspace** — validates `~/.picoclaw/config.json` and workspace directory exist
- **Auth credentials** — checks OpenAI OAuth status (authenticated, expired, needs refresh)
- **Companion tools** — verifies `docx-review`, `pubmed-cli`, `quarto`, `irl`, `pandoc`, `rg`, `python3` are installed, with install hints for missing tools
- **Baseline skills** — confirms all 12 skills are present, detects legacy skill names (`docx`, `pubmed-database`)
- **Gateway health** — scans logs for Telegram 409 conflicts (multiple bot instances)
- **Service health** — checks backend support plus installed/running/enabled status for background mode
- **Homebrew** — checks if `sciclaw` is outdated

Exit code 0 = all checks pass. Exit code 1 = at least one error.

## Troubleshooting

**"no credentials for openai/anthropic"**
```bash
sciclaw auth login --provider openai
sciclaw auth status
```

**Web search says "API configuration problem"**
Get a free key at [brave.com/search/api](https://brave.com/search/api) (2000 free queries/month) and add to config under `tools.web.search.api_key`.

**Telegram "Conflict: terminated by other getUpdates"**
Only one `sciclaw gateway` instance can run at a time.
If you use service mode, restart it cleanly:
```bash
sciclaw service restart
```

## Updating

```bash
# Homebrew
brew upgrade sciclaw

# Refresh skills to latest built-in versions
sciclaw onboard

# Check everything
sciclaw status
sciclaw --version
```

## License

MIT — see [LICENSE](LICENSE).

sciClaw is a fork of [PicoClaw](https://github.com/sipeed/picoclaw) by Sipeed, which is based on [nanobot](https://github.com/HKUDS/nanobot) by HKUDS. The `picoclaw` command remains available as a compatibility alias.
