//go:build cgo
// +build cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef int (*CopilotProxyResolveTokenFn)(const char *account_ref, char **token_out, char **error_out, void *user_data);
typedef int (*CopilotProxyResolveModelFn)(const char *model_id, char **model_json_out, char **error_out, void *user_data);
typedef void (*CopilotProxyResultCallback)(int status_code, const char *headers_json, const uint8_t *body, size_t body_len, const char *error_message, void *user_data);
typedef void (*CopilotProxyTelemetryCallback)(const char *event_json, void *user_data);

static inline int copilot_proxy_call_resolve_token(CopilotProxyResolveTokenFn cb, const char *account_ref, char **token_out, char **error_out, void *user_data) {
	if (cb == NULL) {
		return 1;
	}
	return cb(account_ref, token_out, error_out, user_data);
}

static inline int copilot_proxy_call_resolve_model(CopilotProxyResolveModelFn cb, const char *model_id, char **model_json_out, char **error_out, void *user_data) {
	if (cb == NULL) {
		return 1;
	}
	return cb(model_id, model_json_out, error_out, user_data);
}

static inline void copilot_proxy_call_result(CopilotProxyResultCallback cb, int status_code, const char *headers_json, const uint8_t *body, size_t body_len, const char *error_message, void *user_data) {
	if (cb != NULL) {
		cb(status_code, headers_json, body, body_len, error_message, user_data);
	}
}

