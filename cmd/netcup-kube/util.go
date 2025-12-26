package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// findProjectRoot locates the netcup-kube project root directory.
// It searches in the current directory, parent directories (if in bin/), and relative to the executable.
// Returns an error if scripts/main.sh cannot be found.
func findProjectRoot() (string, error) {
	// Try current directory first
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if scripts/main.sh exists in current directory
	if _, err := os.Stat(filepath.Join(currentDir, "scripts", "main.sh")); err == nil {
		return currentDir, nil
	}

	// If running from bin/, go up one level
	if filepath.Base(currentDir) == "bin" {
		parent := filepath.Dir(currentDir)
		if _, err := os.Stat(filepath.Join(parent, "scripts", "main.sh")); err == nil {
			return parent, nil
		}
	}

	// Try relative to the executable
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		projectRoot := filepath.Dir(exeDir)
		if _, err := os.Stat(filepath.Join(projectRoot, "scripts", "main.sh")); err == nil {
			return projectRoot, nil
		}
	}

	return "", fmt.Errorf("could not locate project root: scripts/main.sh not found in current directory or expected locations")
}
