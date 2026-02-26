# Configuration Examples

This directory contains example configuration files for copilot-proxy.

## Files

- `settings.example.json` - Proxy server settings
- `auth.example.json` - GitHub authentication configuration

## Usage

Configuration files are stored in `~/.config/copilot-proxy/`:

```bash
# Copy example files to config directory
mkdir -p ~/.config/copilot-proxy
cp config/settings.example.json ~/.config/copilot-proxy/settings.json
cp config/auth.example.json ~/.config/copilot-proxy/auth.json
```

## Authentication

Use the built-in device flow to authenticate:

```bash
./bin/copilot-proxy auth login
```

This will automatically create and update `~/.config/copilot-proxy/auth.json`.

## Settings

| Field              | Default                         | Description                          |
|--------------------|---------------------------------|--------------------------------------|
| `listen_addr`      | `127.0.0.1:4000`                | Local proxy listen address           |
| `upstream_base`    | `https://api.githubcopilot.com` | Copilot API base URL                 |
| `required_headers` | (see defaults)                  | Headers to inject into requests      |
| `upstream_timeout` | `5m`                            | Proxy-applied timeout for upstream requests (`0` disables) |
| `max_retries`      | `3`                             | Max retry attempts on network errors |
| `retry_backoff`    | `1s`                            | Backoff duration between retries     |

Default upstream injected headers:

- `user-agent: copilot/0.0.400`
- `copilot-integration-id: copilot-developer-cli`

Notes:

- `editor-version` is no longer injected by default.
- GitHub OAuth access token (`gho_`) is used directly as upstream bearer token.
- `upstream_timeout` is enforced by upstream middleware via request context deadline.
