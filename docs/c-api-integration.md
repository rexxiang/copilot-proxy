# C API Integration Guide

This guide covers the C ABI exposed by `cmd/copilot-proxy-c`, including host bridge callbacks, event callbacks, and the `state.set_new` session behavior.

## Overview

The C ABI is stateless from the library perspective:

- The library does not keep app-level account/session state.
- Host code owns state and exposes operations through `CopilotProxyHostDispatchFn`.
- Request lifecycle output is emitted via `CopilotProxyEventCallback`.

Build output:

- Shared library: `./bin/copilot-proxy-c.so` (platform equivalent on non-Linux)
- Generated header: `./bin/copilot-proxy-c.h`

## Entry Points

```c
typedef int (*CopilotProxyHostDispatchFn)(
  const char *request_json,
  char **response_json_out,
  char **error_out,
  void *user_data
);

typedef void (*CopilotProxyEventCallback)(
  const char *event_json,
  void *user_data
);

typedef struct {
  uint32_t version;
  uint64_t capabilities;
  CopilotProxyHostDispatchFn dispatch;
  void *user_data;
} CopilotProxyHostBridge;

int CopilotProxy_Execute(
  const char *request_json,
  const CopilotProxyHostBridge *host_bridge,
  CopilotProxyEventCallback on_event,
  void *event_user_data,
  char **final_error_out
);

int CopilotProxyAuth_RequestCode(char **challenge_out, char **error_out);
int CopilotProxyAuth_PollToken(const char *device_payload, char **token_out, char **error_out);
int CopilotProxyUser_FetchInfo(const char *token, char **info_out, char **error_out);
int CopilotProxyModels_Fetch(const char *token, char **models_out, char **error_out);
void CopilotProxy_FreeCString(char *ptr);
```

`request_json` for `CopilotProxy_Execute` is JSON for `types.RequestInvocation`:

- `Method`
- `Path`
- optional `Header`
- `Body` (base64 when serialized through standard Go JSON handling of `[]byte`)

## Host Dispatch Protocol

The host callback receives a JSON request and must return a JSON response.

Request envelope:

```json
{
  "version": 1,
  "op": "auth.resolve_token",
  "payload": {}
}
```

Response envelope:

```json
{
  "version": 1,
  "ok": true,
  "code": "OK",
  "payload": {}
}
```

If callback-level failure happens, return non-zero from callback and set `error_out`.

### Supported `op` values

- `auth.resolve_token`
  - request payload: `{ "account_ref": "..." }`
  - response payload: `{ "token": "..." }`
- `model.resolve`
  - request payload: `{ "model_id": "..." }`
  - response payload: `{ "id": "...", "endpoints": ["..."], "supported_reasoning_effort": ["low","medium","high"] }`
- `state.set_new`
  - request payload: `{ "namespace": "...", "key": "...", "value": "..." }`
  - response payload: `{ "created": true|false }`

`state.set_new` semantics:

- If key already exists in namespace, do not overwrite and return `created=false`.
- If key does not exist, write it and return `created=true`.

## Event Callback Protocol

`CopilotProxyEventCallback` receives JSON events:

- `kind="response_head"`: first response frame (`status_code`, `headers`, optional `body_base64`)
- `kind="response_chunk"`: subsequent stream chunk (`body_base64`)
- `kind="telemetry"`: lifecycle event (`start`, `first_byte`, `end`, `error`)
- `kind="fatal"`: callback-level fatal error payload

Example:

```json
{
  "version": 1,
  "kind": "response_head",
  "payload": {
    "status_code": 200,
    "headers": { "Content-Type": "application/json" },
    "body_base64": "eyJyZXN1bHQiOiJvayJ9"
  }
}
```

## Session Behavior (`X-Claude-Code-Session-Id`)

When request headers include `X-Claude-Code-Session-Id`, runtime uses:

- `op=state.set_new`
- `namespace="claude_session_seen"`
- `key=<session_id>`
- `value="1"`

Classification rule:

- `created=true` (first observed session id) => `IsAgent=false`
- `created=false` (session id already seen) => `IsAgent=true`

If `state.set_new` fails, runtime falls back to existing request-body based agent detection.

## Minimal Integration Skeleton

```c
static int my_host_dispatch(
  const char *request_json,
  char **response_json_out,
  char **error_out,
  void *user_data
) {
  // 1. parse request_json
  // 2. switch on "op"
  // 3. produce {"version":1,"ok":true,"payload":...}
  // 4. set *response_json_out (allocated C string)
  return 0;
}

static void my_event_callback(const char *event_json, void *user_data) {
  // parse event_json and handle response_head/response_chunk/telemetry/fatal
}

void run_once(const char *invoke_json) {
  CopilotProxyHostBridge bridge = {
    .version = 1,
    .capabilities = 0,
    .dispatch = my_host_dispatch,
    .user_data = NULL,
  };

  char *final_error = NULL;
  int rc = CopilotProxy_Execute(
    invoke_json,
    &bridge,
    my_event_callback,
    NULL,
    &final_error
  );

  if (rc != 0 && final_error != NULL) {
    // report error
  }
  CopilotProxy_FreeCString(final_error);
}
```

## Build & Verify

Build shared library:

```bash
mise run build:c-shared
```

Verify C API tests:

```bash
mise x -- env CGO_ENABLED=1 go test ./cmd/copilot-proxy-c
```
