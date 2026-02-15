<div align="center">

# sciClaw

**An autonomous paired-scientist CLI for reproducible research workflows.**

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![Platforms](https://img.shields.io/badge/Platforms-macOS%20%C2%B7%20Linux%20%C2%B7%20Windows-blue)](https://github.com/drpedapati/sciclaw/releases)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

[Documentation](https://drpedapati.github.io/sciclaw/docs.html) ·
[Releases](https://github.com/drpedapati/sciclaw/releases) ·
[Issues](https://github.com/drpedapati/sciclaw/issues)

</div>

---

sciClaw is a lightweight AI agent that acts as a research collaborator. It connects to any major LLM provider, follows a hypothesis-driven research loop, and ships with 12 built-in scientific skills — literature search, manuscript drafting, citation graphs, document review, and more.

Built on the [PicoClaw](https://github.com/sipeed/picoclaw) runtime (a Go rewrite of [nanobot](https://github.com/HKUDS/nanobot)), sciClaw adds a paired-scientist operating model while keeping the single-binary, low-resource footprint.

## Install

### Homebrew (recommended)

```bash
brew tap drpedapati/tap
brew install drpedapati/tap/sciclaw
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

These CLI tools integrate with sciClaw's built-in skills:

```bash
brew install henrybloomingdale/tools/docx-review   # Word docs with tracked changes (Open XML SDK)
brew install henrybloomingdale/tools/pubmed-cli     # PubMed search & citation graphs
```

## Quick Start

**1. Initialize**

```bash
sciclaw onboard
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
sciclaw auth login --provider openai          # OAuth (browser)
sciclaw auth login --provider openai --device-code  # OAuth (headless)
sciclaw auth login --provider anthropic       # Token paste
```

**3. Chat**

```bash
# One-shot
sciclaw agent -m "What pathways are implicated in ALS?"

# Interactive
sciclaw agent

# Override model for one invocation
sciclaw agent --model anthropic/claude-opus-4-6 -m "Review this manuscript draft"

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
| `sciclaw doctor` | Verify deployment: config, tools, skills, auth, gateway |
| `sciclaw doctor --fix` | Auto-fix: sync baseline skills, remove legacy names |
| `sciclaw gateway` | Start chat channel gateway (Telegram, Discord, etc.) |
| `sciclaw auth login` | Authenticate with a provider |
| `sciclaw auth status` | Show stored credentials |
| `sciclaw skills list` | List installed skills |
| `sciclaw skills install <source>` | Install a skill from GitHub or local path |
| `sciclaw cron list` | List scheduled jobs |
| `sciclaw cron add` | Add a scheduled job |

## Providers

sciClaw auto-detects the provider from the model name. Set credentials via `config.json` or `sciclaw auth login`.

| Provider | Models | Auth |
|----------|--------|------|
| **OpenAI** | gpt-5.2, gpt-4o, o3, o4-mini, codex-mini | API key or OAuth |
| **Anthropic** | claude-opus-4-6, claude-sonnet-4-5 | API key or token paste |
| **Gemini** | gemini-2.5-pro, gemini-2.5-flash | API key |
| **OpenRouter** | All models via `openrouter/` prefix | API key |
| **DeepSeek** | deepseek-chat, deepseek-reasoner | API key |
| **Groq** | Fast inference + Whisper voice transcription | API key |
| **Zhipu** | GLM models | API key |

> Groq provides free voice transcription via Whisper. When configured, Telegram voice messages are automatically transcribed.

## Reasoning Effort

The `--effort` flag controls how deeply the model thinks before answering. Critical for reasoning models where effort directly affects quality, latency, and cost.

| Provider | Valid levels | Default |
|----------|-------------|---------|
| OpenAI / Codex | `none` · `minimal` · `low` · `medium` · `high` · `xhigh` | `medium` |
| Anthropic / Claude | `low` · `medium` · `high` · `max` | Standard (no thinking) |

```bash
sciclaw agent --effort high -m "Complex analysis"
sciclaw agent --model codex-mini-latest --effort xhigh -m "Debug this"
sciclaw agent --model anthropic/claude-opus-4-6 --effort max -m "Prove this theorem"

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
- **docx-review** — Word documents with tracked changes, comments, and semantic diff ([CLI tool](https://github.com/henrybloomingdale/docx-review))
- **pptx** — PowerPoint creation and editing
- **pdf** — PDF creation, merging, splitting, and extraction
- **xlsx** — Spreadsheet creation, analysis, and conversion

### Polish
- **humanize-text** — Final-pass language polishing for natural tone

Additional skills are available from the [skills catalog](https://github.com/drpedapati/sciclaw-skills):

```bash
sciclaw skills install drpedapati/sciclaw-skills/<skill-name>
```

## Skills Installer

The skills installer validates all content before writing to disk:

- **Size limits** — 256KB per skill, 1MB for catalog downloads
- **Binary rejection** — NUL byte detection prevents non-text content
- **Frontmatter validation** — Skills must have valid YAML frontmatter with a `name` field
- **Provenance logging** — Every install writes a `.provenance.json` with source URL, SHA-256, timestamp, and size

## IRL Integration

sciClaw integrates with [IRL](https://github.com/drpedapati/irl-template) (Idempotent Research Loop) for project lifecycle management. IRL is installed automatically as a Homebrew dependency.

The agent mediates IRL operations — creating projects, adopting existing directories, discovering past work — through natural conversation:

```bash
sciclaw agent -m "Create a new project for ERP correlation analysis"
sciclaw agent -m "What projects do I have?"
sciclaw agent -m "Adopt my old als-biomarker folder as a managed project"
```

Every IRL command is recorded in `~/.picoclaw/workspace/irl/commands/` for auditability.

## Chat Channels

Talk to sciClaw through messaging apps by running `sciclaw gateway`.

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
2. Get your user ID from `@userinfobot`
3. Add to `~/.picoclaw/config.json`:

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "YOUR_BOT_TOKEN",
      "allowFrom": ["YOUR_USER_ID"]
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
      "allowFrom": ["YOUR_USER_ID"]
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
~/.picoclaw/workspace/
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

Run `sciclaw doctor` to verify your deployment — config, workspace, auth credentials, companion tools, baseline skills, gateway health, and Homebrew update status:

```bash
sciclaw doctor            # Human-readable report
sciclaw doctor --json     # Machine-readable output
sciclaw doctor --fix      # Auto-fix: sync baseline skills, remove legacy skill names
```

The doctor checks:
- **Config & workspace** — validates `~/.picoclaw/config.json` and workspace directory exist
- **Auth credentials** — checks OpenAI OAuth status (authenticated, expired, needs refresh)
- **Companion tools** — verifies `docx-review`, `pubmed-cli`, `irl`, `pandoc`, `rg`, `python3` are installed, with install hints for missing tools
- **Baseline skills** — confirms all 12 skills are present, detects legacy skill names (`docx`, `pubmed-database`)
- **Gateway health** — scans logs for Telegram 409 conflicts (multiple bot instances)
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
