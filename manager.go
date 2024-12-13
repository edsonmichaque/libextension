package extension

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/andybalholm/brotli"
	"github.com/go-logr/logr"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4"
)

// Manager implements the Store interface
type Manager struct {
	pluginDir string
	store     Store
	mu        sync.RWMutex
	logger    logr.Logger
}

// NewManager creates a new plugin manager instance
func NewManager(pluginDir string, store Store, logger logr.Logger) *Manager {
	return &Manager{
		pluginDir: pluginDir,
		store:     store,
		logger:    logger.WithName("plugin-manager"),
	}
}

// Install handles plugin installation
func (m *Manager) Install(ctx context.Context, name, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger := m.logger.WithValues("plugin", name, "version", version)
	logger.V(1).Info("starting plugin installation")

	// Early validation
	if err := ctx.Err(); err != nil {
		logger.Error(err, "context cancelled before installation")
		return fmt.Errorf("context cancelled before installation: %w", err)
	}

	if m.pluginDir == "" {
		logger.Error(nil, "no valid plugin directory found")
		return fmt.Errorf("no valid plugin directory found")
	}

	pluginDir := filepath.Join(m.pluginDir, name)
	logger = logger.WithValues("dir", pluginDir)

	// Check if plugin is already installed
	if _, err := os.Stat(pluginDir); err == nil {
		logger.Error(nil, "plugin is already installed")
		return fmt.Errorf("plugin %s is already installed", name)
	}

	// Setup cleanup in case of failure
	var success bool
	defer func() {
		if !success {
			os.RemoveAll(pluginDir)
		}
	}()

	logger.V(1).Info("os.MkdirAll(pluginDir, 0755)")

	// Create the plugin directory
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	logger.V(1).Info("m.store.Fetch(ctx, name, version)")

	// Fetch plugin from store
	info, err := m.store.Fetch(ctx, name, version)
	if err != nil {
		return fmt.Errorf("failed to fetch plugin: %w", err)
	}

	logger.V(1).Info("writePluginFiles(ctx, pluginDir, info)")
	// Write plugin data
	if err := writePluginFiles(ctx, pluginDir, info); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	// Create metadata
	info.Version = version
	info.Status = "enabled"
	info.Metadata = map[string]string{
		"installed": time.Now().Format(time.RFC3339),
	}

	// Save metadata
	metadataBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	logger.V(1).Info("metadataPath := filepath.Join(pluginDir, \"metadata.json\")")

	metadataPath := filepath.Join(pluginDir, "metadata.json")
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	success = true

	return nil
}

// Uninstall removes a plugin from the filesystem
func (m *Manager) Uninstall(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled during uninstall: %w", err)
	}

	pluginDir := filepath.Join(m.pluginDir, name)

	// Check if plugin directory exists
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %s not found in plugin directory", name)
	}

	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("failed to remove plugin directory: %w", err)
	}

	return nil
}

// Enable activates a plugin
func (m *Manager) Enable(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before enabling plugin: %w", err)
	}

	pluginDir := filepath.Join(m.pluginDir, name)
	metadataPath := filepath.Join(pluginDir, "metadata.json")

	var info Info

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	info.Status = "enabled"

	metadataBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// Disable deactivates a plugin
func (m *Manager) Disable(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before disabling plugin: %w", err)
	}

	pluginDir := filepath.Join(m.pluginDir, name)
	metadataPath := filepath.Join(pluginDir, "metadata.json")

	var info Info

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	info.Status = "disabled"

	metadataBytes, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// List returns information about all installed plugins
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before listing plugins: %w", err)
	}

	var plugins []Info

	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return plugins, nil
		}

		return nil, fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadataPath := filepath.Join(m.pluginDir, entry.Name(), "metadata.json")

		data, err := os.ReadFile(metadataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("failed to read metadata for plugin %s: %w", entry.Name(), err)
		}

		var info Info
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, fmt.Errorf("failed to parse metadata for plugin %s: %w", entry.Name(), err)
		}

		plugins = append(plugins, info)
	}

	return plugins, nil
}

// Search returns available plugins from the store with installation status
func (m *Manager) Search(ctx context.Context, searchOptions SearchOptions) ([]Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before search: %w", err)
	}

	// Get available plugins from store using Search
	available, err := m.store.Search(ctx, searchOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to search available plugins: %w", err)
	}

	// Get installed plugins
	installed, err := m.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list installed plugins: %w", err)
	}

	// Mark installed plugins and their versions
	installedMap := make(map[string]string)
	for _, plugin := range installed {
		installedMap[plugin.Name] = plugin.Version
	}

	// Update available plugins with installation status
	for i := range available {
		if version, ok := installedMap[available[i].Name]; ok {
			available[i].Status = "installed"
			available[i].Metadata["installed_version"] = version
		} else {
			available[i].Status = "available"
		}
	}

	return available, nil
}

