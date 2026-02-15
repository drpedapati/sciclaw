# Security Review: Chinese-Language Code and Call-Home Analysis

**Date:** 2026-02-15
**Scope:** Full codebase review of `sciclaw` (fork of `sipeed/picoclaw`)
**Focus:** Chinese-language code patterns, outbound network connections ("call home"), and data exfiltration risks

---

## Executive Summary

sciClaw is a Go fork of PicoClaw, an ultra-lightweight personal AI agent created by **Sipeed**, a Shenzhen-based hardware company. The codebase contains **10 files with Chinese-language content** (comments, documentation, and code). **No malicious call-home, beacon, or data exfiltration patterns were found.** All outbound network connections are legitimate, user-configured, and transparent.

However, several security concerns were identified, most notably that the **default LLM provider routes all user data to Chinese cloud services** (Zhipu GLM at `open.bigmodel.cn`), and the skills installer can fetch and execute arbitrary content from GitHub without validation.

---

## 1. Chinese-Language Code Inventory

### Files Containing Chinese Characters (10 total)

| File | Type | Chinese Content |
|------|------|----------------|
| `README.zh.md` | Documentation | Full Chinese README (1,556 lines) |
| `README.ja.md` | Documentation | Chinese tagline embedded in Japanese docs |
| `README.md` | Documentation | Chinese tagline only |
| `pkg/channels/qq.go` | Source | All comments in Chinese |
| `pkg/channels/dingtalk.go` | Source | Type doc comment in Chinese |
| `pkg/channels/telegram.go` | Source | Comments in Chinese |
| `pkg/channels/discord.go` | Source | Comments in Chinese |
| `pkg/channels/slack.go` | Source | Comments in Chinese |
| `pkg/skills/loader.go` | Source | Comments in Chinese |
| `cmd/picoclaw/main.go` | Source | Comments in Chinese |

### Analysis

Chinese comments appear throughout **all** channel implementations, not just Chinese-specific platforms (QQ, DingTalk). This confirms the original development team is Chinese-speaking. Examples:

- `// 检查白名单，避免为被拒绝的用户下载附件` ("Check allowlist, avoid downloading attachments for rejected users") - appears in telegram.go, discord.go, slack.go
- `// 确保临时文件在函数返回时被清理` ("Ensure temporary files are cleaned up when function returns") - appears in multiple channel files
- `// 项目级别` ("project level"), `// 全局 skills` ("global skills"), `// 内置 skills` ("built-in skills") - in loader.go

**Assessment:** The Chinese comments are developer documentation, not obfuscation. The code logic is straightforward and matches the comments. No hidden functionality was found behind Chinese-language comments.

---

## 2. Call-Home / Beacon Analysis

### Outbound Network Connections Found

| Endpoint | File | Purpose | User-Initiated? |
|----------|------|---------|----------------|
| `raw.githubusercontent.com/sipeed/picoclaw-skills/main/skills.json` | `pkg/skills/installer.go:96` | Skills catalog | Yes (search cmd) |
| `raw.githubusercontent.com/<repo>/main/SKILL.md` | `pkg/skills/installer.go:46` | Skill install | Yes (install cmd) |
| `open.bigmodel.cn/api/paas/v4` | `pkg/providers/http_provider.go:276` | Zhipu GLM API | Config-driven |
| `api.moonshot.cn/v1` | `pkg/providers/http_provider.go:329` | Moonshot/Kimi API | Config-driven |
| `router.shengsuanyun.com/api/v1` | `pkg/providers/http_provider.go:296` | ShengSuanYun API | Config-driven |
| `api.deepseek.com/v1` | `pkg/providers/http_provider.go:310` | DeepSeek API | Config-driven |
| `api.anthropic.com/v1` | `pkg/providers/http_provider.go:259` | Anthropic API | Config-driven |
| `api.openai.com/v1` | `pkg/providers/http_provider.go:249` | OpenAI API | Config-driven |
| `openrouter.ai/api/v1` | `pkg/providers/http_provider.go:268` | OpenRouter API | Config-driven |
| `api.groq.com/openai/v1` | `pkg/providers/http_provider.go:238` | Groq API | Config-driven |
| `integrate.api.nvidia.com/v1` | `pkg/providers/http_provider.go:391` | NVIDIA API | Config-driven |
| `generativelanguage.googleapis.com/v1beta` | `pkg/providers/http_provider.go:284` | Google Gemini API | Config-driven |
| `auth.openai.com` | `pkg/auth/oauth.go:30` | OpenAI OAuth | User-initiated |
| Telegram/Discord/Slack/QQ/DingTalk/Feishu APIs | `pkg/channels/*` | Messaging platforms | Config-driven |

