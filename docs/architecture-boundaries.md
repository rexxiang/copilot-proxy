# Architecture Dependency Boundaries

This repository includes an executable architecture boundary check at:

- `internal/architecture/deps_test.go`

Run it with:

```bash
mise x -- test:architecture
```

Current baseline rules:

1. `internal/*` packages must not import `cmd/*`.
2. `internal/runtime/config/*` must not import `internal/runtime/server/*` or `internal/runtime/api/*`.
3. `internal/runtime/*` must not import `cmd/copilot-proxy/app/*`.

These rules are intentionally minimal as a baseline and will be tightened in follow-up refactor stages.
