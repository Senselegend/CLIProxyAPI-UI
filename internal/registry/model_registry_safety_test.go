package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetModelInfoReturnsClone(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "gemini", []*ModelInfo{{
		ID:          "m1",
		DisplayName: "Model One",
		Thinking:    &ThinkingSupport{Min: 1, Max: 2, Levels: []string{"low", "high"}},
	}})

	first := r.GetModelInfo("m1", "gemini")
	if first == nil {
		t.Fatal("expected model info")
	}
	first.DisplayName = "mutated"
	first.Thinking.Levels[0] = "mutated"

	second := r.GetModelInfo("m1", "gemini")
	if second.DisplayName != "Model One" {
		t.Fatalf("expected cloned display name, got %q", second.DisplayName)
	}
	if second.Thinking == nil || len(second.Thinking.Levels) == 0 || second.Thinking.Levels[0] != "low" {
		t.Fatalf("expected cloned thinking levels, got %+v", second.Thinking)
	}
}

func TestGetModelsForClientReturnsClones(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "gemini", []*ModelInfo{{
		ID:          "m1",
		DisplayName: "Model One",
		Thinking:    &ThinkingSupport{Levels: []string{"low", "high"}},
	}})

	first := r.GetModelsForClient("client-1")
	if len(first) != 1 || first[0] == nil {
		t.Fatalf("expected one model, got %+v", first)
	}
	first[0].DisplayName = "mutated"
	first[0].Thinking.Levels[0] = "mutated"

	second := r.GetModelsForClient("client-1")
	if len(second) != 1 || second[0] == nil {
		t.Fatalf("expected one model on second fetch, got %+v", second)
	}
	if second[0].DisplayName != "Model One" {
		t.Fatalf("expected cloned display name, got %q", second[0].DisplayName)
	}
	if second[0].Thinking == nil || len(second[0].Thinking.Levels) == 0 || second[0].Thinking.Levels[0] != "low" {
		t.Fatalf("expected cloned thinking levels, got %+v", second[0].Thinking)
	}
}

func TestGetAvailableModelsByProviderReturnsClones(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "gemini", []*ModelInfo{{
		ID:          "m1",
		DisplayName: "Model One",
		Thinking:    &ThinkingSupport{Levels: []string{"low", "high"}},
	}})

	first := r.GetAvailableModelsByProvider("gemini")
	if len(first) != 1 || first[0] == nil {
		t.Fatalf("expected one model, got %+v", first)
	}
	first[0].DisplayName = "mutated"
	first[0].Thinking.Levels[0] = "mutated"

	second := r.GetAvailableModelsByProvider("gemini")
	if len(second) != 1 || second[0] == nil {
		t.Fatalf("expected one model on second fetch, got %+v", second)
	}
	if second[0].DisplayName != "Model One" {
		t.Fatalf("expected cloned display name, got %q", second[0].DisplayName)
	}
	if second[0].Thinking == nil || len(second[0].Thinking.Levels) == 0 || second[0].Thinking.Levels[0] != "low" {
		t.Fatalf("expected cloned thinking levels, got %+v", second[0].Thinking)
	}
}

func TestCleanupExpiredQuotasInvalidatesAvailableModelsCache(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "openai", []*ModelInfo{{ID: "m1", Created: 1}})
	r.SetModelQuotaExceeded("client-1", "m1")
	if models := r.GetAvailableModels("openai"); len(models) != 1 {
		t.Fatalf("expected cooldown model to remain listed before cleanup, got %d", len(models))
	}

	r.mutex.Lock()
	quotaTime := time.Now().Add(-6 * time.Minute)
	r.models["m1"].QuotaExceededClients["client-1"] = &quotaTime
	r.mutex.Unlock()

	r.CleanupExpiredQuotas()

	if count := r.GetModelCount("m1"); count != 1 {
		t.Fatalf("expected model count 1 after cleanup, got %d", count)
	}
	models := r.GetAvailableModels("openai")
	if len(models) != 1 {
		t.Fatalf("expected model to stay available after cleanup, got %d", len(models))
	}
	if got := models[0]["id"]; got != "m1" {
		t.Fatalf("expected model id m1, got %v", got)
	}
}

func TestGetAvailableModelsReturnsClonedSupportedParameters(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "openai", []*ModelInfo{{
		ID:                  "m1",
		DisplayName:         "Model One",
		SupportedParameters: []string{"temperature", "top_p"},
	}})

	first := r.GetAvailableModels("openai")
	if len(first) != 1 {
		t.Fatalf("expected one model, got %d", len(first))
	}
	params, ok := first[0]["supported_parameters"].([]string)
	if !ok || len(params) != 2 {
		t.Fatalf("expected supported_parameters slice, got %#v", first[0]["supported_parameters"])
	}
	params[0] = "mutated"

	second := r.GetAvailableModels("openai")
	params, ok = second[0]["supported_parameters"].([]string)
	if !ok || len(params) != 2 || params[0] != "temperature" {
		t.Fatalf("expected cloned supported_parameters, got %#v", second[0]["supported_parameters"])
	}
}

func TestGetAvailableModelsReturnsClonedAdditionalSpeedTiers(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "openai", []*ModelInfo{{
		ID:                   "m1",
		DisplayName:          "Model One",
		AdditionalSpeedTiers: []string{"fast"},
	}})

	first := r.GetAvailableModels("openai")
	if len(first) != 1 {
		t.Fatalf("expected one model, got %d", len(first))
	}
	tiers, ok := first[0]["additional_speed_tiers"].([]string)
	if !ok || len(tiers) != 1 || tiers[0] != "fast" {
		t.Fatalf("expected additional_speed_tiers [fast], got %#v", first[0]["additional_speed_tiers"])
	}
	tiers[0] = "mutated"

	second := r.GetAvailableModels("openai")
	tiers, ok = second[0]["additional_speed_tiers"].([]string)
	if !ok || len(tiers) != 1 || tiers[0] != "fast" {
		t.Fatalf("expected cloned additional_speed_tiers, got %#v", second[0]["additional_speed_tiers"])
	}
}

