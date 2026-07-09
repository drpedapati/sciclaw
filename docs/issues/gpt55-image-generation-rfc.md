# RFC: GPT Image Generation in sciClaw Chat

## Status

Implemented MVP on this branch (Codex/Responses hosted `image_generation` tool).

What shipped:
- Codex tool translation appends `{type: "image_generation"}` whenever function tools are present
- `parseCodexResponse` decodes `image_generation_call` base64 into `LLMResponse.Media`
- Agent persists PNGs under `workspace/artifacts/generated/` and attaches them on outbound chat messages

Remaining gaps:
- API-key Chat Completions path still has no image generation
- Multi-turn image edit / inbound photo edit not wired
- Enable/disable config flag and cost controls still open

## Goal

Let a user say something like:

> Image, generate a schematic of TDP-43 condensate formation under stress

…and have sciClaw generate an image, save it in the workspace, and send it back on Telegram/Discord/email as an attachment.

This RFC covers:

1. How OpenAI’s current GPT image APIs actually work (including GPT-5.5)
2. How sciClaw’s AI / tool layer is structured today
3. Why a naive “just enable image gen on the model” approach will fail
4. Conflicting image-related skills and how to disambiguate them
5. A phased, feasible implementation plan

## Research Summary: How To Call It

OpenAI exposes **two** official routes. They are easy to conflate.

### Route A — Image API (direct)

Best when the image is the product of the call.

```http
POST https://api.openai.com/v1/images/generations
Authorization: Bearer $OPENAI_API_KEY
Content-Type: application/json

{
  "model": "gpt-image-2",
  "prompt": "A schematic of TDP-43 condensate formation under stress",
  "size": "1024x1024",
  "quality": "high"
}
```

Response includes base64 (`b64_json`) image bytes. Edits use `/v1/images/edits`.

Current GPT Image model family (as of OpenAI docs, 2026):

| Model | Role |
| --- | --- |
| `gpt-image-2` | Latest / recommended for new work |
| `gpt-image-1.5` | Prior generation |
| `gpt-image-1` | Original GPT Image |
| `gpt-image-1-mini` | Cheaper / faster |

**Important:** `gpt-image-2` is **not** a valid `model` for the Responses API chat turn. It is the image model behind generation.

Org verification may be required in the OpenAI developer console before GPT Image models work.

### Route B — Responses API `image_generation` tool (conversational)

Best when image generation is one step inside an agent turn. Official GPT-5.5 example:

```js
const response = await openai.responses.create({
  model: "gpt-5.5",
  input: "Generate an image of gray tabby cat hugging an otter with an orange scarf",
  tools: [{ type: "image_generation" }],
});

const imageData = response.output
  .filter((o) => o.type === "image_generation_call")
  .map((o) => o.result); // base64
```

Key details:

- Mainline model is `gpt-5.5` / `gpt-5.2` / etc.
- Hosted tool type is `image_generation` (not a sciClaw function tool)
- Optional `tool_choice: { type: "image_generation" }` forces the call
- Optional tool params: `action` (`auto` | `generate` | `edit`), `size`, `quality`, `output_format`, and an image-model override
- Output item type is `image_generation_call` with base64 in `result`
- Multi-turn edit works by keeping prior `image_generation_call` items (or IDs) in context

Sources:

- https://developers.openai.com/api/docs/guides/image-generation
- https://developers.openai.com/api/docs/guides/tools-image-generation
- https://developers.openai.com/api/docs/models/gpt-image-2

## Current sciClaw Architecture (relevant bits)

### Provider split

`CreateProvider` in `pkg/providers/http_provider.go`:

| Auth / config | Provider | Wire protocol |
| --- | --- | --- |
| OpenAI API key | `HTTPProvider` | Chat Completions `POST /chat/completions` |
| OpenAI OAuth / stored token | `CodexProvider` | Responses streaming via `chatgpt.com/backend-api/codex` |
| Anthropic | Claude providers | Anthropic Messages / agent bridge |
| PHI mode | Ollama / local | Local OpenAI-compatible |

Implications:

1. **API-key OpenAI path cannot use the hosted `image_generation` tool today** — it never calls `/v1/responses`.
2. **OAuth Codex path already speaks Responses**, but `buildCodexParams` / `translateToolsForCodex` only emit **function** tools, and `parseCodexResponse` only handles `message` + `function_call`. It **drops** `image_generation_call` output items.
3. Community reports suggest Codex OAuth tokens often **cannot** call `/v1/images/*` directly; those endpoints expect a normal API key. Treat OAuth image support as unverified until smoke-tested.

