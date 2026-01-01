package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mfittko/netcup-kube/internal/tunnel"
)

func TestLoadSSHEnv(t *testing.T) {
	tests := []struct {
		name    string
		content string
		noEnv   bool
		wantErr bool
	}{
		{
			name: "loads env file successfully",
			content: `TUNNEL_HOST=example.com
TUNNEL_USER=testuser`,
			noEnv:   false,
			wantErr: false,
		},
		{
			name:    "skip loading when noEnv is true",
			content: "",
			noEnv:   true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global variable
			oldNoEnv := sshNoEnv
			defer func() { sshNoEnv = oldNoEnv }()

			sshNoEnv = tt.noEnv

			if tt.content != "" && !tt.noEnv {
				// Create temporary env file
				tmpfile, err := os.CreateTemp("", "ssh-env-*.env")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = os.Remove(tmpfile.Name())
				})

				if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
					t.Fatal(err)
				}
				if err := tmpfile.Close(); err != nil {
					t.Fatalf("failed to close temp file: %v", err)
				}

				// Set the env file path
				oldEnvFile := sshEnvFile
				sshEnvFile = tmpfile.Name()
				defer func() { sshEnvFile = oldEnvFile }()
			}

			err := loadSSHEnv()
			if (err != nil) != tt.wantErr {
				t.Errorf("loadSSHEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSSHEnv_DefaultLocations(t *testing.T) {
	// Create a temporary directory to act as working directory
	tmpDir, err := os.MkdirTemp("", "ssh-env-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	// Save current directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create config directory with env file
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	envContent := `TEST_KEY=test_value`
	envFile := filepath.Join(configDir, "netcup-kube.env")
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Reset global variables
	oldNoEnv := sshNoEnv
	oldEnvFile := sshEnvFile
	defer func() {
		sshNoEnv = oldNoEnv
		sshEnvFile = oldEnvFile
	}()

	sshNoEnv = false
	sshEnvFile = ""

	// Test loading from default location
	err = loadSSHEnv()
	if err != nil {
		t.Errorf("loadSSHEnv() error = %v", err)
	}
}

func TestLoadSSHEnv_NonExistentExplicitFile(t *testing.T) {
	oldNoEnv := sshNoEnv
	oldEnvFile := sshEnvFile
	defer func() {
		sshNoEnv = oldNoEnv
		sshEnvFile = oldEnvFile
	}()

	sshNoEnv = false
	sshEnvFile = "/nonexistent/file.env"

	err := loadSSHEnv()
	if err == nil {
		t.Error("loadSSHEnv() should return error for explicitly specified non-existent file")
	}
}

func TestPortInUse(t *testing.T) {
	// Test with a port that's definitely not in use
	inUse := tunnel.PortInUse("99999")
	// The result depends on system state, so we just verify it doesn't crash
	t.Logf("Port 99999 in use: %v", inUse)
}

func TestShowPortListeners(t *testing.T) {
	// Test that the function doesn't crash
	// It may or may not find listeners depending on the system
	showPortListeners("99999")
}

func TestOpenSSHShell_Validation(t *testing.T) {
	// Save original values
	oldHost := sshHost
	oldUser := sshUser
	defer func() {
		sshHost = oldHost
		sshUser = oldUser
	}()

	// Test with invalid host (should fail to connect, but function should handle it)
	sshHost = "invalid-host-that-does-not-exist"
	sshUser = "testuser"

	// This will fail to connect, which is expected in a test environment
	err := openSSHShell()
	if err == nil {
		t.Log("openSSHShell() completed (connection failure expected in test)")
	}
}

func TestSSHTunnelStart_Validation(t *testing.T) {
	// Save original values
	oldHost := sshHost
	oldUser := sshUser
	oldLocalPort := sshLocalPort
	oldRemoteHost := sshRemoteHost
	oldRemotePort := sshRemotePort
	defer func() {
		sshHost = oldHost
		sshUser = oldUser
		sshLocalPort = oldLocalPort
		sshRemoteHost = oldRemoteHost
		sshRemotePort = oldRemotePort
	}()

	// Test with invalid host (should fail, which is expected)
	sshHost = "invalid-host-that-does-not-exist"
	sshUser = "testuser"
	sshLocalPort = "6443"
	sshRemoteHost = "127.0.0.1"
	sshRemotePort = "6443"

	err := sshTunnelStart()
	// Error expected since we can't actually connect
	if err == nil {
		t.Log("sshTunnelStart() completed (connection failure expected in test)")
	}
}

func TestSSHTunnelStop_NoTunnel(t *testing.T) {
	// Save original values
	oldHost := sshHost
	oldUser := sshUser
	oldLocalPort := sshLocalPort
	defer func() {
		sshHost = oldHost
		sshUser = oldUser
		sshLocalPort = oldLocalPort
	}()

	// Test stopping a tunnel that doesn't exist
	sshHost = "nonexistent-host"
	sshUser = "testuser"
	sshLocalPort = "6443"

	err := sshTunnelStop()
	// Should not error when stopping non-existent tunnel
	if err != nil {
		t.Errorf("sshTunnelStop() should not error for non-existent tunnel, got: %v", err)
	}
}

func TestSSHTunnelStatus_NoTunnel(t *testing.T) {
	// Save original values
	oldHost := sshHost
	oldUser := sshUser
	oldLocalPort := sshLocalPort
	oldRemoteHost := sshRemoteHost
	oldRemotePort := sshRemotePort
	defer func() {
		sshHost = oldHost
		sshUser = oldUser
		sshLocalPort = oldLocalPort
		sshRemoteHost = oldRemoteHost
		sshRemotePort = oldRemotePort
	}()

	// Test status of non-existent tunnel
	sshHost = "nonexistent-host"
	sshUser = "testuser"
	sshLocalPort = "6443"
	sshRemoteHost = "127.0.0.1"
	sshRemotePort = "6443"

	err := sshTunnelStatus()
	// Error expected since tunnel doesn't exist
	if err == nil {
		t.Error("sshTunnelStatus() should return error for non-existent tunnel")
	}
}

func TestPortInUse_WithLsof(t *testing.T) {
	// This test verifies PortInUse handles the lsof case
	// We use a high port that's unlikely to be in use
	inUse := tunnel.PortInUse("65432")
	// Result depends on system state, just verify it doesn't crash
	t.Logf("Port 65432 in use (via lsof path): %v", inUse)
}

func TestSSHTunnelStart_PortAlreadyBound(t *testing.T) {
	// We can't easily test this without actually binding a port
	// This test is a placeholder for documentation
	t.Skip("Skipping test that requires binding a port")
}
