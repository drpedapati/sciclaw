# Claude OAuth Parity

This tracks the contract parity between:

- direct Anthropic API path: `ClaudeProvider`
- Claude.ai oat-token path: `ClaudeAgentProvider` + `sciclaw-claude-agent`

Scope is the sciClaw provider contract, not full Claude Code feature parity.

## Option Parity Tracker

| Option / behavior | Direct Claude API provider | Claude agent bridge path | Status |
| --- | --- | --- | --- |
| `model` | Supported | Supported | Parity |
| `messages` | Supported | Supported | Parity |
| `tools` | Supported | Supported | Parity |
| native system prompt handling | Supported | Supported through bridge `systemPrompt` | Parity |
| `reasoning_effort` | Sent as adaptive thinking + effort | Sent as `effort` plus adaptive thinking | Parity |
| explicit `thinking` | Not a first-class direct-provider option in current Go code | Supported on bridge path | Bridge-only additive support |
| `persist_session` | Not applicable | Supported, but bridge defaults to stateless | Intentionally stateless by default |
| `additional_directories` | Not applicable | Supported | Bridge-only additive support |
| workspace cwd | Not applicable | Supported | Parity |
| `max_tokens` | Supported | Not exposed by Agent SDK query options | Upstream-limited gap |
| `temperature` | Supported when thinking is not enabled | Not exposed by Agent SDK query options | Upstream-limited gap |

## Fixed

- oat-token detection and reroute
  - tests: `TestCreateProvider_AnthropicOATAPIKeyUsesClaudeAgentProvider`
  - tests: `TestCreateProvider_StoredAnthropicOATUsesClaudeAgentProvider`
- model normalization for `anthropic/...` and dotted aliases
  - bridge tests: `normalizeClaudeModel strips provider prefix and dots`
- tool-call round trips through sciClaw's own tool loop
  - covered by live smoke tests during implementation
- native system-prompt handling on the bridge path
  - bridge tests: `buildSystemPrompt carries system instructions and tool section`
  - bridge tests: `buildPrompt excludes system messages and preserves transcript`
- explicit thinking / effort passthrough
  - tests: `TestClaudeAgentProvider_ChatRequestPassthrough`
  - tests: `TestClaudeAgentProvider_ChatThinkingBudgetPassthrough`
- explicit stateless behavior
  - bridge defaults `persistSession` to false unless explicitly requested
  - tests: `TestClaudeAgentProvider_ChatSuccess`
- additional directory passthrough
  - tests: `TestClaudeAgentProvider_ChatRequestPassthrough`

## Intentionally Not a Parity Target

- Claude Code built-in tools / MCP runtime
  - sciClaw keeps using its own tool loop on both Anthropic paths
- session persistence to disk
  - direct Anthropic API is stateless, so the bridge now defaults to stateless too

## Upstream-Limited Gaps

- `max_tokens`
  - direct Anthropic API path supports this
  - Agent SDK query options do not expose an equivalent max output token setting
  - current behavior: oat bridge does not pass it through
  - tests: `TestClaudeAgentProvider_ChatRequestPassthrough`
- `temperature`
  - direct Anthropic API path supports this
  - Agent SDK query options do not expose temperature control
  - current behavior: oat bridge does not pass it through
  - tests: `TestClaudeAgentProvider_ChatRequestPassthrough`

## Notes

- If Anthropic adds max-output-token or temperature controls to the Agent SDK query surface, these are the first parity gaps to close.
- Until then, the bridge path is intentionally explicit about what it does and does not honor.
