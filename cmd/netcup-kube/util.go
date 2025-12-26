package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfittko/netcup-kube/internal/config"
)

// getTunnelControlSocket returns the path to the SSH ControlMaster socket for the tunnel
func getTunnelControlSocket(user, host, localPort string) string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}

	key := fmt.Sprintf("%s@%s-%s", user, host, localPort)
	key = strings.ReplaceAll(key, "@", "_")
	key = strings.ReplaceAll(key, ":", "_")
	key = strings.ReplaceAll(key, "/", "_")

	return filepath.Join(base, fmt.Sprintf("netcup-kube-tunnel-%s.ctl", key))
}

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

// loadEnvFile loads key=value pairs from an environment file.
// This is a thin wrapper around config.LoadEnvFileToMap for backward compatibility.
func loadEnvFile(path string) (map[string]string, error) {
	return config.LoadEnvFileToMap(path)
}
