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

## How You Talk to sciClaw

The most natural way to use sciClaw is through **Telegram** or **Discord**. You message it like you'd message a colleague. Ask a question, attach a file, get results back. No terminal, no special syntax.

```
You:      "Find recent papers on TDP-43 proteinopathy in ALS"
sciClaw:  [searches PubMed, returns 47 papers with citations, saves to workspace]

You:      "Draft a methods section using the attached protocol"
sciClaw:  [produces a Word doc with tracked changes you review in Microsoft Word]
```

After install, connect a chat app and start the gateway:

```bash
sciclaw channels setup telegram    # or: sciclaw channels setup discord
sciclaw service install && sciclaw service start
```

That's it. sciClaw runs in the background and responds to your messages. See the [Scientist Setup Guide](https://sciclaw.dev/docs.html#scientist-setup) for a walkthrough.

> A CLI is also available for power users: `sciclaw agent -m "your question"` or `sciclaw agent` for interactive mode.

## Install

### Homebrew (recommended)

```bash
brew tap drpedapati/tap && brew install sciclaw
```

Then run the interactive setup wizard:
```bash
sciclaw onboard
```

macOS only:
```bash
brew install --cask quarto
```

### Download a binary

Pre-compiled binaries for macOS (arm64), Linux (amd64, arm64, riscv64), and Windows (amd64/WSL) are on the [releases page](https://github.com/drpedapati/sciclaw/releases).

### From source

```bash
git clone https://github.com/drpedapati/sciclaw.git
cd sciclaw
make deps && make install
```

Homebrew pulls companion tools automatically (ImageMagick, IRL, ripgrep, docx-review, pubmed-cli). For binary/source installs, run `sciclaw doctor` for hints.

## Quick Start

**1. Initialize** — the wizard walks you through everything:

```bash
sciclaw onboard
```

**2. Authenticate** with your AI provider:

```bash
sciclaw auth login --provider openai     # OAuth device code — works with your ChatGPT account
sciclaw auth login --provider anthropic  # Token paste
```

**3. Connect a chat app** and start messaging:

```bash
sciclaw channels setup telegram
sciclaw service install && sciclaw service start
```

See [Authentication docs](https://sciclaw.dev/docs.html#authentication) for all providers. See [Chat Channels](#chat-channels) below for Telegram and Discord setup details.

## Security

sciClaw's default posture is **local, private, and locked down**. Here's what that means in practice:

- **Runs on your machine.** sciClaw is a program on your computer, not a cloud service. There's no account to create with us, no server to connect to, nothing hosted anywhere.
- **Your data stays in one folder.** Everything sciClaw produces lives in `~/sciclaw` — a folder on your machine that you own and control. You can open it, back it up, or delete it anytime.
- **Nothing is exposed to the internet.** sciClaw doesn't open any ports or listen for incoming connections. It reaches out only when you send a message, and only to the AI provider you chose (OpenAI, Anthropic, etc.) and any tools you explicitly enable (like PubMed).
- **Messages go through your private bot.** When you chat via Telegram or Discord, messages travel through a bot that only you control. Nobody else can talk to it unless you explicitly allow them.
- **No telemetry, no analytics, no tracking.** sciClaw sends nothing back to us. No usage data, no error reports, no phone-home behavior. We don't know you're running it.
- **API keys stay local.** Your credentials are stored in a config file on your machine (`~/.picoclaw/config.json`). They're never transmitted to anyone except the provider they belong to.
- **Skills are validated before install.** Every skill goes through size limits, binary rejection, frontmatter validation, and SHA-256 provenance logging. Catalog fetches use pinned commit refs for supply-chain hardening.

For the full security model, see [Security](https://sciclaw.dev/security.html).

## Providers

sciClaw auto-detects the provider from the model name. Set credentials via the onboard wizard or `sciclaw auth login`.

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

## Built-in Skills

Fifteen skills are installed during `sciclaw onboard`:

### Research & Literature
- **scientific-writing** — Manuscript drafting with claim-evidence alignment
- **pubmed-cli** — PubMed search, article fetch, citation graphs, MeSH lookup ([CLI tool](https://github.com/drpedapati/pubmed-cli))
- **biorxiv-database** — bioRxiv/medRxiv preprint surveillance

### Authoring & Visualization
- **quarto-authoring** — Loop-driven `.qmd` authoring and rendering
- **pandoc-docx** — Clean `.docx` manuscript generation from Markdown with NIH template auto-apply
- **imagemagick** — Reproducible image preprocessing (resize, crop, convert, DPI normalization) via `magick`
- **beautiful-mermaid** — Publication-grade Mermaid diagrams
- **explainer-site** — Technical, single-page "How X Works" explainer site generation

### Evidence & Provenance
- **experiment-provenance** — Reproducible experiment metadata capture
- **benchmark-logging** — Benchmark records with acceptance criteria

### Office & Documents
- **docx-review** — Word tracked-change review, comments, semantic diff, and template population (`--create`, v1.3.0+) ([CLI tool](https://github.com/drpedapati/docx-review))
- For clean first-draft Word output, use `pandoc ... -o file.docx`; sciClaw injects `--defaults <generated-file>` at runtime to apply a bundled NIH reference template (no global `~/.pandoc/defaults.yaml` required).
- **pptx** — PowerPoint creation and editing
- **pdf** — PDF creation, merging, splitting, and extraction
- **xlsx** — Spreadsheet creation, analysis, and conversion

### Polish
- **humanize-text** — Final-pass language polishing for natural tone

### Optional
- **phi-cleaner** — Clinical text de-identification for PHI-safe sharing (`brew install drpedapati/tools/phi-cleaner`)

Additional skills: [skills catalog](https://github.com/drpedapati/sciclaw-skills) — install with `sciclaw skills install drpedapati/sciclaw-skills/<name>`

## Chat Channels

Telegram and Discord are the recommended way to interact with sciClaw. You message it from the app you already have open.
When the agent generates deliverables (for example `.docx`), it can now send real file attachments back through Discord/Telegram via the `message` tool.

<details>
<summary><strong>Telegram setup</strong> (easiest)</summary>

1. Open Telegram, search `@BotFather`, send `/newbot`, copy the token
2. Run `sciclaw channels setup telegram` (pairs your account and writes config)
3. Start the gateway: `sciclaw service install && sciclaw service start`

Manual config (advanced) in `~/.picoclaw/config.json`:

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

</details>

<details>
<summary><strong>Discord setup</strong></summary>

1. Create app at [discord.com/developers](https://discord.com/developers/applications)
2. Enable **MESSAGE CONTENT INTENT** in Bot settings
3. Copy bot token, get your User ID (Developer Mode → right-click → Copy User ID)
4. Run `sciclaw channels setup discord` or add to config manually
5. Generate invite URL (scopes: `bot`; permissions: Send Messages, Read Message History)
6. Start the gateway: `sciclaw service install && sciclaw service start`

</details>

**Background service** (recommended — keeps sciClaw running):
```bash
sciclaw service install    # register with launchd (macOS) or systemd (Linux)
sciclaw service start
sciclaw service status     # check it's running
```

Platform notes:
- **macOS**: per-user `launchd` (`~/Library/LaunchAgents/io.sciclaw.gateway.plist`)
- **Linux**: `systemd --user` (`~/.config/systemd/user/sciclaw-gateway.service`)
- **WSL**: service mode works when systemd is enabled; otherwise `sciclaw gateway` in a terminal

## IRL Integration

sciClaw integrates with [IRL](https://github.com/drpedapati/irl-template) (Idempotent Research Loop) for project lifecycle management. IRL is installed automatically as a Homebrew dependency.

The agent manages projects through natural conversation:

```bash
sciclaw agent -m "Create a new project for ERP correlation analysis"
sciclaw agent -m "What projects do I have?"
```

Every IRL command is recorded in `~/sciclaw/irl/commands/` for auditability.

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
├── HOOKS.md           # Hook policy (plain-language)
├── IDENTITY.md        # sciClaw identity
├── SOUL.md            # Agent values & guardrails
├── TOOLS.md           # Tool descriptions
└── USER.md            # User preferences
```

## Docker

```bash
git clone https://github.com/drpedapati/sciclaw.git && cd sciclaw
cp config/config.example.json config/config.json   # edit with your credentials
docker compose --profile gateway up -d              # gateway mode
docker compose run --rm sciclaw-agent -m "Hello"    # one-shot
```

## Troubleshooting

Run `sciclaw doctor` to diagnose issues — it checks config, auth, tools, skills, gateway, and service health.

```bash
sciclaw doctor            # human-readable report
sciclaw doctor --fix      # auto-fix common issues
```

<details>
<summary>Common issues</summary>

**"no credentials for openai/anthropic"**
```bash
sciclaw auth login --provider openai
```

**Telegram "Conflict: terminated by other getUpdates"** — only one gateway instance can run at a time:
```bash
sciclaw service restart
```

**Web search "API configuration problem"** — get a free key at [brave.com/search/api](https://brave.com/search/api) and add to config under `tools.web.search.api_key`.

</details>

## Updating

```bash
brew upgrade sciclaw     # update the binary
sciclaw onboard          # refresh skills to latest
sciclaw doctor           # verify everything
```

<details>
<summary>CLI reference</summary>

| Command | Description |
|---------|-------------|
| `sciclaw onboard` | Initialize config, workspace, and baseline skills |
| `sciclaw agent -m "..."` | One-shot message |
| `sciclaw agent` | Interactive chat |
| `sciclaw agent --model <m>` | Override model |
| `sciclaw agent --effort <level>` | Set reasoning effort (`none` through `xhigh`) |
| `sciclaw models list` | Show current model and providers |
| `sciclaw models set <model>` | Change default model |
| `sciclaw models effort <level>` | Change default effort |
| `sciclaw status` | System status |
| `sciclaw doctor` | Verify deployment |
| `sciclaw doctor --fix` | Auto-fix common issues |
| `sciclaw gateway` | Start chat gateway |
| `sciclaw service install\|start\|stop\|restart\|logs\|uninstall` | Manage background service |
| `sciclaw channels setup <channel>` | Configure a chat channel |
| `sciclaw auth login\|status` | Manage credentials |
| `sciclaw skills list\|install` | Manage skills |
| `sciclaw cron list\|add` | Manage scheduled jobs |

</details>

## License

MIT — see [LICENSE](LICENSE).

sciClaw is a fork of [PicoClaw](https://github.com/sipeed/picoclaw) by Sipeed, which is based on [nanobot](https://github.com/HKUDS/nanobot) by HKUDS.