### Agent tool loop

`pkg/agent/loop.go` builds a `tools.ToolRegistry` of **local** tools (`read_file`, `exec`, `message`, PubMed, review tools, etc.). The model proposes function calls; sciClaw executes them and feeds results back.

This is the opposite of OpenAI’s hosted `image_generation` tool, where OpenAI executes the tool server-side and returns image bytes in the response.

### Delivery already exists

Channels already send files:

- Telegram: photo/document via `OutboundAttachment`
- Discord: file upload via `OutboundAttachment`
- Email: base64 attachments

The `message` tool already accepts `attachments: [{ path, filename }]`.

So once an image exists on disk under the workspace, delivery is mostly solved.

### Skills are prompt packs, not executors

`pkg/skills/loader.go` injects `SKILL.md` text into context. Skills do **not** register Go tools. A skill alone cannot call OpenAI image APIs.

## Conflicting “Image” Surfaces

These are not the same product. The agent will thrash without explicit routing rules.

| Surface | What it actually does | When to use |
| --- | --- | --- |
| `skills/imagemagick` | Deterministic `magick` preprocess (resize/crop/DPI) | Existing scientific figures |
| `skills/beautiful-mermaid` | Diagram source → SVG | Architecture / methods diagrams |
| `skills/explainer-site` | HTML explainer pages (often with Mermaid) | Educational sites |
| Cursor Higgsfield / GenerateImage skills | IDE-side image/video generation | Cursor agent sessions only — **not** sciClaw runtime |
| Proposed `generate_image` tool | OpenAI GPT Image → workspace file | “Image, generate …” chat requests |
| Future Responses `image_generation` | Hosted tool on GPT-5.5 turns | Multi-turn edit inside OpenAI Responses path |

**Decision for v1:** keep ImageMagick / Mermaid / explainer skills unchanged. Add a **typed Go tool** for generative images, plus a thin skill that teaches the model when to call it vs preprocess/diagram skills.

## Feasibility Verdict

| Approach | Feasible now? | Notes |
| --- | --- | --- |
| **A. Local `generate_image` tool → Image API (`gpt-image-2`)** | **Yes — recommended MVP** | Works with API key; fits existing tool loop; saves file; `message` attaches it. Provider-agnostic for Anthropic users who still have an OpenAI key for images. |
| **B. Enable Responses hosted `image_generation` on Codex** | Medium / later | Needs tool translation, response parsing, disk write, and Codex backend capability verification. |
| **C. Migrate `HTTPProvider` to Responses + hosted tool** | Large | Touches every OpenAI API-key chat turn; high regression risk. |
| **D. Skill-only (no Go tool)** | No | Skills cannot call APIs; model would invent shell/`curl` hacks. |
| **E. Force model to shell out to `curl`** | No | Fragile, secret leakage risk, poor UX. |

**Recommended path: A first, B second.**

## Proposed UX

Natural language (no slash required for MVP):

- “Image, generate a lab schematic of …”
- “Generate an image of …”
- “Make a figure illustration of …”

Optional later Discord surface (fits existing `/skill` pattern):

- `/skill name:image-gen prompt:…`

Agent behavior:

1. Call `generate_image` with the user prompt (and optional size/quality).
2. Tool writes `workspace/artifacts/generated/<timestamp>-<slug>.png` (or configured dir).
3. Tool returns path + revised prompt metadata.
4. Agent calls `message` with that path as an attachment (and a short caption).

Routing rules to encode in skill + tool description:

- Generative / illustrative / “make an image” → `generate_image`
- Resize / DPI / convert existing asset → `imagemagick`
- Flowchart / architecture diagram → `beautiful-mermaid`
- Do not invent fake citations or photorealistic “experimental results” presented as real data

## Implementation Plan

### Phase 0 — Smoke tests (before coding much)

1. With a verified OpenAI **API key**, call `/v1/images/generations` with `model: "gpt-image-2"`.
2. With the same key, call `/v1/responses` with `model: "gpt-5.5"` and `tools: [{type:"image_generation"}]`.
3. With sciClaw **OAuth / Codex** credentials, attempt both; record which fail.
4. Confirm org verification status if 403/verification errors appear.

Exit criteria: know which auth modes can generate images in this account.

### Phase 1 — MVP local tool (shippable)

Files (expected):

- `pkg/tools/generate_image.go` (+ tests)
- Register in `pkg/agent/loop.go` `createToolRegistry`
- `skills/image-gen/SKILL.md` (routing + provenance notes)
- AGENTS.md baseline skill bullet
- Config knobs (optional): enable flag, default model `gpt-image-2`, default size/quality, output dir

