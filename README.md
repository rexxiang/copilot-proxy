# copilot-proxy

`copilot-proxy` is a local GitHub Copilot API proxy. It exposes OpenAI-style and Anthropic-style local endpoints, then forwards requests to `https://api.githubcopilot.com` with auth and header adaptation.

## Features

- OpenAI-compatible local endpoints:
  - `POST /v1/chat/completions`
  - `POST /v1/responses`
- Anthropic-compatible local endpoint:
  - `POST /v1/messages`
- Models endpoint:
  - `GET /copilot/models`
- Path mapping:
  - `/v1/chat/completions` -> `/chat/completions`
  - `/v1/responses` -> `/responses`
  - `/copilot/models` -> `/models`
  - `/v1/messages` can route to `/v1/messages`, `/chat/completions`, or `/responses` based on model endpoint support

Default listen address: `127.0.0.1:4000`.

## Core / Monitor boundary

- `internal/core` (observability, stats, account, models, etc.) owns the runtime instrumentation, persistence, and DTO implementations that drive metrics and user telemetry.
- `internal/monitor` is a thin compatibility shim for the CLI/TUI: it re-exports `core.RequestRecord`, `core.Snapshot`, `models.ModelInfo`, and `monitor.UserInfo`, and it keeps the `/copilot_internal/user` helper so the UI can surface quota details without importing `core`.
- The CLI/TUI consumes observability data through `core/stats.Service.MonitorSnapshot()` while persistence and sink wiring remain strictly inside `internal/core/observability`, keeping instrumentation logic decoupled from UI adapters.

Keeping `internal/monitor` focused on surface-level DTOs and the user-info bridge ensures the UI layers stay decoupled from the actual metric collection.

## Install

