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

## Install

### One-command install (latest release)

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | sh
```

Default install path is `/usr/local/bin/copilot-proxy`.

Set a custom install directory with `INSTALL_DIR`:

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

### Build from source

```bash
mise x -- go build -o ./bin/copilot-proxy ./cmd/copilot-proxy
```

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

Headless mode:

```bash
copilot-proxy --no-tui
```

### 3) Send requests to local endpoints

List available models:

```bash
curl -sS http://127.0.0.1:4000/copilot/models
```

Use a model ID returned by `/copilot/models`. For `/v1/messages`, choose a model that includes `/v1/messages` in `supported_endpoints`.

OpenAI Responses:

```bash
curl -sS http://127.0.0.1:4000/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "input": "Say hello from copilot-proxy."
  }'
```

OpenAI Chat Completions:

```bash
curl -sS http://127.0.0.1:4000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role":"user","content":"Hello"}]
  }'
```

Anthropic Messages-compatible request:

```bash
curl -sS http://127.0.0.1:4000/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "your-model-with-messages-support",
    "max_tokens": 128,
    "messages": [{"role":"user","content":"Hello"}]
  }'
```

## Account Management

- `copilot-proxy auth login` - authenticate via GitHub device flow
- `copilot-proxy auth ls` - list and manage accounts
- `copilot-proxy auth rm <user>` - remove an account

## Configuration Paths

- `~/.config/copilot-proxy/auth.json`
- `~/.config/copilot-proxy/settings.json`
- `~/.config/copilot-proxy/metrics.json`

See [config examples](config/README.md) for configuration details.
