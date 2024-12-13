package extension

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PodmanExecutor implements the Executor interface for Podman-based plugins
type PodmanExecutor struct {
	pluginDir    string
	networkMode  string
	extraLabels  map[string]string
	podmanPath   string
	extraOptions []string
}

// Name returns the executor's name
func (e *PodmanExecutor) Name() string {
	return "podman"
}

// ConfigSchema returns the JSON schema for the executor's configuration
func (e *PodmanExecutor) ConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plugin_dir": map[string]interface{}{
				"type":        "string",
				"description": "Directory containing plugin files",
			},
			"network_mode": map[string]interface{}{
				"type":        "string",
				"description": "Network mode for containers (e.g., host, bridge)",
				"default":     "host",
			},
			"extra_labels": map[string]interface{}{
				"type":        "object",
				"description": "Additional labels to add to containers",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
			"podman_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to podman executable",
				"default":     "podman",
			},
			"extra_options": map[string]interface{}{
				"type":        "array",
				"description": "Additional podman run options",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"security_opts": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"memory_limit": map[string]interface{}{
						"type":        "string",
						"description": "Container memory limit (e.g., 512m, 1g)",
						"default":     "512m",
					},
					"cpu_shares": map[string]interface{}{
						"type":        "integer",
						"description": "CPU shares (relative weight)",
						"default":     1024,
					},
					"pids_limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of processes",
						"default":     100,
					},
					"allowed_capabilities": map[string]interface{}{
						"type":        "array",
						"description": "List of allowed Linux capabilities",
						"items": map[string]interface{}{
							"type": "string",
						},
						"default": []interface{}{},
					},
				},
			},
		},
		"required": []string{"plugin_dir"},
	}
}

// NewPodmanExecutor creates a new PodmanExecutor instance
func NewPodmanExecutor(pluginDir string) *PodmanExecutor {
	return &PodmanExecutor{
		pluginDir: pluginDir,
	}
}

// Execute runs a Podman plugin with the given options
func (e *PodmanExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	startTime := time.Now()

	// Build Podman command arguments with security defaults
	args := []string{"run", "--rm",
		"--security-opt=no-new-privileges", // Prevent privilege escalation
		"--cap-drop=ALL",                   // Drop all capabilities by default
		"--read-only",                      // Make root filesystem read-only
		"--tmpfs=/tmp:rw,noexec,nosuid",    // Secure temp directory
		"--pids-limit=100",                 // Limit number of processes
		"--memory=512m",                    // Limit memory usage
		"--cpu-shares=1024",                // Limit CPU usage
	}

	// Add network mode (consider restricting to specific networks)
	if e.networkMode == "" {
		e.networkMode = "none" // Default to no network access
	}
	args = append(args, fmt.Sprintf("--network=%s", e.networkMode))

	// Add labels
	for k, v := range e.extraLabels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}

	// Add extra options
	args = append(args, e.extraOptions...)

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

	// Create command (use configured podman path)
	cmd := exec.CommandContext(ctx, e.podmanPath, args...)

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
			return nil, fmt.Errorf("failed to execute Podman command: %w", err)
		}
	}

	// Build command line for logging
	commandLine := fmt.Sprintf("podman %s", strings.Join(args, " "))

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
		PID:         0, // Podman containers don't expose host PIDs
		Success:     exitCode == 0,
	}, nil
}

// Configure applies the provided configuration map
func (e *PodmanExecutor) Configure(config map[string]interface{}) error {
	// Extract plugin directory
	if pluginDir, ok := config["plugin_dir"].(string); ok {
		e.pluginDir = pluginDir
	}

	// Extract network mode
	if networkMode, ok := config["network_mode"].(string); ok {
		e.networkMode = networkMode
	} else {
		e.networkMode = "host" // default
	}

	// Extract extra labels
	if labels, ok := config["extra_labels"].(map[string]interface{}); ok {
		e.extraLabels = make(map[string]string)
		for k, v := range labels {
			if strVal, ok := v.(string); ok {
				e.extraLabels[k] = strVal
			}
		}
	}

	// Extract podman path
	if podmanPath, ok := config["podman_path"].(string); ok {
		e.podmanPath = podmanPath
	} else {
		e.podmanPath = "podman" // default
	}

	// Extract extra options
	if options, ok := config["extra_options"].([]interface{}); ok {
		e.extraOptions = make([]string, 0, len(options))
		for _, opt := range options {
			if strOpt, ok := opt.(string); ok {
				e.extraOptions = append(e.extraOptions, strOpt)
			}
		}
	}

	// Validate required fields
	if e.pluginDir == "" {
		return fmt.Errorf("plugin_dir is required")
	}

	return nil
}
