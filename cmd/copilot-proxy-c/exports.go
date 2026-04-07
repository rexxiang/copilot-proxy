//go:build cgo
// +build cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef int (*CopilotProxyHostDispatchFn)(const char *request_json, char **response_json_out, char **error_out, void *user_data);
typedef void (*CopilotProxyEventCallback)(const char *event_json, void *user_data);

typedef struct {
	uint32_t version;
	uint64_t capabilities;
	CopilotProxyHostDispatchFn dispatch;
	void *user_data;
} CopilotProxyHostBridge;

static inline int copilot_proxy_call_host_dispatch(CopilotProxyHostDispatchFn cb, const char *request_json, char **response_json_out, char **error_out, void *user_data) {
	if (cb == NULL) {
		return 1;
	}
	return cb(request_json, response_json_out, error_out, user_data);
}

static inline void copilot_proxy_call_event(CopilotProxyEventCallback cb, const char *event_json, void *user_data) {
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
	"fmt"
	"unsafe"
)

func main() {}

//export CopilotProxy_Execute
func CopilotProxy_Execute(
	requestJSON *C.char,
	hostBridge *C.CopilotProxyHostBridge,
	eventCb C.CopilotProxyEventCallback,
	eventUserData unsafe.Pointer,
	finalErrorOut **C.char,
) C.int {
	if requestJSON == nil {
		setFinalError(finalErrorOut, errors.New("request JSON is required"))
		return C.int(1)
	}

	bridge := makeHostBridge(hostBridge)
	deps := buildExecuteDeps(bridge)
	opts := executeOptions{
		EventCallback: makeEventCallback(eventCb, eventUserData),
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

func makeHostBridge(bridge *C.CopilotProxyHostBridge) hostBridge {
	if bridge == nil {
		return hostBridge{}
	}
	return hostBridge{
		Version:      uint32(bridge.version),
		Capabilities: uint64(bridge.capabilities),
		Dispatch:     makeHostDispatchFunc(bridge.dispatch, bridge.user_data),
	}
}

func makeHostDispatchFunc(cb C.CopilotProxyHostDispatchFn, userData unsafe.Pointer) hostDispatch {
	if cb == nil {
		return nil
	}
	return func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error) {
		select {
		case <-ctx.Done():
			return hostDispatchResponse{}, ctx.Err()
		default:
		}

		payload, err := json.Marshal(request)
		if err != nil {
			return hostDispatchResponse{}, fmt.Errorf("marshal host dispatch request: %w", err)
		}
		requestC := C.CString(string(payload))
		defer C.free(unsafe.Pointer(requestC))

		var responseC *C.char
		var errC *C.char
		status := C.copilot_proxy_call_host_dispatch(cb, requestC, &responseC, &errC, userData)
		defer releaseCStringPair(responseC, errC)

		if status != 0 {
			if errC != nil {
				return hostDispatchResponse{}, errors.New(C.GoString(errC))
			}
			return hostDispatchResponse{}, errors.New("host dispatch reported failure")
		}
		if responseC == nil {
			return hostDispatchResponse{}, errors.New("host dispatch returned nil response")
		}

		var response hostDispatchResponse
		if err := json.Unmarshal([]byte(C.GoString(responseC)), &response); err != nil {
			return hostDispatchResponse{}, fmt.Errorf("decode host dispatch response: %w", err)
		}
		return response, nil
	}
}

func makeEventCallback(cb C.CopilotProxyEventCallback, userData unsafe.Pointer) eventCallback {
	if cb == nil {
		return nil
	}
	return func(event eventEnvelope) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		eventC := C.CString(string(payload))
		defer C.free(unsafe.Pointer(eventC))
		C.copilot_proxy_call_event(cb, eventC, userData)
	}
}

func releaseCStringPair(primary, secondary *C.char) {
	if primary != nil {
		C.free(unsafe.Pointer(primary))
	}
	if secondary != nil {
		C.free(unsafe.Pointer(secondary))
	}
}
