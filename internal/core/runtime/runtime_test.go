package runtime

import (
	"context"
	"testing"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/models"
)

type testLoader struct{}

func (testLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return []models.ModelInfo{{ID: "test", Name: "test"}}, nil
}

type testObservabilitySink struct{}

func (testObservabilitySink) RecordStart(_ *core.RequestRecord)                            {}
func (testObservabilitySink) RecordFirstResponse(string, int, time.Duration, string, bool) {}
func (testObservabilitySink) RecordComplete(string, int, time.Duration, string)            {}
func (testObservabilitySink) AddEvent(core.Event)                                          {}
func (testObservabilitySink) Snapshot() core.Snapshot                                      { return core.Snapshot{} }

func TestRuntimeStoresProvidedObservabilitySink(t *testing.T) {
	ctx := context.Background()
	sink := &testObservabilitySink{}
	deps := RuntimeDeps{
		SettingsFunc: func() (config.Settings, error) {
			settings := config.DefaultSettings()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (config.AuthConfig, error) {
			return config.AuthConfig{
				Default:  "user",
				Accounts: []config.Account{{User: "user", GhToken: "token"}},
			}, nil
		},
		Observability: sink,
		ModelLoader:   testLoader{},
	}

	rt, err := NewRuntimeWithContext(ctx, deps)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	if rt.Observability != sink {
		t.Fatalf("expected runtime to store sink, got %T", rt.Observability)
	}
}
