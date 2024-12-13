package pluginkit

import "context"

// StoreConfig contains configuration options for a plugin store
type StoreConfig map[string]interface{}

type SearchOptions map[string]string

type Store interface {
	Setup(config StoreConfig) error
	Fetch(ctx context.Context, name string, version string) (*Info, error)
	Search(ctx context.Context, criteria SearchOptions) ([]Info, error)
}
