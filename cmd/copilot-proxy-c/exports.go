//go:build cgo
// +build cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef void (*CopilotProxyCallback)(const char *payload_json, const char *error_message, uint64_t invocation_id, void *user_data);

enum {
    COPILOT_PROXY_CORE_STATUS_STOPPED = 0,
    COPILOT_PROXY_CORE_STATUS_RUNNING = 1,
};
static inline void copilot_proxy_callback_call(CopilotProxyCallback cb, const char *payload_json, const char *error_message, uint64_t invocation_id, void *user_data) {
	if (cb != NULL) {
		cb(payload_json, error_message, invocation_id, user_data);
	}
}
*/
import "C"

import (
	"unsafe"

	"copilot-proxy/internal/core"
)

func main() {}

func fromHandle(handle unsafe.Pointer) *copilotProxyCore {
	if handle == nil {
		return nil
	}
	return (*copilotProxyCore)(handle)
}

//export CopilotProxyCore_Create
func CopilotProxyCore_Create() unsafe.Pointer {
	return unsafe.Pointer(newCopilotProxyCore())
}

//export CopilotProxyCore_Destroy
func CopilotProxyCore_Destroy(handle unsafe.Pointer) {
	proxy := fromHandle(handle)
	if proxy == nil {
		return
	}
	proxy.Destroy()
}

//export CopilotProxyCore_Start
func CopilotProxyCore_Start(handle unsafe.Pointer) C.int {
	proxy := fromHandle(handle)
	if proxy == nil {
		return 1
	}
	if err := proxy.Start(); err != nil {
		return 1
	}
	return 0
}

//export CopilotProxyCore_Stop
func CopilotProxyCore_Stop(handle unsafe.Pointer) C.int {
	proxy := fromHandle(handle)
	if proxy == nil {
		return 1
	}
	if err := proxy.Stop(); err != nil {
		return 1
	}
	return 0
}

//export CopilotProxyCore_Status
func CopilotProxyCore_Status(handle unsafe.Pointer) C.int {
	proxy := fromHandle(handle)
	if proxy == nil {
		return C.COPILOT_PROXY_CORE_STATUS_STOPPED
	}
	if proxy.Status() == core.StateRunning {
		return C.COPILOT_PROXY_CORE_STATUS_RUNNING
	}
	return C.COPILOT_PROXY_CORE_STATUS_STOPPED
}

//export CopilotProxyCore_Invoke
func CopilotProxyCore_Invoke(handle unsafe.Pointer, payload *C.char) C.int {
	proxy := fromHandle(handle)
	if proxy == nil {
		return 1
	}
	if payload == nil {
		return 1
	}
	goPayload := C.GoString(payload)
	if err := proxy.Invoke(goPayload); err != nil {
		return 1
	}
	return 0
}

func buildCallback(cb C.CopilotProxyCallback, userData unsafe.Pointer) func(string, error, uint64) {
	if cb == nil {
		return nil
	}
	return func(payload string, err error, id uint64) {
		var cPayload *C.char
		if payload != "" {
			cPayload = C.CString(payload)
			defer C.free(unsafe.Pointer(cPayload))
		}
		var cErr *C.char
		if err != nil {
			cErr = C.CString(err.Error())
			defer C.free(unsafe.Pointer(cErr))
		}
		C.copilot_proxy_callback_call(cb, cPayload, cErr, C.uint64_t(id), userData)
	}
}

//export CopilotProxyCore_SetCallback
func CopilotProxyCore_SetCallback(handle unsafe.Pointer, cb C.CopilotProxyCallback, userData unsafe.Pointer) {
	proxy := fromHandle(handle)
	if proxy == nil {
		return
	}
	proxy.SetCallback(buildCallback(cb, userData))
}
