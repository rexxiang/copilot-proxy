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

## Core boundary

- `internal/core` is split by capability domain (`execute`, `runtimeapi`, `observability`, `stats`, `model`, `account`).
- `internal/core/runtimeapi` is the stateless operation entry and type surface shared by server runtime and the C ABI.
  - operations: `Execute`, device-flow auth, user info, model fetch
  - shared types: request invocation, execute options/results, telemetry events, device-code payloads, user info, model DTOs
- `cmd/copilot-proxy/app` owns mutable app state (account selection, login session state, settings editing state, monitor state) and application composition.
- Model catalog state is caller-owned and injected explicitly; production runtime no longer falls back to a process-global default catalog.
- CLI/TUI consume observability snapshots through `core/stats.Service.MonitorSnapshot()`.
- Persistence and sink wiring stay inside `internal/core/observability`, while CLI/TUI adapters live under `cmd/copilot-proxy/app`.

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

`cmd/copilot-proxy-c` now exposes a **stateless** C ABI. The library does not keep runtime handles, queues, account state, login sessions, settings state, model-selection state, or observability aggregates. Callers provide token/model resolution callbacks and own all state. The exported execution path uses the same stateless `runtimeapi` flow as the server runtime, including model rewrite, endpoint selection, and `/v1/messages` protocol translation. Building the shared workflow produces `./bin/copilot-proxy`, `./bin/copilot-proxy-c.so`, and the generated header `./bin/copilot-proxy-c.h`. Use `mise x -- build` for the combined CLI + shared library or `mise x -- build:c-shared` for the library alone.

### Entry points

```
int CopilotProxy_Execute(
  const char *request_json,
  CopilotProxyResolveTokenFn resolve_token,
  CopilotProxyResolveModelFn resolve_model,
  CopilotProxyResultCallback on_result,
  CopilotProxyTelemetryCallback on_telemetry,
  void *user_data,
  char **final_error_out
);

int CopilotProxyAuth_RequestCode(char **challenge_out, char **error_out);
int CopilotProxyAuth_PollToken(const char *device_payload, char **token_out, char **error_out);
int CopilotProxyUser_FetchInfo(const char *token, char **info_out, char **error_out);
int CopilotProxyModels_Fetch(const char *token, char **models_out, char **error_out);
void CopilotProxy_FreeCString(char *ptr);
```

Callback contracts:

```
typedef int (*CopilotProxyResolveTokenFn)(const char *account_ref, char **token_out, char **error_out, void *user_data);
typedef int (*CopilotProxyResolveModelFn)(const char *model_id, char **model_json_out, char **error_out, void *user_data);
typedef void (*CopilotProxyResultCallback)(int status_code, const char *headers_json, const uint8_t *body, size_t body_len, const char *error_message, void *user_data);
typedef void (*CopilotProxyTelemetryCallback)(const char *event_json, void *user_data);
```

`request_json` is the JSON encoding of `core.RequestInvocation` (`Method`, `Path`, optional `Header`, and `Body` as base64).

### Execution semantics

- `CopilotProxy_Execute` is synchronous: it returns only after the request is fully finished.
- Non-stream responses trigger exactly one `CopilotProxyResultCallback`.
- Stream responses trigger one first callback with `status_code` + `headers_json`, then body-only callbacks (`status_code=0`, `headers_json=NULL`) for subsequent chunks.
- Telemetry emits raw lifecycle events (`start`, `first_byte`, `end`, `error`) through `CopilotProxyTelemetryCallback`. Aggregation is caller-owned.
- Any C string returned through out-params must be freed with `CopilotProxy_FreeCString`.

## Testing

- Run the general Go test suite (CGO disabled by default) with `mise x -- test`.
- Verify the C ABI layer with `mise x -- env CGO_ENABLED=1 go test ./cmd/copilot-proxy-c`, ensuring the stateless exports and callbacks compile and run.
