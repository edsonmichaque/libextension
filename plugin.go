package pluginkit

import (
	"io"
	"time"
)

const (
	Prefix = "dashboard-"
)

// Plugin represents a CLI plugin
type Plugin struct {
	Info     Info      // Plugin metadata (contains Name, Version, Description)
	Path     string    // File system path to the plugin
	FileName string    // Name of the plugin file
	Store    Store     // Reference to the store that manages this plugin
	Runtime  Runtime   // Reference for the runtime that executes this plugin
	Reader   io.Reader // Reader containing the plugin data
	ModTime  time.Time // Modification time of the file
	Size     int64     // Size of the file in bytes
}

// Info represents metadata about a plugin
type Info struct {
	Name        string            `json:"name"`
	FileName    string            `json:"filename"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Store       string            `json:"store"`              // Identifier for the store (github, gitlab, local, etc)
	Runtime     string            `json:"runtime"`            // Identifier for the runtime (local, docker, etc)
	Metadata    map[string]string `json:"metadata,omitempty"` // Additional store/runner specific metadata
	Status      string            `json:"status,omitempty"`   // Status of the plugin (enabled, disabled)
	Content     interface{}       `json:"content,omitempty"`  // Content of the plugin file
}
