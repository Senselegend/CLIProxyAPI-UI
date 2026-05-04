package ollama

import (
	"net/http/httputil"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/modules"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	openai "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/openai"
	log "github.com/sirupsen/logrus"
)

type Option func(*OllamaModule)

type OllamaModule struct {
	registerOnce sync.Once
	configMu     sync.RWMutex
	restrictMu   sync.RWMutex
	proxyMu      sync.RWMutex

	modelMapper atomic.Pointer[ModelMapper]
	embedProxy  *httputil.ReverseProxy
	lastConfig  *config.Ollama

	enabled             atomic.Bool
	restrictToLocalhost bool
}

func New(opts ...Option) *OllamaModule {
	m := &OllamaModule{}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *OllamaModule) Name() string {
	return "ollama-routing"
}

func (m *OllamaModule) Register(ctx modules.Context) error {
	settings := ctx.Config.Ollama
	var regErr error
	m.registerOnce.Do(func() {
		m.setModelMapper(NewModelMapper(settings.ModelMappings))
		m.enabled.Store(settings.Enabled)
		m.setRestrictToLocalhost(settings.LocalhostOnly)
		settingsCopy := settings
		m.lastConfig = &settingsCopy
		proxy, err := NewEmbeddingsProxy(settings.EmbeddingsUpstreamURL)
		if err != nil {
			regErr = err
			return
		}
		m.setEmbedProxy(proxy)
		openaiHandlers := openai.NewOpenAIAPIHandler(ctx.BaseHandler)
		m.registerRoutes(ctx.Engine, openaiHandlers.ChatCompletions)
	})
	return regErr
}

func (m *OllamaModule) OnConfigUpdated(cfg *config.Config) error {
	newSettings := cfg.Ollama
	m.configMu.RLock()
	oldSettings := m.lastConfig
	m.configMu.RUnlock()

	if oldSettings == nil || oldSettings.Enabled != newSettings.Enabled {
		m.enabled.Store(newSettings.Enabled)
	}
	if oldSettings == nil || oldSettings.LocalhostOnly != newSettings.LocalhostOnly {
		m.setRestrictToLocalhost(newSettings.LocalhostOnly)
	}
	if oldSettings == nil || !reflect.DeepEqual(oldSettings.ModelMappings, newSettings.ModelMappings) {
		m.setModelMapper(NewModelMapper(newSettings.ModelMappings))
	}
	oldUpstream := ""
	if oldSettings != nil {
		oldUpstream = strings.TrimSpace(oldSettings.EmbeddingsUpstreamURL)
	}
	newUpstream := strings.TrimSpace(newSettings.EmbeddingsUpstreamURL)
	if oldSettings == nil || oldUpstream != newUpstream {
		proxy, err := NewEmbeddingsProxy(newUpstream)
		if err != nil {
			log.Errorf("ollama config: failed to update embeddings proxy: %v", err)
		} else {
			m.setEmbedProxy(proxy)
		}
	}

	m.configMu.Lock()
	settingsCopy := newSettings
	m.lastConfig = &settingsCopy
	m.configMu.Unlock()
	return nil
}

func (m *OllamaModule) isEnabled() bool {
	return m.enabled.Load()
}

func (m *OllamaModule) setModelMapper(mapper *ModelMapper) {
	m.modelMapper.Store(mapper)
}

func (m *OllamaModule) getModelMapper() *ModelMapper {
	mapper := m.modelMapper.Load()
	if mapper == nil {
		return NewModelMapper(nil)
	}
	return mapper
}

func (m *OllamaModule) setRestrictToLocalhost(restrict bool) {
	m.restrictMu.Lock()
	defer m.restrictMu.Unlock()
	m.restrictToLocalhost = restrict
}

func (m *OllamaModule) isRestrictedToLocalhost() bool {
	m.restrictMu.RLock()
	defer m.restrictMu.RUnlock()
	return m.restrictToLocalhost
}

func (m *OllamaModule) setEmbedProxy(proxy *httputil.ReverseProxy) {
	m.proxyMu.Lock()
	defer m.proxyMu.Unlock()
	m.embedProxy = proxy
}

func (m *OllamaModule) getEmbedProxy() *httputil.ReverseProxy {
	m.proxyMu.RLock()
	defer m.proxyMu.RUnlock()
	return m.embedProxy
}
