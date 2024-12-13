package pluginkit

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NerdctlExecutor implements the Executor interface for Nerdctl-based plugins
type NerdctlExecutor struct {
	pluginDir string
}

// NewNerdctlExecutor creates a new NerdctlExecutor instance
func NewNerdctlExecutor(pluginDir string) *NerdctlExecutor {
	return &NerdctlExecutor{
		pluginDir: pluginDir,
	}
}

// Execute runs a Nerdctl plugin with the given options
func (e *NerdctlExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	startTime := time.Now()

	// Build Nerdctl command arguments
	args := []string{"run", "--rm"}

	// Add environment variables
	for k, v := range opts.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add working directory mount if specified
	if opts.WorkingDir != "" {
		args = append(args, "-v", fmt.Sprintf("%s:/app", opts.WorkingDir))
		args = append(args, "-w", "/app")
	}

	// Add image name and command arguments
	args = append(args, pluginName)
	args = append(args, opts.Args...)

	// Create command
	cmd := exec.CommandContext(ctx, "nerdctl", args...)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()
	endTime := time.Now()

	// Handle exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute Nerdctl command: %w", err)
		}
	}

	// Build command line for logging
	commandLine := fmt.Sprintf("nerdctl %s", strings.Join(args, " "))

	return &ExecuteResult{
		ExitCode:    exitCode,
		Stdout:      stdout.Bytes(),
		Stderr:      stderr.Bytes(),
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    endTime.Sub(startTime),
		CommandLine: commandLine,
		WorkingDir:  opts.WorkingDir,
		Environment: opts.Environment,
		PID:         0, // Nerdctl containers don't expose host PIDs
		Success:     exitCode == 0,
	}, nil
}
