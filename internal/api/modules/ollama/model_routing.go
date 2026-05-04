package ollama

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

type ModelMapper struct {
	exact    map[string]string
	mappings []config.OllamaModelMapping
}

func NewModelMapper(mappings []config.OllamaModelMapping) *ModelMapper {
	m := &ModelMapper{
		exact:    make(map[string]string, len(mappings)),
		mappings: make([]config.OllamaModelMapping, 0, len(mappings)),
	}
	for _, mapping := range mappings {
		from := strings.TrimSpace(mapping.From)
		to := strings.TrimSpace(mapping.To)
		if from == "" || to == "" {
			continue
		}
		m.exact[from] = to
		m.mappings = append(m.mappings, config.OllamaModelMapping{From: from, To: to})
	}
	return m
}

func (m *ModelMapper) Resolve(name string) (string, bool) {
	if m == nil {
		return "", false
	}
	target, ok := m.exact[strings.TrimSpace(name)]
	return target, ok
}

func (m *ModelMapper) Mappings() []config.OllamaModelMapping {
	if m == nil {
		return nil
	}
	out := make([]config.OllamaModelMapping, len(m.mappings))
	copy(out, m.mappings)
	return out
}
