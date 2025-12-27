package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mfittko/netcup-kube/internal/tunnel"
)

func TestFetchKubeconfig(t *testing.T) {
	// Create temporary environment file
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "test.env")

	// Write test env file content
	content := `MGMT_HOST=test.example.com
MGMT_USER=testuser
`
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	localKubeconfig := filepath.Join(tmpDir, "k3s.yaml")

	// This test will fail at the scp step (expected), but tests the env file loading
	err := fetchKubeconfig(envFile, localKubeconfig, tmpDir)
	t.Logf("fetchKubeconfig returned (scp failure expected): %v", err)
}

func TestTunnelManager(t *testing.T) {
	// Test the tunnel manager creation
	mgr := tunnel.New("testuser", "test.example.com", "6443", "127.0.0.1", "6443")

	if mgr == nil {
		t.Fatal("tunnel.New() returned nil")
	}

	// Check that we can get the control socket path
	socket := mgr.GetControlSocket()
	if socket == "" {
		t.Error("GetControlSocket() returned empty string")
	}

	t.Logf("Control socket: %s", socket)
}

func TestEnsureTunnelRunning_NoEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "nonexistent.env")

	// Should not error if env file doesn't exist (uses defaults)
	err := ensureTunnelRunning(envFile, tmpDir)
	if err != nil {
		t.Logf("ensureTunnelRunning() returned: %v (expected when env values missing)", err)
	}
}
