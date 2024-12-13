package extension

import (
	"context"
)

// Runtime interface defines how plugins are executed
type Runtime interface {
	// List returns all installed plugins
	List(ctx context.Context) ([]*Plugin, error)
	// Install sets up a new plugin
	Install(ctx context.Context, plugin *Plugin) error
	// Uninstall removes a plugin
	Uninstall(ctx context.Context, plugin *Plugin) error
	// Execute runs a plugin with given arguments
	Execute(ctx context.Context, plugin *Plugin, args []string) error
	// Setup prepares the environment for the plugin
	Setup(ctx context.Context, plugin *Plugin) error
	// Cleanup performs any necessary cleanup after plugin execution
	Cleanup(ctx context.Context, plugin *Plugin) error
}