Tool sketch:

```text
name: generate_image
params:
  prompt: string (required)
  size?: "1024x1024" | "1024x1536" | "1536x1024" | "auto"
  quality?: "low" | "medium" | "high"
  filename?: string
behavior:
  - resolve OpenAI API key from config/env (not Codex OAuth by default)
  - POST /v1/images/generations
  - decode b64 → workspace file
  - return { path, model, size, revised_prompt? }
```

Auth policy for MVP:

- Require OpenAI API key (`config.providers.openai.api_key` or env).
- If user is on Anthropic for chat but has an OpenAI key configured, still allow image gen.
- If only OAuth Codex creds exist, return a clear error: “Image generation needs an OpenAI API key (OAuth/Codex path not supported yet).”

Tests:

- Mock HTTP server for generations response
- Workspace path restriction
- Missing-key error
- Attachment round-trip via `message` tool args (unit-level)

### Phase 2 — Chat intent reliability

- Strengthen tool description + `image-gen` skill so “Image, generate …” reliably selects the tool
- Optional lightweight intent hint in context builder when message matches `/^\s*image[,:]/i` (keep tiny; prefer model+tool over brittle regex)
- Doctor check: warn if image skill enabled but no OpenAI API key

### Phase 3 — Responses / GPT-5.5 hosted tool (optional)

Only after Phase 0 proves Codex or API-key Responses works:

1. Extend `translateToolsForCodex` (or a dedicated Responses OpenAI provider) to append `{type: "image_generation", ...}`.
2. Extend `parseCodexResponse` to handle `image_generation_call`, write bytes to disk, and surface a synthetic local tool result or attachment hint.
3. Decide whether hosted tool **replaces** or **supplements** the local `generate_image` tool (recommend: keep local tool as the portable path; hosted tool as OpenAI-only enhancement for multi-turn edit).

### Phase 4 — Edits / references

- Accept inbound chat photos as edit inputs (`/v1/images/edits` or Responses `action: "edit"`)
- Mask support later if needed

## Risks & Open Questions

1. **OAuth vs API key:** Can Codex backend host `image_generation` for sciClaw OAuth users? Unknown until smoke-tested.
2. **Cost / abuse:** Image gen is expensive; need enable flag + maybe per-workspace allowlist.
3. **Scientific integrity:** Generated illustrations must not be presented as real experimental data. Skill copy must say so.
4. **Vendor SDK age:** Vendored `openai-go/v3` documents older GPT Image models in some comments; Image API may still accept `gpt-image-2` via raw HTTP even if SDK enums lag. Prefer thin HTTP client in the tool (like `WeatherForecastTool`) or bump vendor.
5. **PHI mode:** Local models cannot call OpenAI image APIs; tool must no-op/error clearly in PHI mode.
6. **Cursor skill confusion:** Higgsfield / GenerateImage skills live in the IDE, not sciClaw. Document that they are out of scope for runtime chat.

## Non-Goals (this effort)

- Replacing ImageMagick preprocessing
- Building a general media studio UI
- Migrating all OpenAI traffic to Responses in one PR
- Supporting Midjourney / Flux / Higgsfield as sciClaw backends

## Suggested PR Sequence (after this plan PR)

1. `feat: add generate_image tool (gpt-image-2 Image API)`
2. `feat: image-gen skill + AGENTS routing`
3. `feat: doctor/config for image generation credentials`
4. (optional) `feat: Responses image_generation on OpenAI path`

## Acceptance Criteria for the Feature (not this plan PR)

- [ ] User can ask in Telegram/Discord to generate an image and receive a photo/file back
- [ ] Artifact is saved under the workspace with a stable path
- [ ] ImageMagick / Mermaid requests still route correctly
- [ ] Missing API key / PHI mode produce actionable errors
- [ ] Unit tests cover the tool without live OpenAI calls

## Appendix: Why Not Only Responses + GPT-5.5?

GPT-5.5 + `tools: [{type:"image_generation"}]` is the cleanest **OpenAI-native** agent story. sciClaw’s difficulty is not the API shape — it is that:

1. Most of the tool ecosystem is **local function tools** on a Chat Completions-shaped loop.
2. The Responses-capable path (`CodexProvider`) does not yet understand image tool outputs.
3. Many installs will want image gen while chatting on Anthropic.

A local `generate_image` tool sidesteps all three for MVP, while leaving the door open to hosted Responses image gen later.