func (m *Manager) Upgrade(ctx context.Context, name string, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before upgrade: %w", err)
	}

	// Check if plugin exists
	pluginDir := filepath.Join(m.pluginDir, name)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %s is not installed", name)
	}

	// Read current metadata
	currentInfo, err := readMetadata(filepath.Join(pluginDir, "metadata.json"))
	if err != nil {
		return fmt.Errorf("failed to read current plugin metadata: %w", err)
	}

	// Skip if already at requested version
	if currentInfo.Version == version {
		return fmt.Errorf("plugin %s is already at version %s", name, version)
	}

	// Create temporary upgrade directory
	tmpDir := pluginDir + ".upgrade"
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temporary upgrade directory: %w", err)
	}

	// Fetch new version
	newInfo, err := m.store.Fetch(ctx, name, version)
	if err != nil {
		return fmt.Errorf("failed to fetch plugin upgrade: %w", err)
	}

	// Write new plugin files
	if err := writePluginFiles(ctx, tmpDir, newInfo); err != nil {
		return fmt.Errorf("failed to write upgraded plugin files: %w", err)
	}

	// Update metadata
	newInfo.Version = version
	newInfo.Status = currentInfo.Status
	newInfo.Metadata = map[string]string{
		"installed":        time.Now().Format(time.RFC3339),
		"upgraded_from":    currentInfo.Version,
		"previous_install": currentInfo.Metadata["installed"],
	}

	// Write new metadata
	metadataBytes, err := json.MarshalIndent(newInfo, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "metadata.json"), metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Atomic swap
	backupDir := pluginDir + ".backup"
	if err := os.Rename(pluginDir, backupDir); err != nil {
		return fmt.Errorf("failed to backup existing plugin: %w", err)
	}

	if err := os.Rename(tmpDir, pluginDir); err != nil {
		// Attempt to restore backup
		os.Rename(backupDir, pluginDir)
		return fmt.Errorf("failed to install upgrade: %w", err)
	}

	// Clean up backup
	os.RemoveAll(backupDir)

	return nil
}

func (m *Manager) Fetch(ctx context.Context, name string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metadataPath := filepath.Join(m.pluginDir, name, "metadata.json")

	return readMetadata(metadataPath)
}

// Helper function for reading metadata
func readMetadata(path string) (*Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// Helper function for writing plugin files
func writePluginFiles(ctx context.Context, dir string, info *Info) error {
	// Create plugin-specific directory
	plugindir := filepath.Join(dir, info.Name)
	log.Printf("[Manager.Install] plugindir: %s", plugindir)

	if err := os.MkdirAll(plugindir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin-specific directory: %w", err)
	}

	// Prepare content for type detection
	var contentType string

	switch v := info.Content.(type) {
	case string:
		log.Printf("[Manager.Install] v: %s", v)
		contentType = http.DetectContentType([]byte(v))
	case []byte:
		log.Printf("[Manager.Install] v: %s", v)
		contentType = http.DetectContentType(v)
	case io.Reader:
		log.Println("[Manager.Install] v is io.Reader")

		// Read just enough for content type detection
		sniffBuf := make([]byte, 512)

		n, err := v.Read(sniffBuf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read content for type detection: %w", err)
		}

		contentType = http.DetectContentType(sniffBuf[:n])
		log.Printf("[Manager.Install] contentType: %s", contentType)

		// Try to seek back if possible
		if seeker, ok := v.(io.Seeker); ok {
			log.Println("[Manager.Install] seeker is io.Seeker")

			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek back after type detection: %w", err)
			}
		} else {
			log.Println("[Manager.Install] seeker is not io.Seeker")
			// If we can't seek, prepend the read bytes to a new reader
			info.Content = io.MultiReader(bytes.NewReader(sniffBuf[:n]), v)
		}
	default:
		return fmt.Errorf("unsupported plugin data type: %T", info.Content)
	}

	// Convert content to io.Reader if needed
	var reader io.Reader
	switch v := info.Content.(type) {
	case string:
		reader = strings.NewReader(v)
	case []byte:
		reader = bytes.NewReader(v)
	case io.Reader:
		reader = v
	default:
		return fmt.Errorf("unsupported content type: %T", info.Content)
	}

	binPath := filepath.Join(plugindir, info.FileName)

	binFile, err := os.OpenFile(binPath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("failed to create plugin file: %w", err)
	}
	defer binFile.Close()

	// Handle different content types
	processors, ok := fileProcessorMap[contentType]
	if !ok {
		log.Println("[Manager.Install] extracting other")

		if _, err := io.Copy(binFile, reader); err != nil {
			return fmt.Errorf("failed to write plugin data: %w", err)
		}

		return nil
	}

	log.Printf("[Manager.Install] extracting %s", contentType)

	// Process through the chain of processors
	reader, err = processFile(ctx, reader, plugindir, processors...)
	if err != nil {
		return fmt.Errorf("failed to process file: %w", err)
	}

	// Close if the final reader implements io.Closer
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	if _, err := io.Copy(binFile, reader); err != nil {
		return fmt.Errorf("failed to write plugin data: %w", err)
	}

	return nil
}

