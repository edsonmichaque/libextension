package pluginkit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// QEMUExecutor implements the Executor interface for QEMU-based plugins
type QEMUExecutor struct {
	imageDir   string // Directory containing VM images
	sshKeyPath string // Path to SSH private key
	sshPort    int    // SSH port for communication
	memory     string // VM memory allocation (e.g., "2G")
	cpus       int    // Number of CPU cores
}

// QEMUConfig holds configuration for the QEMU executor
type QEMUConfig struct {
	ImageDir   string
	SSHKeyPath string
	SSHPort    int
	Memory     string
	CPUs       int
}

// NewQEMUExecutor creates a new QEMUExecutor instance
func NewQEMUExecutor(config QEMUConfig) *QEMUExecutor {
	if config.SSHPort == 0 {
		config.SSHPort = 2222
	}
	if config.Memory == "" {
		config.Memory = "2G"
	}
	if config.CPUs == 0 {
		config.CPUs = 2
	}
	return &QEMUExecutor{
		imageDir:   config.ImageDir,
		sshKeyPath: config.SSHKeyPath,
		sshPort:    config.SSHPort,
		memory:     config.Memory,
		cpus:       config.CPUs,
	}
}

// Execute runs a plugin in a QEMU VM
func (e *QEMUExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	startTime := time.Now()

	// Construct paths
	imagePath := filepath.Join(e.imageDir, pluginName, "disk.qcow2")
	if _, err := os.Stat(imagePath); err != nil {
		return nil, fmt.Errorf("VM image not found: %w", err)
	}

	// Prepare QEMU arguments
	qemuArgs := []string{
		"-machine", "type=q35,accel=kvm",
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", e.cpus),
		"-m", e.memory,
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback,discard=unmap,format=qcow2", imagePath),
		"-net", "nic,model=virtio",
		"-net", fmt.Sprintf("user,hostfwd=tcp::%d-:22", e.sshPort),
		"-display", "none",
		"-daemonize",
	}

	// Start QEMU VM
	startCmd := exec.CommandContext(ctx, "qemu-system-x86_64", qemuArgs...)
	if err := startCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to start VM: %w", err)
	}

	// Wait for SSH to become available
	if err := e.waitForSSH(ctx); err != nil {
		return nil, fmt.Errorf("SSH connection failed: %w", err)
	}

	// Prepare environment variables
	envVars := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		envVars = append(envVars, fmt.Sprintf("export %s=%s;", k, v))
	}

	// Prepare command
	sshArgs := []string{
		"-i", e.sshKeyPath,
		"-p", fmt.Sprintf("%d", e.sshPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"user@localhost",
	}

	// Build command with environment and working directory
	command := strings.Join(append(envVars, strings.Join(opts.Args, " ")), " ")
	if opts.WorkingDir != "" {
		command = fmt.Sprintf("cd %s && %s", opts.WorkingDir, command)
	}
	sshArgs = append(sshArgs, command)

	// Execute command via SSH
	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	endTime := time.Now()

	// Cleanup: Shutdown VM
	defer e.shutdownVM(ctx)

	// Handle exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command in VM: %w", err)
		}
	}

	return &ExecuteResult{
		ExitCode:    exitCode,
		Stdout:      stdout.Bytes(),
		Stderr:      stderr.Bytes(),
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    endTime.Sub(startTime),
		CommandLine: fmt.Sprintf("qemu://%s/%s", imagePath, strings.Join(opts.Args, " ")),
		WorkingDir:  opts.WorkingDir,
		Environment: opts.Environment,
		PID:         0, // VM PID not exposed
		Success:     exitCode == 0,
	}, nil
}

// waitForSSH attempts to establish SSH connection until successful or timeout
func (e *QEMUExecutor) waitForSSH(ctx context.Context) error {
	timeout := time.After(30 * time.Second)
	tick := time.Tick(1 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for SSH connection")
		case <-tick:
			cmd := exec.Command("ssh",
				"-i", e.sshKeyPath,
				"-p", fmt.Sprintf("%d", e.sshPort),
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "ConnectTimeout=1",
				"user@localhost",
				"echo test",
			)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
}

// shutdownVM gracefully stops the QEMU VM
func (e *QEMUExecutor) shutdownVM(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "ssh",
		"-i", e.sshKeyPath,
		"-p", fmt.Sprintf("%d", e.sshPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"user@localhost",
		"sudo shutdown -h now",
	)
	return cmd.Run()
}

// Configure applies the provided configuration map
func (e *QEMUExecutor) Configure(config map[string]interface{}) error {
	// Extract image directory
	if imageDir, ok := config["image_dir"].(string); ok {
		e.imageDir = imageDir
	}

	// Extract SSH key path
	if sshKeyPath, ok := config["ssh_key_path"].(string); ok {
		e.sshKeyPath = sshKeyPath
	}

	// Extract SSH port
	if sshPort, ok := config["ssh_port"].(float64); ok {
		e.sshPort = int(sshPort)
	} else {
		e.sshPort = 2222 // default
	}

	// Extract memory
	if memory, ok := config["memory"].(string); ok {
		e.memory = memory
	} else {
		e.memory = "2G" // default
	}

	// Extract CPUs
	if cpus, ok := config["cpus"].(float64); ok {
		e.cpus = int(cpus)
	} else {
		e.cpus = 2 // default
	}

	// Validate required fields
	if e.imageDir == "" {
		return fmt.Errorf("image_dir is required")
	}
	if e.sshKeyPath == "" {
		return fmt.Errorf("ssh_key_path is required")
	}

	return nil
}
