package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfirmationRequired tests that destructive operations require CONFIRM=true in non-interactive mode
// Note: These tests require root privileges and are primarily tested via Docker integration tests.
// This test serves as documentation of the expected behavior.
func TestConfirmationRequired(t *testing.T) {
	// Skip if not running as root (these commands require root even in DRY_RUN mode)
	if os.Geteuid() != 0 {
		t.Skip("Skipping confirmation tests: requires root privileges (run via 'make test' for Docker-based testing)")
	}
	
	// This test validates that the dns command (which overwrites Caddy config)
	// requires CONFIRM=true when running non-interactively
	
	// Skip if we can't find the binary
	binPath := filepath.Join("..", "..", "bin", "netcup-kube")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skip("netcup-kube binary not found, run 'make build-go' first")
	}

	tests := []struct {
		name        string
		command     []string
		env         map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name:        "dns command without CONFIRM should fail in non-interactive mode",
			command:     []string{"dns", "--type", "edge-http", "--domains", "test.example.com"},
			env:         map[string]string{
				"DRY_RUN": "true",
				// No CONFIRM=true, and no TTY (test runs non-interactively)
			},
			wantErr:     true,
			errContains: "CONFIRM=true", // Should mention CONFIRM in error
		},
		{
			name:        "dns command with CONFIRM=true should succeed",
			command:     []string{"dns", "--type", "edge-http", "--domains", "test.example.com"},
			env:         map[string]string{
				"DRY_RUN": "true",
				"CONFIRM": "true",
			},
			wantErr: false,
		},
		{
			name:        "bootstrap in DRY_RUN mode should not require CONFIRM",
			command:     []string{"bootstrap"},
			env:         map[string]string{
				"DRY_RUN":      "true",
				"EDGE_PROXY":   "none",
				"DASH_ENABLE":  "false",
				"ENABLE_UFW":   "false",
			},
			wantErr: false,
		},
		{
			name:        "join in DRY_RUN mode should not require CONFIRM",
			command:     []string{"join"},
			env:         map[string]string{
				"MODE":       "join",
				"DRY_RUN":    "true",
				"SERVER_URL": "https://192.168.1.1:6443",
				"TOKEN":      "dummytoken",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binPath, tt.command...)
			
			// Set up environment
			env := os.Environ()
			for k, v := range tt.env {
				env = append(env, k+"="+v)
			}
			cmd.Env = env
			
			// Capture output
			output, err := cmd.CombinedOutput()
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but command succeeded. Output: %s", output)
				}
				if tt.errContains != "" && !strings.Contains(string(output), tt.errContains) {
					t.Errorf("Error output should contain %q, got: %s", tt.errContains, output)
				}
			} else {
				if err != nil {
					t.Errorf("Expected success but got error: %v. Output: %s", err, output)
				}
			}
		})
	}
}

// TestDryRunBehavior tests DRY_RUN mode behavior
// Note: These tests require root privileges and are primarily tested via Docker integration tests.
func TestDryRunBehavior(t *testing.T) {
	// Skip if not running as root
	if os.Geteuid() != 0 {
		t.Skip("Skipping DRY_RUN tests: requires root privileges (run via 'make test' for Docker-based testing)")
	}
	
	binPath := filepath.Join("..", "..", "bin", "netcup-kube")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skip("netcup-kube binary not found, run 'make build-go' first")
	}

	tests := []struct {
		name    string
		command []string
		env     map[string]string
		wantErr bool
	}{
		{
			name:    "bootstrap with DRY_RUN should succeed without system changes",
			command: []string{"bootstrap"},
			env: map[string]string{
				"DRY_RUN":     "true",
				"EDGE_PROXY":  "none",
				"DASH_ENABLE": "false",
				"ENABLE_UFW":  "false",
			},
			wantErr: false,
		},
		{
			name:    "join with DRY_RUN should succeed without system changes",
			command: []string{"join"},
			env: map[string]string{
				"MODE":       "join",
				"DRY_RUN":    "true",
				"SERVER_URL": "https://192.168.1.1:6443",
				"TOKEN":      "dummytoken",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binPath, tt.command...)
			
			env := os.Environ()
			for k, v := range tt.env {
				env = append(env, k+"="+v)
			}
			cmd.Env = env
			
			output, err := cmd.CombinedOutput()
			
			if tt.wantErr && err == nil {
				t.Errorf("Expected error but command succeeded. Output: %s", output)
			} else if !tt.wantErr && err != nil {
				t.Errorf("Expected success but got error: %v. Output: %s", err, output)
			}
		})
	}
}

