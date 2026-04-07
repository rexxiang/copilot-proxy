# copilot-proxy

## What It Is

`copilot-proxy` is a local GitHub Copilot API proxy. It accepts OpenAI-style and Anthropic-style client requests on local endpoints, then forwards them to `https://api.githubcopilot.com` with authentication and header adaptation.

## Core Features

- OpenAI-compatible endpoints:
  - `POST /v1/chat/completions`
  - `POST /v1/responses`
- Anthropic-compatible endpoint:
  - `POST /v1/messages`
- Models endpoint:
  - `GET /copilot/models`
- Endpoint mapping:
  - `/v1/chat/completions` -> `/chat/completions`
  - `/v1/responses` -> `/responses`
  - `/copilot/models` -> `/models`
  - `/v1/messages` can route to `/v1/messages`, `/chat/completions`, or `/responses` depending on selected model endpoint support

Default listen address: `127.0.0.1:4000`.

## Install

### 1) Install latest release (recommended)

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | sh
```

Default install path is `~/.local/bin/copilot-proxy`.

Use a custom install directory:

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

If needed, add it to `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### 2) Optional: build from source

```bash
mise x -- go build -o ./bin/copilot-proxy ./cmd/copilot-proxy
```

## Usage Tutorial

### 1) Authenticate your GitHub account

```bash
copilot-proxy auth login
```

This uses GitHub device flow and stores credentials in `~/.config/copilot-proxy/auth.json`.

### 2) Start the proxy

Run with the TUI monitor (default when TTY is available):

```bash
copilot-proxy
```

Or run without TUI:

```bash
copilot-proxy --no-tui
```

### 3) Connect your client (example: Claude Code)

```bash
export ANTHROPIC_BASE_URL='http://127.0.0.1:4000'
```

### 4) Manage accounts

```bash
copilot-proxy auth ls
copilot-proxy auth rm <user>
```

## More Docs

- C API integration guide: [docs/c-api-integration.md](docs/c-api-integration.md)