### One-command install (latest release)

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | sh
```

Default install path is `~/.local/bin/copilot-proxy` (no `sudo` required).

Set a custom install directory with `INSTALL_DIR`:

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

If `~/.local/bin` is not in your `PATH`, add:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Build from source

- CLI only (no CGO): `mise x -- go build -o ./bin/copilot-proxy ./cmd/copilot-proxy`
- CLI and C ABI: `mise x -- build` (go build followed by `mise run build:c-shared`)
- C ABI only: `mise x -- build:c-shared`

The c-shared build emits `./bin/copilot-proxy-c.so` (or the platform-appropriate shared library) plus the generated header `./bin/copilot-proxy-c.h`.

## Usage

### 1) Authenticate a GitHub account

```bash
copilot-proxy auth login
```

This uses GitHub device flow and stores credentials in `~/.config/copilot-proxy/auth.json`.

### 2) Start the proxy

With TUI monitor (default when TTY is available):

```bash
copilot-proxy
```

### 3) Configure Claude Code

Point Claude Code to the local proxy by setting:

```bash
export ANTHROPIC_BASE_URL='http://127.0.0.1:4000'
```

### 4) Configure messages agent detection and rate limiting (optional)

`messages_agent_detection_request_mode` defaults to `true` for `/v1/messages`.
- `premium request=true`:
  - all `user` and `len(messages)>=2` -> `isAgent=true`
  - otherwise only the **last** message is checked (`role!="user"` or last content type suffix is `tool_use` / `tool_result`)
- `session=false`:
  - all `user` and `len(messages)>=2` -> `isAgent=true`
  - any historical `role!="user"` -> `isAgent=true`
  - any historical content type suffix `tool_result` -> `isAgent=true`

`~/.config/copilot-proxy/settings.json`:

```json
{
  "messages_agent_detection_request_mode": true,
  "rate_limit_seconds": 0,
  "claude_haiku_fallback_models": [
    "gpt-5-mini",
    "grok-code-fast-1"
  ],
  "reasoning_policies": {
    "gpt-5-mini@responses": "low",
    "grok-code-fast-1@chat": "none"
  }
}
```

`rate_limit_seconds` uses whole seconds and enforces a global cooldown between one proxied request finishing and the next starting. `0` disables rate limiting.

`claude_haiku_fallback_models` is an ordered list of explicit replacement models to try for `claude-haiku-*` requests. If none are available, the proxy automatically falls back to the highest available `claude-haiku-*` model.

`reasoning_policies` values must be `none|low|medium|high`.  
For `/v1/messages` requests, reasoning effort is only sent upstream when the selected model reports supported levels via `/models` capability metadata.
Map-style settings (for example `required_headers` and `reasoning_policies`) are storage fields; TUI edits should be done through dedicated array-object shadow fields.

## Account Management

- `copilot-proxy auth login` - authenticate via GitHub device flow
- `copilot-proxy auth ls` - list and manage accounts
- `copilot-proxy auth rm <user>` - remove an account

## C ABI

`cmd/copilot-proxy-c` boots the same `internal/core.Kernel` that powers the CLI/TUI runtime (including auth, config, models, and observability) and exposes it through a compact C ABI that exchanges `core.RequestInvocation`/`core.ResponsePayload` pairs over JSON. Building the shared workflow produces `./bin/copilot-proxy`, `./bin/copilot-proxy-c.so`, and the generated header `./bin/copilot-proxy-c.h`. Use `mise x -- build` for the combined CLI + shared library or `mise x -- build:c-shared` for the library alone.

### Entry points

```
void *CopilotProxyCore_Create(void);
void CopilotProxyCore_Destroy(void *core);
int CopilotProxyCore_Start(void *core);
int CopilotProxyCore_Stop(void *core);
int CopilotProxyCore_Status(void *core);
int CopilotProxyCore_Invoke(void *core, const char *request_json);
void CopilotProxyCore_SetCallback(void *core, CopilotProxyCallback cb, void *user_data);
```

`CopilotProxyCore_Status` returns either `COPILOT_PROXY_CORE_STATUS_STOPPED` or `COPILOT_PROXY_CORE_STATUS_RUNNING`. `CopilotProxyCallback` has the signature:

```
typedef void (*CopilotProxyCallback)(const char *payload_json, const char *error_message, uint64_t invocation_id, void *user_data);
```

`request_json` / `payload_json` are the JSON encodings of `core.RequestInvocation` and `core.ResponsePayload`, respectively. `core.RequestInvocation` contains `Method`, `Path`, optional `Header` map, and `Body` bytes (JSON/Go encodes `[]byte` as base64). `core.ResponsePayload` mirrors the HTTP response (`StatusCode`, `Headers`, `Body`).

### Lifecycle and threading

`CopilotProxyCore_Create` sets up the runtime dependencies (settings, auth, model catalog, observability) so the C ABI shares the same kernel instance that the CLI and TUI use. `CopilotProxyCore_Start` launches that kernel on a dedicated goroutine, waits until the runtime reports `StateRunning`, and then returns. `CopilotProxyCore_Stop` asks the kernel to shut down, waits for the goroutine to exit, and propagates any fatal errors; revisit `CopilotProxyCore_Status` to confirm whether the stop succeeded. Because CLI/TUI/C all build the same core, they observe the same configuration, telemetry, and model availability.

### Request queue and callback

`CopilotProxyCore_Invoke` enqueues a JSON request into a single-threaded serialization loop. A dedicated goroutine serializes the JSON, invokes `kernel.Invoke`, re-encodes the response, and finally runs the callback on that same goroutine so users can rely on a consistent thread context. `payload_json` carries the `core.ResponsePayload`, while `error_message` carries Go error text that occurs during decoding, validation, or kernel rejection. `Invoke` returns non-zero when the queue is full, the kernel is not running, or the core has been destroyed; the callback still fires with whatever diagnostics are available. Wrap every `Invoke` between `Start`/`Stop`, register your callback with `CopilotProxyCore_SetCallback`, and destroy the handle once you are finished.

## Testing

- Run the general Go test suite (CGO disabled by default) with `mise x -- test`.
- Verify the C ABI layer with `mise x -- test:cgo` (the same as `CGO_ENABLED=1 go test ./cmd/copilot-proxy-c`), ensuring the exported symbols link to the core kernel.
