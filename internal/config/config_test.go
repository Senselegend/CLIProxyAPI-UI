package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOllamaConfigUnmarshal(t *testing.T) {
	yamlSrc := []byte(`
ollama:
  enabled: true
  localhost-only: true
  embeddings-upstream-url: http://127.0.0.1:11434
  model-mappings:
    - from: "gemma4:e2b"
      to: "kimi-k2-0905-preview"
    - from: "gpt-oss:20b"
      to: "claude-sonnet-4-6"
`)
	var cfg Config
	if err := yaml.Unmarshal(yamlSrc, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !cfg.Ollama.Enabled {
		t.Errorf("expected Ollama.Enabled=true")
	}
	if !cfg.Ollama.LocalhostOnly {
		t.Errorf("expected Ollama.LocalhostOnly=true")
	}
	if cfg.Ollama.EmbeddingsUpstreamURL != "http://127.0.0.1:11434" {
		t.Errorf("got EmbeddingsUpstreamURL=%q", cfg.Ollama.EmbeddingsUpstreamURL)
	}
	if len(cfg.Ollama.ModelMappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(cfg.Ollama.ModelMappings))
	}
	if cfg.Ollama.ModelMappings[0].From != "gemma4:e2b" || cfg.Ollama.ModelMappings[0].To != "kimi-k2-0905-preview" {
		t.Errorf("bad mapping[0]: %+v", cfg.Ollama.ModelMappings[0])
	}
}

func TestOllamaLocalhostOnlyDefaultsTrueWhenOmitted(t *testing.T) {
	yamlSrc := []byte(`
ollama:
  enabled: true
`)
	var cfg Config
	if err := yaml.Unmarshal(yamlSrc, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !cfg.Ollama.LocalhostOnly {
		t.Fatalf("localhost-only should default true when omitted")
	}
}

func TestOllamaLocalhostOnlyCanBeExplicitlyDisabled(t *testing.T) {
	yamlSrc := []byte(`
ollama:
  enabled: true
  localhost-only: false
`)
	var cfg Config
	if err := yaml.Unmarshal(yamlSrc, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cfg.Ollama.LocalhostOnly {
		t.Fatalf("explicit localhost-only:false should be honored")
	}
}

func TestOllamaEmbeddingsURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty ok", "", false},
		{"http ok", "http://127.0.0.1:11434", false},
		{"https ok", "https://ollama.local", false},
		{"ftp not ok", "ftp://example.com", true},
		{"missing scheme", "127.0.0.1:11434", true},
		{"missing host", "http://", true},
		{"garbage", "://??/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOllamaConfig(&Ollama{EmbeddingsUpstreamURL: tc.url})
			if (err != nil) != tc.wantErr {
				t.Errorf("url=%q: want err=%v, got %v", tc.url, tc.wantErr, err)
			}
		})
	}
}
