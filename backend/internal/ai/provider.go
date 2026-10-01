package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Provider is an AI service that can run both batch processing and web-search requests.
type Provider interface {
	Client
	Searcher
}

// ProviderConfig is what every provider is created from.
type ProviderConfig struct {
	APIKey    string
	Model     string
	MaxTokens int64
}

// ProviderFactory creates a Provider from its configuration.
type ProviderFactory func(ctx context.Context, cfg ProviderConfig) (Provider, error)

var providers = map[string]ProviderFactory{}

// RegisterProvider makes a provider selectable by name through AI_PROVIDER. Call it from
// an init function; to plug in another AI service, implement Provider and register it.
func RegisterProvider(name string, f ProviderFactory) {
	providers[name] = f
}

// NewProvider creates the provider registered under name.
func NewProvider(ctx context.Context, name string, cfg ProviderConfig) (Provider, error) {
	f, ok := providers[name]
	if !ok {
		known := make([]string, 0, len(providers))
		for n := range providers {
			known = append(known, n)
		}
		slices.Sort(known)
		return nil, fmt.Errorf("unknown AI_PROVIDER %q (registered: %s)", name, strings.Join(known, ", "))
	}
	return f(ctx, cfg)
}
