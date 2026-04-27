package executor

import (
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func TestEnsureImageGenerationTool_NoToolsDoesNotInjectImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"draw a cat"}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	if gjson.GetBytes(result, "tools").Exists() {
		t.Fatalf("expected no injected tools, got %s", gjson.GetBytes(result, "tools").Raw)
	}
}

func TestEnsureImageGenerationTool_ExistingToolsWithoutImageGenDoesNotInjectImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","tools":[{"type":"function","name":"get_weather","parameters":{}}]}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	tools := gjson.GetBytes(result, "tools")
	arr := tools.Array()
	if len(arr) != 1 {
		t.Fatalf("expected only original tool, got %d", len(arr))
	}
	if arr[0].Get("type").String() != "function" {
		t.Fatalf("expected first tool type=function, got %s", arr[0].Get("type").String())
	}
}

func TestEnsureImageGenerationTool_ImageTriggerInjectsImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"/image draw a cat"}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	tools := gjson.GetBytes(result, "tools")
	if !tools.IsArray() {
		t.Fatalf("expected tools array, got %v", tools.Type)
	}
	arr := tools.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(arr))
	}
	if arr[0].Get("type").String() != "image_generation" {
		t.Fatalf("expected type=image_generation, got %s", arr[0].Get("type").String())
	}
	if arr[0].Get("output_format").String() != "png" {
		t.Fatalf("expected output_format=png, got %s", arr[0].Get("output_format").String())
	}
}

func TestEnsureImageGenerationTool_ImageTriggerAppendsToExistingTools(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"please /image draw a cat","tools":[{"type":"web_search"}]}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	tools := gjson.GetBytes(result, "tools")
	arr := tools.Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(arr))
	}
	if arr[0].Get("type").String() != "web_search" {
		t.Fatalf("expected first tool type=web_search, got %s", arr[0].Get("type").String())
	}
	if arr[1].Get("type").String() != "image_generation" {
		t.Fatalf("expected second tool type=image_generation, got %s", arr[1].Get("type").String())
	}
}

func TestEnsureImageGenerationTool_ImageTriggerDoesNotDuplicateExistingImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"/image draw a cat","tools":[{"type":"image_generation","output_format":"webp"},{"type":"function","name":"f1"}]}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	tools := gjson.GetBytes(result, "tools")
	arr := tools.Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 tools (no duplicate), got %d", len(arr))
	}
	if arr[0].Get("output_format").String() != "webp" {
		t.Fatalf("expected original output_format=webp preserved, got %s", arr[0].Get("output_format").String())
	}
}

func TestEnsureImageGenerationTool_ImageTriggerFalsePositivesDoNotInjectImageGeneration(t *testing.T) {
	cases := []string{
		`{"model":"gpt-5.4","input":"document the /images endpoint"}`,
		`{"model":"gpt-5.4","input":"open https://example.com/image"}`,
		`{"model":"gpt-5.4","input":"use /image-generation docs"}`,
	}
	for _, body := range cases {
		result := ensureImageGenerationTool([]byte(body), "gpt-5.4", nil)
		if string(result) != body {
			t.Fatalf("expected body to be unchanged, got %s", string(result))
		}
		if gjson.GetBytes(result, "tools").Exists() {
			t.Fatalf("expected no injected tools, got %s", gjson.GetBytes(result, "tools").Raw)
		}
	}
}

func TestEnsureImageGenerationTool_EmptyToolsArrayDoesNotInjectImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","tools":[]}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	tools := gjson.GetBytes(result, "tools")
	arr := tools.Array()
	if len(arr) != 0 {
		t.Fatalf("expected no tools, got %d", len(arr))
	}
}

func TestEnsureImageGenerationTool_WebSearchDoesNotInjectImageGeneration(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","tools":[{"type":"web_search"}]}`)
	result := ensureImageGenerationTool(body, "gpt-5.4", nil)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	tools := gjson.GetBytes(result, "tools")
	arr := tools.Array()
	if len(arr) != 1 {
		t.Fatalf("expected only original tool, got %d", len(arr))
	}
	if arr[0].Get("type").String() != "web_search" {
		t.Fatalf("expected first tool type=web_search, got %s", arr[0].Get("type").String())
	}
}

func TestEnsureImageGenerationTool_GPT53CodexSparkDoesNotInjectTool(t *testing.T) {
	body := []byte(`{"model":"gpt-5.3-codex-spark","input":"draw a cat"}`)
	result := ensureImageGenerationTool(body, "gpt-5.3-codex-spark", nil)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	if gjson.GetBytes(result, "tools").Exists() {
		t.Fatalf("expected no tools for gpt-5.3-codex-spark, got %s", gjson.GetBytes(result, "tools").Raw)
	}
}

func TestEnsureImageGenerationTool_FreeCodexAuthDoesNotInjectTool(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"draw a cat"}`)
	freeAuth := &cliproxyauth.Auth{
		Provider:   "codex",
		Attributes: map[string]string{"plan_type": "free"},
	}
	result := ensureImageGenerationTool(body, "gpt-5.4", freeAuth)

	if string(result) != string(body) {
		t.Fatalf("expected body to be unchanged, got %s", string(result))
	}
	if gjson.GetBytes(result, "tools").Exists() {
		t.Fatalf("expected no tools for free codex auth, got %s", gjson.GetBytes(result, "tools").Raw)
	}
}