### Periodic/Background Operations

| Service | File | Interval | External? |
|---------|------|----------|-----------|
| Heartbeat | `pkg/heartbeat/service.go` | 30 min (default) | No - reads local `HEARTBEAT.md`, sends results to user's channel |
| Cron | `pkg/cron/service.go` | User-configured | No - executes user-defined tasks locally |

### Verdict

**No malicious call-home, beacon, phone-home, or data exfiltration patterns found.**

- No hidden telemetry or analytics SDKs
- No obfuscated code (`eval`, `exec`, base64 tricks, etc.)
- No environment variable harvesting beyond `PICOCLAW_*` prefixed config
- No system fingerprinting sent to external servers
- All network connections serve documented, user-facing functionality
- The heartbeat is NOT a beacon - it processes a local file and sends results to the user's own messaging channel

---

## 3. Security Findings

### MEDIUM Severity

#### M1: Default LLM Provider Routes Data to Chinese Cloud

**Location:** `pkg/config/config.go:213`
```go
Model: "glm-4.7",
```

The default configuration sets the LLM model to `glm-4.7` (Zhipu GLM), which routes all prompts and conversation data to `https://open.bigmodel.cn/api/paas/v4`, a server operated by Zhipu AI in China. Users who run `sciclaw onboard` and add an API key without changing the model will unknowingly send all their data to Chinese infrastructure.

Additional Chinese LLM providers available:
- **Moonshot/Kimi**: `api.moonshot.cn` (Beijing Moonshot AI)
- **ShengSuanYun**: `router.shengsuanyun.com`
- **DeepSeek**: `api.deepseek.com`

**Impact:** Data residency violation for users subject to GDPR, HIPAA, or organizational policies prohibiting data transfer to China. Scientific/medical data from the sciClaw research workflows could be particularly sensitive.

**Recommendation:** Change the default model to a non-region-specific option (e.g., require explicit provider selection during onboard). Document data residency implications prominently in README.

---

#### M2: Skills Installer Fetches and Installs Arbitrary Content

**Location:** `pkg/skills/installer.go:39-78`

```go
func (si *SkillInstaller) InstallFromGitHub(ctx context.Context, repo string) error {
    url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/SKILL.md", repo)
    // ... fetches content and writes to local filesystem
}
```

The `InstallFromGitHub` function accepts any GitHub repo path and fetches its `SKILL.md` content. This content is then loaded by `SkillsLoader` and injected into LLM prompts as context. There is no:

- Content validation or sanitization
- Signature verification
- Size limits on downloaded content
- Allowlist of trusted skill sources

**Impact:** A malicious skill could contain prompt injection attacks that alter the agent's behavior, exfiltrate data through crafted prompts, or manipulate the scientific workflows.

**Recommendation:** Add integrity checks (signatures/hashes), content size limits, and a curated skills registry with trust levels.

---

#### M3: Skills Catalog Controlled by Upstream Chinese Entity

**Location:** `pkg/skills/installer.go:96`

```go
url := "https://raw.githubusercontent.com/sipeed/picoclaw-skills/main/skills.json"
```

The skills search/discovery feature fetches the catalog from Sipeed's GitHub repository. If this repository is compromised (or Sipeed pushes malicious content), users running `skills search` could be directed to install malicious skills.

**Impact:** Supply chain risk through the skills catalog.

**Recommendation:** Pin skills catalog to a specific commit hash or maintain a fork.

---

### LOW Severity

#### L1: Config File Written World-Readable

**Location:** `pkg/config/config.go:352`

```go
return os.WriteFile(path, data, 0644)
```

The config file (containing API keys, bot tokens, and client secrets) is written with `0644` permissions, making it readable by all users on the system.

**Recommendation:** Use `0600` permissions for files containing secrets.

