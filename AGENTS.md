# AGENTS.md

This file provides practical guidance for AI coding agents working in this repository.

## Build, Test, Run

```bash
# Build
mise x -- go build -o ./bin/copilot-proxy ./cmd/copilot-proxy

# Test
mise x -- go test ./...
mise x -- go test -v ./internal/proxy
mise x -- go test -run TestHandler ./...

# Run
./bin/copilot-proxy
./bin/copilot-proxy --no-tui
./bin/copilot-proxy auth login
./bin/copilot-proxy auth ls
./bin/copilot-proxy auth rm <user>
```

Environment: Go 1.24+, mise 2026.0.0+

## Project Overview

This project is a GitHub Copilot API proxy that exposes local AI API endpoints and forwards to `api.githubcopilot.com` with auth/header adaptation.

It supports:
- OpenAI-style `chat/completions`
- OpenAI-style `responses`
- Anthropic-style `messages`

## Exposed URLs

Default listen address: `http://127.0.0.1:4000`

### Local endpoints (client-facing)

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/messages` (Anthropic Messages-compatible surface)
- `GET /copilot/models`

### Upstream endpoints (forwarded)

- `/chat/completions`
- `/responses`
- `/v1/messages`
- `/models`

### Mapping behavior

- `/v1/chat/completions` → fixed to `/chat/completions`
- `/v1/responses` → fixed to `/responses`
- `/copilot/models` → `/models`
- `/v1/messages`:
  - prefers `/v1/messages` when model supports it
  - may route to `/chat/completions` or `/responses` based on selected model endpoints
  - request/response payloads (including SSE stream chunks) are transformed for protocol compatibility

## Directory Map

```text
cmd/copilot-proxy/     CLI entry
  app/                 command routing, server wiring, Bubble Tea monitor, app-owned state
cmd/copilot-proxy-c/   C ABI entry
internal/
  runtime/
    api/               stateless shared execution engine for server runtime and C ABI
    server/            HTTP runtime wiring and handler composition
    config/            auth persistence + runtime settings + protocol constants
    identity/
      oauth/           GitHub device flow + user lookup
      account/         account DTOs + user info fetch helpers
    model/             model catalog loading/fetching + refresh service
    endpoint/          upstream endpoint selection and protocol transforms
    execute/           stateless request execution primitive
    observability/     metrics sinks and persistence
    stats/             monitor snapshot service
    types/             shared DTOs and runtime state/event contracts
  middleware/          shared HTTP interfaces + upstream helpers
  proxy/               reverse proxy and retry transport
  server/              HTTP server lifecycle
  token/               Copilot token cache + inflight deduplication
```

## Core Runtime Rules

- **Auth source of truth**: `~/.config/copilot-proxy/auth.json`, with `@default` as active account.
- **CLI/TUI state ownership**: login sessions, active-account edits, settings editing state, and model catalog state live in the app layer and persist through config callbacks or injected state holders.
- **Runtime statelessness**: `internal/runtime/api` and the C ABI resolve auth/settings/models through callbacks/providers instead of holding mutable app state.
- **Monitor views**: Stats / Models / Logs, refreshed by periodic tick.
- **Stats account modal** (`a`, Stats view only):
  - switch active account
  - add account via device-flow (`Add Account`)
  - cancel in-progress authorization with `Esc`
  - adding/updating account should not replace existing default unless it is the first account
- **SSE metrics lifecycle**:
  - track active request start
  - record first response timing for streams
  - finalize on EOF/cancel/close (client-canceled uses 499)
- **Logs view semantics**:
  - `Timestamp`: request start time
  - `Duration`: total request duration (or stream first-response duration for SSE)
  - `Stream`: SSE stream-phase duration

## Persistence Paths

- `~/.config/copilot-proxy/auth.json`
- `~/.config/copilot-proxy/settings.json`
- `~/.config/copilot-proxy/log/error/YYYYMMDD.md`
- `~/.config/copilot-proxy/log/debug/YYYYMMDD.md`

## Working Guidelines

- Keep changes focused and minimal.
- Follow existing middleware/TUI patterns before introducing new structure.
- Prefer dependency injection already used in `cmd/copilot-proxy/app/server.go` for testability.
- When changing auth, monitor, SSE/log behavior, add/adjust tests in the same area.
- Avoid broad refactors unless explicitly requested.