func TestGetAvailableModelsOmitsAdditionalSpeedTiersWhenAbsent(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "openai", []*ModelInfo{{
		ID:          "m1",
		DisplayName: "Model One",
	}})

	models := r.GetAvailableModels("openai")
	if len(models) != 1 {
		t.Fatalf("expected one model, got %d", len(models))
	}
	if _, exists := models[0]["additional_speed_tiers"]; exists {
		t.Fatalf("expected additional_speed_tiers to be omitted, got %#v", models[0]["additional_speed_tiers"])
	}
}

func TestLookupModelInfoReturnsCloneForStaticDefinitions(t *testing.T) {
	first := LookupModelInfo("claude-sonnet-4-6")
	if first == nil || first.Thinking == nil || len(first.Thinking.Levels) == 0 {
		t.Fatalf("expected static model with thinking levels, got %+v", first)
	}
	first.Thinking.Levels[0] = "mutated"

	second := LookupModelInfo("claude-sonnet-4-6")
	if second == nil || second.Thinking == nil || len(second.Thinking.Levels) == 0 || second.Thinking.Levels[0] == "mutated" {
		t.Fatalf("expected static lookup clone, got %+v", second)
	}
}

func TestLookupModelInfoReturnsCloneForAdditionalSpeedTiers(t *testing.T) {
	first := LookupModelInfo("gpt-5.4")
	if first == nil || len(first.AdditionalSpeedTiers) == 0 {
		t.Fatalf("expected static model with additional speed tiers, got %+v", first)
	}
	first.AdditionalSpeedTiers[0] = "mutated"

	second := LookupModelInfo("gpt-5.4")
	if second == nil || len(second.AdditionalSpeedTiers) == 0 || second.AdditionalSpeedTiers[0] != "fast" {
		t.Fatalf("expected static lookup clone for additional speed tiers, got %+v", second)
	}
}

func TestLoadModelsFromBytesPreservesEmbeddedAdditionalSpeedTiersOnRemoteRefresh(t *testing.T) {
	original := getModels()
	if original == nil {
		t.Fatal("expected initial models catalog")
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original catalog: %v", err)
	}

	var remote staticModelsJSON
	if err := json.Unmarshal(payload, &remote); err != nil {
		t.Fatalf("unmarshal remote catalog: %v", err)
	}

	remoteModel := findModelByID(remote.CodexPro, "gpt-5.4")
	if remoteModel == nil {
		t.Fatal("expected gpt-5.4 in codex-pro catalog")
	}
	remoteModel.AdditionalSpeedTiers = nil

	remotePayload, err := json.Marshal(&remote)
	if err != nil {
		t.Fatalf("marshal remote catalog: %v", err)
	}

	t.Cleanup(func() {
		modelsCatalogStore.mu.Lock()
		modelsCatalogStore.data = original
		modelsCatalogStore.mu.Unlock()
	})

	if err := loadModelsFromBytes(remotePayload, "test-remote"); err != nil {
		t.Fatalf("load remote catalog: %v", err)
	}

	refreshed := LookupModelInfo("gpt-5.4")
	if refreshed == nil {
		t.Fatal("expected refreshed gpt-5.4")
	}
	if len(refreshed.AdditionalSpeedTiers) == 0 || refreshed.AdditionalSpeedTiers[0] != "fast" {
		t.Fatalf("expected embedded additional speed tiers to survive refresh, got %+v", refreshed.AdditionalSpeedTiers)
	}
}

func TestTryRefreshModelsPreservesEmbeddedAdditionalSpeedTiers(t *testing.T) {
	originalCatalog := getModels()
	if originalCatalog == nil {
		t.Fatal("expected initial models catalog")
	}
	originalURLs := append([]string(nil), modelsURLs...)

	payload, err := json.Marshal(originalCatalog)
	if err != nil {
		t.Fatalf("marshal original catalog: %v", err)
	}

	var remote staticModelsJSON
	if err := json.Unmarshal(payload, &remote); err != nil {
		t.Fatalf("unmarshal remote catalog: %v", err)
	}
	remoteModel := findModelByID(remote.CodexPro, "gpt-5.4")
	if remoteModel == nil {
		t.Fatal("expected gpt-5.4 in codex-pro catalog")
	}
	remoteModel.AdditionalSpeedTiers = nil

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&remote)
	}))
	defer server.Close()

	t.Cleanup(func() {
		modelsCatalogStore.mu.Lock()
		modelsCatalogStore.data = originalCatalog
		modelsCatalogStore.mu.Unlock()
		modelsURLs = originalURLs
	})

	modelsURLs = []string{server.URL}
	tryRefreshModels(context.Background(), "test-refresh")

	refreshed := LookupModelInfo("gpt-5.4")
	if refreshed == nil {
		t.Fatal("expected refreshed gpt-5.4")
	}
	if len(refreshed.AdditionalSpeedTiers) == 0 || refreshed.AdditionalSpeedTiers[0] != "fast" {
		t.Fatalf("expected tryRefreshModels to preserve additional speed tiers, got %+v", refreshed.AdditionalSpeedTiers)
	}
}

func findModelByID(models []*ModelInfo, id string) *ModelInfo {
	for _, model := range models {
		if model != nil && model.ID == id {
			return model
		}
	}
	return nil
}
