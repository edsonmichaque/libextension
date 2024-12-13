package pluginkit

import (
	"context"
	"time"
)

// ExecuteOptions contains parameters for plugin execution
type ExecuteOptions struct {
	Args        []string          // Command line arguments
	Environment map[string]string // Environment variables
	WorkingDir  string            // Working directory for the plugin
}

// ExecuteResult contains the output of plugin execution
type ExecuteResult struct {
	ExitCode    int               // Process exit code
	Stdout      interface{}       // Standard output
	Stderr      interface{}       // Standard error
	StartTime   time.Time         // Time when the plugin execution started
	EndTime     time.Time         // Time when the plugin execution ended
	Duration    time.Duration     // Total execution duration
	CommandLine string            // Full command line that was executed
	WorkingDir  string            // Working directory used for execution
	Environment map[string]string // Environment variables used
	PID         int               // Process ID of the executed plugin
	Success     bool              // Whether the execution was successful (ExitCode == 0)
}

// Executor defines the interface for plugin execution
type Executor interface {
	// Configure applies configuration using a generic map
	Configure(config map[string]interface{}) error

	// Execute runs a plugin with the given options
	Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error)
}