static inline void copilot_proxy_call_telemetry(CopilotProxyTelemetryCallback cb, const char *event_json, void *user_data) {
	if (cb != NULL) {
		cb(event_json, user_data);
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"unsafe"
)

func main() {}

//export CopilotProxy_Execute
func CopilotProxy_Execute(
	requestJSON *C.char,
	resolveToken C.CopilotProxyResolveTokenFn,
	resolveModel C.CopilotProxyResolveModelFn,
	resultCb C.CopilotProxyResultCallback,
	telemetryCb C.CopilotProxyTelemetryCallback,
	userData unsafe.Pointer,
	finalErrorOut **C.char,
) C.int {
	if requestJSON == nil {
		setFinalError(finalErrorOut, errors.New("request JSON is required"))
		return C.int(1)
	}

	deps := executeDeps{
		ResolveToken: makeResolveTokenFunc(resolveToken, userData),
		ResolveModel: makeResolveModelFunc(resolveModel, userData),
	}
	opts := executeOptions{
		ResultCallback:    makeResultCallback(resultCb, userData),
		TelemetryCallback: makeTelemetryCallback(telemetryCb, userData),
	}

	if err := executeRequest(context.Background(), C.GoString(requestJSON), deps, opts); err != nil {
		setFinalError(finalErrorOut, err)
		return C.int(1)
	}
	setFinalError(finalErrorOut, nil)
	return C.int(0)
}

//export CopilotProxyAuth_RequestCode
func CopilotProxyAuth_RequestCode(challengeOut **C.char, errorOut **C.char) C.int {
	if challengeOut == nil {
		setFinalError(errorOut, errors.New("challengeOut is required"))
		return C.int(1)
	}
	payload, err := requestCodeJSON(context.Background())
	if err != nil {
		setFinalError(errorOut, err)
		return C.int(1)
	}
	*challengeOut = C.CString(payload)
	setFinalError(errorOut, nil)
	return C.int(0)
}

//export CopilotProxyAuth_PollToken
func CopilotProxyAuth_PollToken(devicePayload *C.char, tokenOut **C.char, errorOut **C.char) C.int {
	if devicePayload == nil {
		setFinalError(errorOut, errors.New("device payload is required"))
		return C.int(1)
	}
	if tokenOut == nil {
		setFinalError(errorOut, errors.New("tokenOut is required"))
		return C.int(1)
	}
	result, err := pollTokenJSON(context.Background(), C.GoString(devicePayload))
	if err != nil {
		setFinalError(errorOut, err)
		return C.int(1)
	}
	*tokenOut = C.CString(result)
	setFinalError(errorOut, nil)
	return C.int(0)
}

//export CopilotProxyUser_FetchInfo
func CopilotProxyUser_FetchInfo(token *C.char, infoOut **C.char, errorOut **C.char) C.int {
	if token == nil {
		setFinalError(errorOut, errors.New("token is required"))
		return C.int(1)
	}
	if infoOut == nil {
		setFinalError(errorOut, errors.New("infoOut is required"))
		return C.int(1)
	}
	result, err := fetchUserInfoJSON(context.Background(), C.GoString(token))
	if err != nil {
		setFinalError(errorOut, err)
		return C.int(1)
	}
	*infoOut = C.CString(result)
	setFinalError(errorOut, nil)
	return C.int(0)
}

//export CopilotProxyModels_Fetch
func CopilotProxyModels_Fetch(token *C.char, modelsOut **C.char, errorOut **C.char) C.int {
	if token == nil {
		setFinalError(errorOut, errors.New("token is required"))
		return C.int(1)
	}
	if modelsOut == nil {
		setFinalError(errorOut, errors.New("modelsOut is required"))
		return C.int(1)
	}
	result, err := fetchModelsJSON(context.Background(), C.GoString(token))
	if err != nil {
		setFinalError(errorOut, err)
		return C.int(1)
	}
	*modelsOut = C.CString(result)
	setFinalError(errorOut, nil)
	return C.int(0)
}

//export CopilotProxy_FreeCString
func CopilotProxy_FreeCString(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}

func setFinalError(out **C.char, err error) {
	if out == nil {
		return
	}
	if err == nil {
		*out = nil
		return
	}
	*out = C.CString(err.Error())
}

func makeResolveTokenFunc(cb C.CopilotProxyResolveTokenFn, userData unsafe.Pointer) resolveToken {
	if cb == nil {
		return nil
	}
	return func(ctx context.Context, accountRef string) (string, error) {
		cAccount := C.CString(accountRef)
		defer C.free(unsafe.Pointer(cAccount))

		var tokenC *C.char
		var errC *C.char
		status := C.copilot_proxy_call_resolve_token(cb, cAccount, &tokenC, &errC, userData)
		defer releaseResolveStrings(tokenC, errC)
		if status != 0 {
			if errC != nil {
				return "", errors.New(C.GoString(errC))
			}
			return "", errors.New("token resolver reported failure")
		}
		if tokenC == nil {
			return "", errors.New("token resolver returned nil token")
		}
		return C.GoString(tokenC), nil
	}
}

func makeResolveModelFunc(cb C.CopilotProxyResolveModelFn, userData unsafe.Pointer) resolveModel {
	if cb == nil {
		return nil
	}
	return func(ctx context.Context, modelID string) (modelInfo, error) {
		cModelID := C.CString(modelID)
		defer C.free(unsafe.Pointer(cModelID))

		var payloadC *C.char
		var errC *C.char
		status := C.copilot_proxy_call_resolve_model(cb, cModelID, &payloadC, &errC, userData)
		defer releaseResolveStrings(payloadC, errC)
		if status != 0 {
			if errC != nil {
				return modelInfo{}, errors.New(C.GoString(errC))
			}
			return modelInfo{}, errors.New("model resolver reported failure")
		}
		if payloadC == nil {
			return modelInfo{}, errors.New("model resolver returned nil payload")
		}

		var info modelInfo
		if err := json.Unmarshal([]byte(C.GoString(payloadC)), &info); err != nil {
			return modelInfo{}, err
		}
		return info, nil
	}
}

func makeResultCallback(cb C.CopilotProxyResultCallback, userData unsafe.Pointer) resultCallback {
	if cb == nil {
		return func(status int, headers map[string]string, body []byte, errMsg string) {}
	}
	return func(status int, headers map[string]string, body []byte, errMsg string) {
		var headersJSON *C.char
		if len(headers) > 0 {
			if payload, err := json.Marshal(headers); err == nil {
				headersJSON = C.CString(string(payload))
				defer C.free(unsafe.Pointer(headersJSON))
			}
		}

		var bodyPtr *C.uint8_t
		if len(body) > 0 {
			ptr := C.CBytes(body)
			bodyPtr = (*C.uint8_t)(ptr)
			defer C.free(ptr)
		}

		var errC *C.char
		if errMsg != "" {
			errC = C.CString(errMsg)
			defer C.free(unsafe.Pointer(errC))
		}

		C.copilot_proxy_call_result(cb, C.int(status), headersJSON, bodyPtr, C.size_t(len(body)), errC, userData)
	}
}

func makeTelemetryCallback(cb C.CopilotProxyTelemetryCallback, userData unsafe.Pointer) telemetryCallback {
	if cb == nil {
		return nil
	}
	return func(event map[string]any) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		eventC := C.CString(string(payload))
		defer C.free(unsafe.Pointer(eventC))
		C.copilot_proxy_call_telemetry(cb, eventC, userData)
	}
}

func releaseResolveStrings(primary, secondary *C.char) {
	if primary != nil {
		C.free(unsafe.Pointer(primary))
	}
	if secondary != nil {
		C.free(unsafe.Pointer(secondary))
	}
}
