package pluginkit

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// FindAsset finds and filters assets based on naming conventions and returns the best match
func FindAsset(
	prefix string,
	name string,
	version string,
	goos string,
	arch string,
	getAssets func() []string,
) (assetName string, runtime string, err error) {
	log.Printf("[FindAsset] prefix: %s, name: %s, version: %s, goos: %s, arch: %s", prefix, name, version, goos, arch)

	// Filter valid assets
	var validAssets []string

	for _, asset := range getAssets() {
		log.Printf("[FindAsset] asset: %s", asset)

		// Skip if wrong prefix or has unwanted suffix
		if !strings.HasPrefix(asset, name) ||
			strings.HasSuffix(asset, ".sha256") ||
			strings.HasSuffix(asset, ".asc") ||
			strings.HasSuffix(asset, ".sig") {
			continue
		}

		validAssets = append(validAssets, asset)
	}

	log.Printf("[FindAsset] validAssets: %v", validAssets)

	if len(validAssets) == 0 {
		return "", "", fmt.Errorf("no valid assets found for %s-%s", prefix, name)
	}

	// Try to find assets in order of specificity
	patterns := []string{
		// Most specific (with platform)
		fmt.Sprintf("%s-%s-%s-%s", name, version, goos, arch),
		// Version only
		fmt.Sprintf("%s-%s", name, version),
		// Least specific
		name,
	}

	// Add extensions to each pattern
	extensions := []string{"", ".exe", ".zip", ".tar.gz", ".tgz", ".wasm"}
	for _, pattern := range patterns {
		for _, ext := range extensions {
			for _, asset := range validAssets {
				log.Printf("pattern: %s, ext: %s, asset: %s", pattern, ext, asset)

				if matched, _ := filepath.Match(pattern+ext, asset); matched {
					runtime := "exec"
					if strings.Contains(ext, "wasm") {
						runtime = "wasm"
					}

					return asset, runtime, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("no matching assets found")
}

// Filter returns a filtered list of matching plugin artifact names
func Filter(prefix, name, version string, getAssetNames func() []string) []string {
	// Define supported platforms and extensions
	platforms := struct {
		oses   []string
		arches []string
	}{
		oses:   []string{"linux", "windows", "macos"},
		arches: []string{"amd64", "386", "arm", "arm64"},
	}

	extensions := map[string][]string{
		"native":  {"", ".exe"},
		"archive": {".zip", ".tar.gz", ".tgz"},
		"wasm":    {".wasm", ".wasm.zip", ".wasm.tar.gz", ".wasm.tgz"},
	}

	// Generate base patterns
	patterns := []string{
		fmt.Sprintf("%s-%s", prefix, name),
		fmt.Sprintf("%s-%s-v%s", prefix, name, version),
	}

	// Add platform-specific patterns
	for _, os := range platforms.oses {
		for _, arch := range platforms.arches {
			patterns = append(patterns, fmt.Sprintf("%s-%s-v%s-%s-%s", prefix, name, version, os, arch))
		}
	}

	// Create a set of valid asset names for efficient matching
	validAssets := make(map[string]bool)

	for _, pattern := range patterns {
		for _, extList := range extensions {
			for _, ext := range extList {
				validAssets[pattern+ext] = true
			}
		}
	}

	assetNames := getAssetNames()

	result := make([]string, 0, len(assetNames))

	for _, asset := range assetNames {
		if validAssets[asset] {
			result = append(result, asset)
		}
	}

	return result
}
