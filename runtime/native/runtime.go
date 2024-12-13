package native

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NativeExecutor implements the Executor interface
type NativeExecutor struct {
	pluginDir string
}

// NewExecutor creates a new DefaultExecutor instance
func NewExecutor(pluginDir string) *NativeExecutor {
	return &NativeExecutor{
		pluginDir: pluginDir,
	}
}

// Execute runs a plugin with the given options
func (e *NativeExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	// Construct the full path to the plugin executable
	pluginPath := filepath.Join(e.pluginDir, pluginName, pluginName)

	startTime := time.Now()

	// Create command with context
	cmd := exec.CommandContext(ctx, pluginPath, opts.Args...)

	// Set working directory if specified
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}

	// Set environment variables
	if opts.Environment != nil {
		env := make([]string, 0, len(opts.Environment))
		for k, v := range opts.Environment {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Read output
	stdoutData, err := readAll(stdout)
	if err != nil {
		return nil, err
	}
	stderrData, err := readAll(stderr)
	if err != nil {
		return nil, err
	}

	// Wait for completion
	err = cmd.Wait()
	endTime := time.Now()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &ExecuteResult{
		ExitCode:    exitCode,
		Stdout:      stdoutData,
		Stderr:      stderrData,
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    endTime.Sub(startTime),
		CommandLine: pluginPath + " " + strings.Join(opts.Args, " "),
		WorkingDir:  opts.WorkingDir,
		Environment: opts.Environment,
		PID:         cmd.Process.Pid,
		Success:     exitCode == 0,
	}, nil
}

// Helper function to read all data from a pipe
func readAll(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	return buf.Bytes(), err
}
