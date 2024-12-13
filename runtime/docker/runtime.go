package pluginkit

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DockerExecutor implements the Executor interface for Docker-based plugins
type DockerExecutor struct {
	pluginDir    string
	dockerPath   string
	networkMode  string
	extraLabels  map[string]string
	extraOptions []string
}

// NewDockerExecutor creates a new DockerExecutor instance
func NewDockerExecutor(pluginDir string) *DockerExecutor {
	return &DockerExecutor{
		pluginDir: pluginDir,
	}
}

// Execute runs a Docker plugin with the given options
func (e *DockerExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	startTime := time.Now()

	// Build Docker command arguments
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
	cmd := exec.CommandContext(ctx, "docker", args...)

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
			return nil, fmt.Errorf("failed to execute Docker command: %w", err)
		}
	}

	// Build command line for logging
	commandLine := fmt.Sprintf("docker %s", strings.Join(args, " "))

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
		PID:         0, // Docker containers don't expose host PIDs
		Success:     exitCode == 0,
	}, nil
}

// Configure applies the provided configuration map
func (e *DockerExecutor) Configure(config map[string]interface{}) error {
	// Extract plugin directory
	if pluginDir, ok := config["plugin_dir"].(string); ok {
		e.pluginDir = pluginDir
	}

	// Extract docker path
	if dockerPath, ok := config["docker_path"].(string); ok {
		e.dockerPath = dockerPath
	} else {
		e.dockerPath = "docker" // default
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