// extractGz decompresses a gzipped reader and returns a new reader
func extractGz(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	return gr, nil
}

// extractTar extracts a tar archive from a reader to the destination directory
func extractTar(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		// Sanitize file path to prevent directory traversal
		target := filepath.Join(destDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, destDir) {
			return nil, fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return nil, fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return nil, fmt.Errorf("failed to write file: %w", err)
			}

			f.Close()
		}
	}

	return nil, nil
}

func extractZip(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	// Create a temporary file to store the zip content
	tmpFile, err := os.CreateTemp("", "plugin-*.zip")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Copy zip content to temporary file
	if _, err := io.Copy(tmpFile, r); err != nil {
		return nil, fmt.Errorf("failed to write zip content: %w", err)
	}

	// Get zip file size
	info, err := tmpFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get zip file info: %w", err)
	}

	// Open zip reader
	zipReader, err := zip.NewReader(tmpFile, info.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, file := range zipReader.File {
		// Sanitize file path to prevent directory traversal
		target := filepath.Join(destDir, filepath.Clean(file.Name))
		if !strings.HasPrefix(target, destDir) {
			return nil, fmt.Errorf("invalid zip path: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}

			continue
		}

		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}

		// Create and write file
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, file.Mode())
		if err != nil {
			return nil, fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := file.Open()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to open zip file: %w", err)
		}

		_, err = io.Copy(f, rc)
		rc.Close()
		f.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return nil, nil
}

// extractZstd decompresses a zstd compressed reader and returns a new reader
func extractZstd(_ context.Context, r io.Reader) (io.Reader, error) {
	decoder, err := zstd.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}

	return decoder, nil
}

// extractXz decompresses an xz compressed reader and returns a new reader
func extractXz(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	decoder, err := xz.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create xz reader: %w", err)
	}

	return decoder, nil
}

// extractBzip2 decompresses a bzip2 compressed reader and returns a new reader
func extractBzip2(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	decoder := bzip2.NewReader(r)
	if decoder == nil {
		return nil, fmt.Errorf("failed to create bzip2 reader")
	}

	return decoder, nil
}

// extractLz4 decompresses an LZ4 compressed reader and returns a new reader
func extractLz4(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	decoder := lz4.NewReader(r)
	if decoder == nil {
		return nil, fmt.Errorf("failed to create lz4 reader")
	}

	return decoder, nil
}

// extractBrotli decompresses a Brotli compressed reader and returns a new reader
func extractBrotli(_ context.Context, r io.Reader, destDir string) (io.Reader, error) {
	decoder := brotli.NewReader(r)
	if decoder == nil {
		return nil, fmt.Errorf("failed to create brotli reader")
	}

	return decoder, nil
}

type fileProcessor func(ctx context.Context, r io.Reader, destDir string) (io.Reader, error)

var fileProcessorMap map[string][]fileProcessor = map[string][]fileProcessor{
	"application/gzip":     {extractGz, extractTar},
	"application/x-gzip":   {extractGz, extractTar},
	"application/zip":      {extractZip},
	"application/x-xz":     {extractXz, extractTar},
	"application/x-bzip2":  {extractBzip2, extractTar},
	"application/x-lz4":    {extractLz4, extractTar},
	"application/x-brotli": {extractBrotli, extractTar},
}

func processFile(ctx context.Context, r io.Reader, destDir string, processors ...fileProcessor) (io.Reader, error) {
	var reader io.Reader = r

	for _, process := range processors {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("processing cancelled: %w", err)
		}

		processed, err := process(ctx, reader, destDir)
		if err != nil {
			return nil, fmt.Errorf("processing failed: %w", err)
		}

		if processed == nil {
			return nil, nil // Early return if processor indicates completion (e.g., after extraction)
		}

		reader = processed
	}

	return reader, nil
}
