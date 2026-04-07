package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBuildExecuteDepsResolveToken(t *testing.T) {
	dispatch := func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error) {
		if request.Op != hostOpResolveToken {
			t.Fatalf("unexpected op: %q", request.Op)
		}
		payload, ok := request.Payload.(tokenResolveRequest)
		if !ok {
			t.Fatalf("unexpected payload type: %T", request.Payload)
		}
		if payload.AccountRef != "acct-1" {
			t.Fatalf("unexpected account ref: %q", payload.AccountRef)
		}
		responsePayload, _ := json.Marshal(tokenResolveResponse{Token: "token-123"})
		return hostDispatchResponse{
			Version: hostDispatchVersion,
			OK:      true,
			Payload: responsePayload,
		}, nil
	}

	deps := buildExecuteDeps(hostBridge{Dispatch: dispatch})
	token, err := deps.ResolveToken(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestBuildExecuteDepsResolveModel(t *testing.T) {
	dispatch := func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error) {
		if request.Op != hostOpResolveModel {
			t.Fatalf("unexpected op: %q", request.Op)
		}
		payload, ok := request.Payload.(modelResolveRequest)
		if !ok {
			t.Fatalf("unexpected payload type: %T", request.Payload)
		}
		if payload.ModelID != "gpt-4o" {
			t.Fatalf("unexpected model id: %q", payload.ModelID)
		}
		responsePayload, _ := json.Marshal(modelInfo{
			ID:                       "gpt-4o",
			Endpoints:                []string{"/chat/completions"},
			SupportedReasoningEffort: []string{"low"},
		})
		return hostDispatchResponse{
			Version: hostDispatchVersion,
			OK:      true,
			Payload: responsePayload,
		}, nil
	}

	deps := buildExecuteDeps(hostBridge{Dispatch: dispatch})
	model, err := deps.ResolveModel(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("resolve model: %v", err)
	}
	if model.ID != "gpt-4o" {
		t.Fatalf("unexpected model id: %q", model.ID)
	}
	if len(model.Endpoints) != 1 || model.Endpoints[0] != "/chat/completions" {
		t.Fatalf("unexpected endpoints: %#v", model.Endpoints)
	}
}

func TestBuildExecuteDepsStateSetNew(t *testing.T) {
	dispatch := func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error) {
		if request.Op != hostOpStateSetNew {
			t.Fatalf("unexpected op: %q", request.Op)
		}
		payload, ok := request.Payload.(stateSetNewRequest)
		if !ok {
			t.Fatalf("unexpected payload type: %T", request.Payload)
		}
		if payload.Namespace != sessionNamespace || payload.Key != "session-1" || payload.Value != sessionValue {
			t.Fatalf("unexpected state payload: %#v", payload)
		}
		responsePayload, _ := json.Marshal(stateSetNewResponse{Created: true})
		return hostDispatchResponse{
			Version: hostDispatchVersion,
			OK:      true,
			Payload: responsePayload,
		}, nil
	}

	deps := buildExecuteDeps(hostBridge{Dispatch: dispatch})
	created, err := deps.StateSetNew(context.Background(), sessionNamespace, "session-1", sessionValue)
	if err != nil {
		t.Fatalf("state set_new: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
}

func TestInvokeHostOperationRejectsNonOK(t *testing.T) {
	dispatch := func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error) {
		return hostDispatchResponse{
			Version: hostDispatchVersion,
			OK:      false,
			Code:    "NOT_FOUND",
			Error:   "missing value",
		}, nil
	}

	err := invokeHostOperation(context.Background(), dispatch, hostOpResolveToken, tokenResolveRequest{AccountRef: "acct-1"}, &tokenResolveResponse{})
	if err == nil {
		t.Fatalf("expected non-ok response to return error")
	}
}
