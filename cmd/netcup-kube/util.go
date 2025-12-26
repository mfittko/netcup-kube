package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// loadEnvFile loads key=value pairs from an environment file
func loadEnvFile(path string) (map[string]string, error) {
	env := make(map[string]string)

	data, err := os.ReadFile(path)
	if err != nil {
		return env, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			env[key] = value
		}
	}

	return env, nil
}
