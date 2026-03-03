# copilot-proxy

`copilot-proxy` is a local GitHub Copilot API proxy with OpenAI-style and Anthropic-style local endpoints.

## One-Command Install (Latest)

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | sh
```

By default, it installs to `/usr/local/bin/copilot-proxy`. To use a custom directory, set `INSTALL_DIR` before running:

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```

## Quick Usage

```bash
copilot-proxy auth login
copilot-proxy --no-tui
curl -sS http://127.0.0.1:4000/copilot/models
```

For full installation and usage documentation, see the repository README:

- https://github.com/rexxiang/copilot-proxy#readme
