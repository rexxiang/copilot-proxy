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

### 3) Configure Claude Code

Point Claude Code to the local proxy by setting:

```bash
export ANTHROPIC_BASE_URL='http://127.0.0.1:4000'
```

### 4) Configure messages init-sequence agent detection (optional)

By default, `messages_init_seq_agent` is `false`.  
When enabled, `/v1/messages` requests where all message roles are `user` and message count is `>= 2` are treated as `isAgent=true` (affects `X-Initiator` and monitor classification).

`~/.config/copilot-proxy/settings.json`:

```json
{
  "messages_init_seq_agent": false,
  "reasoning_policies": {
    "gpt-5-mini@responses": "low",
    "grok-code-fast-1@chat": "none"
  }
}
```

`reasoning_policies` values must be `none|low|medium|high`.  
For `/v1/messages` requests, reasoning effort is only sent upstream when the selected model reports supported levels via `/models` capability metadata.
Map-style settings (for example `required_headers` and `reasoning_policies`) are storage fields; TUI edits should be done through dedicated array-object shadow fields.

## Account Management

- `copilot-proxy auth login` - authenticate via GitHub device flow
- `copilot-proxy auth ls` - list and manage accounts
- `copilot-proxy auth rm <user>` - remove an account
