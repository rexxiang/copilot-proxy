# copilot-proxy

`copilot-proxy` 是一个 GitHub Copilot API 本地代理，提供与 OpenAI / Anthropic 风格接口兼容的转发能力。

## 一键安装（最新版）

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | sh
```

默认会安装到 `/usr/local/bin/copilot-proxy`。如果需要自定义目录，可在执行前设置 `INSTALL_DIR`：

```bash
curl -fsSL https://rexxiang.github.io/copilot-proxy/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
```
