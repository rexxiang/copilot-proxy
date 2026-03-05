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
internal/
  cli/                 command routing, server wiring, Bubble Tea monitor
  auth/                GitHub device flow + user lookup
  config/              auth/settings persistence and constants
  middleware/          shared interfaces + upstream middleware pipeline
  models/              model catalog loading/fetching
  monitor/             metrics collector, persistence, user/model API clients
  proxy/               reverse proxy and retry transport
  server/              HTTP server lifecycle
  token/               Copilot token cache + inflight deduplication
```

## Core Runtime Rules

- **Auth source of truth**: `~/.config/copilot-proxy/auth.json`, with `@default` as active account.
- **Runtime auth consistency**: account changes from TUI must update both disk and in-process auth store.
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
- Prefer dependency injection already used in `internal/cli/server.go` for testability.
- When changing auth, monitor, SSE/log behavior, add/adjust tests in the same area.
- Avoid broad refactors unless explicitly requested.
