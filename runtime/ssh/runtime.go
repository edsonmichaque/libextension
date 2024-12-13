package extension

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SSHExecutor implements the Executor interface for SSH-based plugins
type SSHExecutor struct {
	host       string   // Remote host address
	user       string   // SSH user
	port       int      // SSH port
	keyPath    string   // Path to SSH private key
	sshOptions []string // Additional SSH options
}

// SSHConfig holds configuration for the SSH executor
type SSHConfig struct {
	Host       string
	User       string
	Port       int
	KeyPath    string
	SSHOptions []string
}

// NewSSHExecutor creates a new SSHExecutor instance
func NewSSHExecutor(config SSHConfig) *SSHExecutor {
	if config.Port == 0 {
		config.Port = 22
	}
	if config.User == "" {
		config.User = "root"
	}
	defaultOptions := []string{
		"StrictHostKeyChecking=no",
		"UserKnownHostsFile=/dev/null",
		"ConnectTimeout=10",
	}
	if config.SSHOptions == nil {
		config.SSHOptions = defaultOptions
	}

	return &SSHExecutor{
		host:       config.Host,
		user:       config.User,
		port:       config.Port,
		keyPath:    config.KeyPath,
		sshOptions: config.SSHOptions,
	}
}

// Execute runs a command on the remote host via SSH
func (e *SSHExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	startTime := time.Now()

	// Prepare SSH arguments
	sshArgs := []string{
		"-p", fmt.Sprintf("%d", e.port),
	}

	// Add SSH key if specified
	if e.keyPath != "" {
		sshArgs = append(sshArgs, "-i", e.keyPath)
	}

	// Add SSH options
	for _, opt := range e.sshOptions {
		sshArgs = append(sshArgs, "-o", opt)
	}

	// Add target host
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", e.user, e.host))

	// Prepare environment variables
	envVars := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		envVars = append(envVars, fmt.Sprintf("export %s=%s;", k, v))
	}

	// Build command with environment and working directory
	command := strings.Join(append(envVars, strings.Join(opts.Args, " ")), " ")
	if opts.WorkingDir != "" {
		command = fmt.Sprintf("cd %s && %s", opts.WorkingDir, command)
	}
	sshArgs = append(sshArgs, command)

	// Create command
	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)

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
			return nil, fmt.Errorf("failed to execute SSH command: %w", err)
		}
	}

	// Build command line for logging
	commandLine := fmt.Sprintf("ssh://%s@%s:%d/%s", e.user, e.host, e.port, strings.Join(opts.Args, " "))

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
		PID:         0, // Remote execution, no local PID
		Success:     exitCode == 0,
	}, nil
}

// TestConnection verifies SSH connectivity to the remote host
func (e *SSHExecutor) TestConnection(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "ssh",
		"-p", fmt.Sprintf("%d", e.port),
		"-i", e.keyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", e.user, e.host),
		"echo test",
	)
	return cmd.Run()
}

// Configure applies the provided configuration map
func (e *SSHExecutor) Configure(config map[string]interface{}) error {
	// Extract host
	if host, ok := config["host"].(string); ok {
		e.host = host
	}

	// Extract user
	if user, ok := config["user"].(string); ok {
		e.user = user
	} else {
		e.user = "root" // default
	}

	// Extract port
	if port, ok := config["port"].(float64); ok {
		e.port = int(port)
	} else {
		e.port = 22 // default
	}

	// Extract key path
	if keyPath, ok := config["key_path"].(string); ok {
		e.keyPath = keyPath
	}

	// Extract SSH options
	if options, ok := config["ssh_options"].([]interface{}); ok {
		e.sshOptions = make([]string, 0, len(options))
		for _, opt := range options {
			if strOpt, ok := opt.(string); ok {
				e.sshOptions = append(e.sshOptions, strOpt)
			}
		}
	} else {
		// Default SSH options
		e.sshOptions = []string{
			"StrictHostKeyChecking=no",
			"UserKnownHostsFile=/dev/null",
			"ConnectTimeout=10",
		}
	}

	// Validate required fields
	if e.host == "" {
		return fmt.Errorf("host is required")
	}

	return nil
}
