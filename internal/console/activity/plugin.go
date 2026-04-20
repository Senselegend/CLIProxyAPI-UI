package activity

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

type UsagePlugin struct {
	store *Store
}

func init() {
	coreusage.RegisterPlugin(NewUsagePlugin(DefaultStore()))
}

func NewUsagePlugin(store *Store) *UsagePlugin {
	return &UsagePlugin{store: store}
}

func (p *UsagePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || p.store == nil {
		return
	}
	p.store.EnrichUsage(logging.GetRequestID(ctx), record)
}
