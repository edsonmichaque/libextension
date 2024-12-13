package pluginkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bytes"

	"github.com/tetratelabs/wazero"
	wasip1 "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmExecutor implements the Executor interface for WebAssembly plugins
type WasmExecutor struct {
	pluginDir string
	runtime   wazero.Runtime
}

// NewWasmExecutor creates a new WasmExecutor instance
func NewWasmExecutor(pluginDir string) (*WasmExecutor, error) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)

	// Initialize WASI
	if _, err := wasip1.Instantiate(ctx, r); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("failed to initialize WASI: %w", err)
	}

	return &WasmExecutor{
		pluginDir: pluginDir,
		runtime:   r,
	}, nil
}

// Execute runs a WASM plugin with the given options
func (e *WasmExecutor) Execute(ctx context.Context, pluginName string, opts ExecuteOptions) (*ExecuteResult, error) {
	if pluginName == "" {
		return nil, fmt.Errorf("plugin name cannot be empty")
	}

	startTime := time.Now()

	// Construct the full path to the WASM plugin
	pluginPath := filepath.Join(e.pluginDir, pluginName, pluginName+".wasm")

	// Read the WASM binary
	wasmBytes, err := os.ReadFile(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	// Compile the WASM module
	module, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}
	defer func() {
		_ = module.Close(ctx)
	}()

	// Configure the WASM instance with stdio
	var stdout, stderr bytes.Buffer
	config := wazero.NewModuleConfig().
		WithArgs(opts.Args...).
		//WithEnv(e.convertEnvToSlice(opts.Environment)).
		WithStdout(&stdout).
		WithStderr(&stderr)
	if opts.WorkingDir != "" {
		config = config.WithFSConfig(wazero.NewFSConfig().
			WithDirMount(opts.WorkingDir, "/"))
	}

	// Instantiate the module
	instance, err := e.runtime.InstantiateModule(ctx, module, config)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}
	defer instance.Close(ctx)

	// Call the _start function (main entry point)
	exitCode := 0
	if _, err := instance.ExportedFunction("_start").Call(ctx); err != nil {
		fmt.Fprintf(&stderr, "Error executing WASM module: %v\nType: %T\n", err, err)
		exitCode = 1
	}

	endTime := time.Now()

	// Get stdout and stderr as bytes
	return &ExecuteResult{
		ExitCode:    exitCode,
		Stdout:      stdout.Bytes(),
		Stderr:      stderr.Bytes(),
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    endTime.Sub(startTime),
		CommandLine: pluginPath,
		WorkingDir:  opts.WorkingDir,
		Environment: opts.Environment,
		PID:         0, // WASM doesn't have a traditional PID
		Success:     exitCode == 0,
	}, nil
}

// Helper function to convert environment map to slice
func (e *WasmExecutor) convertEnvToSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// Configure applies the provided configuration map
func (e *WasmExecutor) Configure(config map[string]interface{}) error {
	// Extract plugin directory
	if pluginDir, ok := config["plugin_dir"].(string); ok {
		e.pluginDir = pluginDir
	}

	// Validate required fields
	if e.pluginDir == "" {
		return fmt.Errorf("plugin_dir is required")
	}

	// Reinitialize runtime if needed
	if e.runtime == nil {
		ctx := context.Background()
		r := wazero.NewRuntime(ctx)

		// Initialize WASI
		if _, err := wasip1.Instantiate(ctx, r); err != nil {
			r.Close(ctx)
			return fmt.Errorf("failed to initialize WASI: %w", err)
		}
		e.runtime = r
	}

	return nil
}