---

#### L2: BaseChannel.running Has Data Race

**Location:** `pkg/channels/base.go:42-44, 106-108`

The `running` field is a plain `bool` accessed without synchronization. `setRunning()` and `IsRunning()` have no mutex protection, creating a data race when channels are started or stopped concurrently.

**Recommendation:** Use `sync/atomic` or add mutex protection.

---

#### L3: QQ Message Dedup Map Grows Unbounded

**Location:** `pkg/channels/qq.go:219-243`

The `processedIDs` map grows until it reaches 10,000 entries, then randomly evicts half. Go map iteration order is non-deterministic, so recently processed IDs may be evicted while old ones are retained, enabling message replay.

**Recommendation:** Use a time-windowed LRU cache or a `sync.Map` with TTL.

---

#### L4: Heartbeat Enabled by Default

**Location:** `pkg/config/config.go:306-308`

```go
Heartbeat: HeartbeatConfig{
    Enabled:  true,
    Interval: 30,
},
```

The heartbeat service is enabled by default and will proactively execute agent turns every 30 minutes based on `HEARTBEAT.md` content. While not malicious, users may not expect background agent activity.

**Recommendation:** Default to disabled; require explicit opt-in.

---

#### L5: OAuth Callback on Fixed Port

**Location:** `pkg/auth/oauth.go:83`

The OAuth callback binds to `127.0.0.1:1455`. If another process is already listening on this port, the OAuth flow will fail. No mechanism exists to try alternative ports.

**Recommendation:** Allow dynamic port selection with a fallback range.

---

### INFORMATIONAL

#### I1: All Dependencies Are Legitimate

The Chinese platform SDKs in `go.mod` are all official, well-maintained libraries:

- `github.com/tencent-connect/botgo` - Official Tencent QQ Bot SDK
- `github.com/open-dingtalk/dingtalk-stream-sdk-go` - Official DingTalk SDK
- `github.com/larksuite/oapi-sdk-go/v3` - Official Feishu/Lark SDK (ByteDance)

All other dependencies (discordgo, telego, slack-go, anthropic-sdk-go, etc.) are widely-used community or official libraries.

#### I2: Project Origin

- **Upstream:** `github.com/sipeed/picoclaw`
- **Sipeed:** A Shenzhen, China-based company primarily known for RISC-V hardware (MaixCAM, etc.)
- The project tagline `皮皮虾，我们走！` ("PicoClaw, Let's Go!") is a Chinese internet meme
- sciClaw adds scientific research capabilities (manuscript authoring, PubMed queries, etc.) on top of the Chinese upstream

#### I3: No Hidden Telemetry

The codebase contains no analytics, no tracking, no usage reporting, and no data collection beyond what's needed for configured functionality. All logs are stored locally.

---

## 4. Summary Matrix

| Category | Finding | Severity |
|----------|---------|----------|
| Default data routing | All data routed to Chinese LLM (Zhipu) by default | MEDIUM |
| Skills supply chain | Arbitrary skill content fetched without validation | MEDIUM |
| Skills catalog | Catalog controlled by upstream Chinese entity | MEDIUM |
| Config security | API keys written world-readable (0644) | LOW |
| Thread safety | BaseChannel.running has data race | LOW |
| Memory safety | QQ dedup map grows unbounded | LOW |
| Default behavior | Heartbeat agent runs by default every 30 min | LOW |
| Auth flow | Fixed OAuth callback port | LOW |
| Call-home / beacon | **None found** | NONE |
| Hidden telemetry | **None found** | NONE |
| Obfuscated code | **None found** | NONE |
| Data exfiltration | **None found** | NONE |

---

## 5. Recommendations for sciClaw Users

1. **Change the default model** from `glm-4.7` to a provider whose data residency aligns with your requirements (e.g., `anthropic/claude-*`, `openai/gpt-*`)
2. **Review `config.json` permissions** and restrict to `0600`
3. **Audit installed skills** before use, especially those from untrusted repositories
4. **Set `heartbeat.enabled: false`** unless you specifically want background agent activity
5. **Configure `allow_from`** on all messaging channels to restrict who can interact with your agent
6. **Fork the skills catalog** rather than relying on Sipeed's upstream repository
