# PHI Mode

PHI Mode runs LLM inference locally on your machine via [Ollama](https://ollama.com) instead of sending requests to cloud providers. No data leaves your machine. This is useful for working with Protected Health Information (PHI), air-gapped environments, or reducing cloud API costs.

## Requirements

- **Ollama** installed from <https://ollama.com>
- One of the following hardware configurations:
  - **macOS ARM64 (Apple Silicon)** with 16GB+ unified RAM
  - **Linux or Windows with NVIDIA GPU** and 6GB+ VRAM
  - **CPU-only** with 6GB+ RAM (any OS; 8GB+ recommended)
- **Models:** Qwen 3.5 family (2B, 4B, or 9B parameters)

## Quick Start

```bash
# Install Ollama first: https://ollama.com
sciclaw modes phi-setup    # detect hardware, pull model, verify
sciclaw modes status       # confirm PHI mode is active
```

That is it. The setup command detects your hardware, selects an appropriate model, pulls it, runs a warmup check, and switches to PHI mode.

## Two Ways to Use PHI Mode

You can use PHI Mode at two levels:

1. **Global PHI mode** (all chats use local inference)
2. **Per-room PHI routing** (only selected rooms use local inference)

Use **global PHI mode** when your whole workflow must stay local.  
Use **per-room PHI routing** when you want a mix (for example: patient channel = local, general channel = cloud).

## Per-Room PHI Routing (TUI-first)

This is the easiest path for non-technical users.

1. Run `sciclaw app`.
2. Open the **Routing** tab.
3. Select the room mapping you want to change.
4. Press **`m`** (**AI Mode**).
5. Choose:
   - Mode: `phi`
   - Backend: `ollama`
   - Model: keep suggested default (or choose a different Qwen 3.5 size)
6. Confirm, then press **`R`** to apply live.

That room now uses local PHI inference, while other rooms can keep using default/cloud mode.

### Return a Room to Default/Cloud

From the same **Routing** tab:

1. Select the room
2. Press **`m`**
3. Set mode to `default` (or `cloud`)
4. Press **`R`** to apply

### Optional CLI Equivalent

Only needed for automation/scripts:

```bash
# Force one room to local PHI
sciclaw routing set-runtime \
  --channel discord \
  --chat-id 1467333670787711140 \
  --mode phi \
  --local-backend ollama \
  --local-model qwen3.5:4b \
  --local-preset balanced

# Return that room to inherited default behavior
sciclaw routing set-runtime \
  --channel discord \
  --chat-id 1467333670787711140 \
  --mode default

# Validate and reload routing after scripted changes
sciclaw routing validate
sciclaw routing reload

# Explain why a message did or did not route
sciclaw routing explain \
  --channel discord \
  --chat-id 1467333670787711140 \
  --sender 8535331528 \
  --mention
```

## CLI Commands

### `sciclaw modes status`

Show the current mode, backend, model, and hardware summary.

### `sciclaw modes set cloud`

Switch back to cloud mode. Your PHI configuration is preserved for next time.

### `sciclaw modes set vm`

Switch to VM mode (for users running gateway/runtime in a managed VM instead of host or PHI local mode).

### `sciclaw modes set phi`

Switch to PHI mode. If PHI mode has not been configured yet, this runs the setup flow automatically.

### `sciclaw modes phi-setup`

Interactive setup flow:

1. Detect hardware (OS, CPU architecture, RAM, GPU vendor, VRAM).
2. Match a hardware profile.
3. Select the default model for your profile and preset.
4. Pull the model via Ollama.
5. Run a warmup inference to verify the model loads correctly.
6. Persist configuration and activate PHI mode.

### `sciclaw modes phi-status`

Show detailed backend health and model readiness, including whether Ollama is running, which model is loaded, and current resource usage.

## Hardware Profiles

The setup command matches your hardware to one of these profiles and selects a default model using the **balanced** preset:

| Profile | OS | GPU | RAM / VRAM | Default Model (balanced) |
|---|---|---|---|---|
| Apple Silicon 16GB+ | macOS arm64 | Apple | 16GB+ RAM | qwen3.5:4b |
| NVIDIA 12GB+ | Linux / Windows | NVIDIA | 12GB+ VRAM | qwen3.5:4b |
| NVIDIA 6-11GB | Linux / Windows | NVIDIA | 6-11GB VRAM | qwen3.5:4b |
| CPU-only 16GB+ | Any | None | 16GB+ RAM | qwen3.5:4b |
| CPU-only <16GB | Any | None | 6-15GB RAM | qwen3.5:2b |

## Presets

Presets control the trade-off between speed and output quality:

| Preset | Model | Notes |
|---|---|---|
| speed | qwen3.5:2b | Fastest responses, lower quality |
| balanced | qwen3.5:4b | Default. Good balance of speed and quality |
| quality | qwen3.5:9b | Best output quality, requires more RAM/VRAM |

The **balanced** preset is used by default. You can change the preset during setup or by editing the config directly.

## Configuration

After setup, the following fields are written to your sciclaw config file:

```json
{
  "agents": {
    "defaults": {
      "mode": "phi",
      "local_backend": "ollama",
      "local_model": "qwen3.5:4b",
      "local_preset": "balanced"
    }
  }
}
```

These values can also be set via environment variables:

| Config Field | Environment Variable |
|---|---|
| `mode` | `PICOCLAW_AGENTS_DEFAULTS_MODE` |
| `local_backend` | `PICOCLAW_AGENTS_DEFAULTS_LOCAL_BACKEND` |
| `local_model` | `PICOCLAW_AGENTS_DEFAULTS_LOCAL_MODEL` |
| `local_preset` | `PICOCLAW_AGENTS_DEFAULTS_LOCAL_PRESET` |

Environment variables take precedence over config file values.

## Switching Between Modes

Switching back to cloud mode is instant:

```bash
sciclaw modes set cloud
```

Your PHI configuration (backend, model, preset) is preserved. When you switch back to PHI mode later, there is no need to re-run setup or re-pull the model:

```bash
sciclaw modes set phi
```

### Global vs Per-Room: Quick Decision

| If you want... | Use |
|---|---|
| Everything local on this machine | `modes set phi` (global PHI mode) |
| Only one or two channels local | Routing tab `AI Mode` = `phi` for those channels |
| Most channels cloud, occasional local/private channel | Keep global `cloud`, route only private channels to `phi` |

## Troubleshooting

### "ollama is not installed"

Install Ollama from <https://ollama.com>. After installing, run `sciclaw modes phi-setup` again.

### "ollama is installed but not running"

Start the Ollama service:

- **macOS:** Open the Ollama application from your Applications folder.
- **Linux:** Run `systemctl start ollama` (or `ollama serve` if not using systemd).

### Room set to PHI but still not responding

Check three things in order:

1. `sciclaw modes phi-status` shows Ollama healthy and model ready.
2. In the Routing tab, the room shows **AI mode: phi** and valid local model/backend.
3. Apply routing changes with **`R`** after editing runtime settings.

### Slow first response

The first inference after starting Ollama (or after the model has been unloaded from memory) requires loading the model weights. This can take 10-30 seconds depending on model size and hardware. Subsequent responses are significantly faster.

### Tool calling quality

Qwen 3.5 models support tool calling, but quality may vary compared to cloud models. If you find tool use unreliable for a particular task, switch to cloud mode for that session:

```bash
sciclaw modes set cloud
```

## MLX Support

MLX backend support for Apple Silicon is planned. When implemented, MLX will be the preferred backend on macOS ARM64 systems, offering better performance through Apple's native ML framework. For now, Ollama is used on all platforms.
