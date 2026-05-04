package ollama

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestModelMapperResolve(t *testing.T) {
	mapper := NewModelMapper([]config.OllamaModelMapping{
		{From: "gemma4:e2b", To: "kimi-k2-0905-preview"},
		{From: "gpt-oss:20b", To: "claude-sonnet-4-6"},
	})

	got, ok := mapper.Resolve("gemma4:e2b")
	if !ok {
		t.Fatalf("expected mapping to resolve")
	}
	if got != "kimi-k2-0905-preview" {
		t.Fatalf("target = %q, want kimi-k2-0905-preview", got)
	}

	got, ok = mapper.Resolve("missing")
	if ok {
		t.Fatalf("missing model resolved to %q", got)
	}
}

func TestModelMapperTrimsAndSkipsInvalid(t *testing.T) {
	mapper := NewModelMapper([]config.OllamaModelMapping{
		{From: " gemma4:e2b ", To: " kimi-k2-0905-preview "},
		{From: "", To: "target"},
		{From: "source", To: ""},
	})

	got, ok := mapper.Resolve("gemma4:e2b")
	if !ok || got != "kimi-k2-0905-preview" {
		t.Fatalf("trimmed mapping got (%q,%v)", got, ok)
	}
	if _, ok := mapper.Resolve("source"); ok {
		t.Fatalf("mapping with empty target should be skipped")
	}
}

func TestModelMapperMappingsReturnsCopy(t *testing.T) {
	mapper := NewModelMapper([]config.OllamaModelMapping{{From: "a", To: "b"}})
	mappings := mapper.Mappings()
	mappings[0].To = "mutated"

	got, ok := mapper.Resolve("a")
	if !ok || got != "b" {
		t.Fatalf("mapper should not be mutated by Mappings copy; got (%q,%v)", got, ok)
	}
}
