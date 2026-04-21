package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"github.com/tidwall/gjson"
)

type chatCaptureExecutor struct {
	payload []byte
	calls   int
}

func (e *chatCaptureExecutor) Identifier() string { return "test-provider" }

func (e *chatCaptureExecutor) Execute(_ context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.payload = append([]byte(nil), req.Payload...)
	return coreexecutor.Response{Payload: []byte(`{"id":"resp-1","object":"chat.completion","choices":[]}`)}, nil
}

func (e *chatCaptureExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("not implemented")
}

func (e *chatCaptureExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *chatCaptureExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("not implemented")
}

func (e *chatCaptureExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func TestChatCompletionsTreatsResponsesPayloadAsChatWithoutDroppingFastTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executor := &chatCaptureExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth-chat", Provider: executor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIAPIHandler(base)
	router := gin.New()
	router.POST("/v1/chat/completions", h.ChatCompletions)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"test-model",
		"service_tier":"fast",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if got := gjson.GetBytes(executor.payload, "service_tier").String(); got != "fast" {
		t.Fatalf("service_tier = %q, want %q: %s", got, "fast", string(executor.payload))
	}
	if !gjson.GetBytes(executor.payload, "messages").Exists() {
		t.Fatalf("messages field missing after chat normalization: %s", string(executor.payload))
	}
}

func TestOpenAIModelsIncludesAdditionalSpeedTiersWhenPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	modelRegistry := registry.GetGlobalRegistry()
	authID := "auth-models-fast-tier"
	provider := "openai"

	modelRegistry.RegisterClient(authID, provider, []*registry.ModelInfo{{
		ID:                   "gpt-5-codex",
		Object:               "model",
		Created:              1234567890,
		OwnedBy:              "openai",
		AdditionalSpeedTiers: []string{"fast"},
	}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(authID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h := NewOpenAIAPIHandler(base)
	router := gin.New()
	router.GET("/v1/models", h.OpenAIModels)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	var body struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Object != "list" {
		t.Fatalf("object = %q, want %q", body.Object, "list")
	}

	var got map[string]interface{}
	for _, model := range body.Data {
		if model["id"] == "gpt-5-codex" {
			got = model
			break
		}
	}
	if got == nil {
		t.Fatalf("registered model missing from response: %s", resp.Body.String())
	}
	if got["object"] != "model" {
		t.Fatalf("object = %#v, want %q", got["object"], "model")
	}
	if got["created"] != float64(1234567890) {
		t.Fatalf("created = %#v, want %d", got["created"], 1234567890)
	}
	if got["owned_by"] != "openai" {
		t.Fatalf("owned_by = %#v, want %q", got["owned_by"], "openai")
	}

	tiers, ok := got["additional_speed_tiers"].([]interface{})
	if !ok {
		t.Fatalf("additional_speed_tiers = %#v, want JSON array", got["additional_speed_tiers"])
	}
	if len(tiers) != 1 || tiers[0] != "fast" {
		t.Fatalf("additional_speed_tiers = %#v, want [fast]", tiers)
	}
}

func TestShouldTreatAsResponsesFormatRequiresInput(t *testing.T) {
	if !shouldTreatAsResponsesFormat([]byte(`{"model":"gpt-5","input":[{"role":"user","content":"hello"}]}`)) {
		t.Fatal("expected payload with input to be treated as responses format")
	}

	if shouldTreatAsResponsesFormat([]byte(`{"model":"gpt-5","instructions":"system prompt"}`)) {
		t.Fatal("expected instructions-only payload to stay out of responses normalization")
	}

	if shouldTreatAsResponsesFormat([]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hello"}],"input":[{"role":"user","content":"other"}]}`)) {
		t.Fatal("expected chat payload with messages to stay out of responses normalization")
	}
}
