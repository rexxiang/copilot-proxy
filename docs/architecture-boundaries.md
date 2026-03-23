# Architecture Dependency Boundaries

This repository includes an executable architecture boundary check at:

- `internal/architecture/deps_test.go`

Run it with:

```bash
mise x -- test:architecture
```

Current rules:

1. `internal/*` packages must not import `cmd/*`.
2. `internal/runtime/config/*` must not import `internal/runtime/server/*` or `internal/runtime/api/*`.
3. `internal/runtime/*` must not import `cmd/copilot-proxy/app/*`.
4. `internal/runtime/api/*` must not import `internal/middleware/*`.
5. `internal/runtime/endpoint/*` must not import `internal/middleware/*`.
6. `internal/runtime/request/*` must not import `internal/middleware/*`.
7. `internal/runtime/protocol/*` must not import:
`internal/runtime/config/*`, `internal/runtime/endpoint/*`, `internal/runtime/request/*`, `internal/middleware/*`.
