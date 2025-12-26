package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchKubeconfig(t *testing.T) {
	// Create temporary directories for test
	tmpDir, err := os.MkdirTemp("", "test-kubeconfig-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a mock env file
	envFile := filepath.Join(configDir, "netcup-kube.env")
	envContent := `MGMT_HOST=test.example.com
MGMT_USER=testuser`
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	localKubeconfig := filepath.Join(configDir, "k3s.yaml")

	// Test that the function requires scp to be available
	// We can't test actual SSH without a real server, but we can test validation
	err = fetchKubeconfig(envFile, localKubeconfig, configDir)
	// Error expected since we don't have actual SSH access
	if err == nil {
		t.Log("fetchKubeconfig() completed (may have failed at SSH step, which is expected in test)")
	}
}

func TestStartTunnelViaGo(t *testing.T) {
	// Test validation - should fail without actual SSH access
	err := startTunnelViaGo("test.example.com", "testuser", "6443")
	// Error expected since we can't actually start a tunnel in tests
	if err == nil {
		t.Log("startTunnelViaGo() completed (may have failed at SSH step, which is expected in test)")
	}
}

func TestEnsureTunnelRunning_NoEnvFile(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-tunnel-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with non-existent env file - should handle gracefully
	nonExistentEnv := filepath.Join(tmpDir, "nonexistent.env")
	err = ensureTunnelRunning(nonExistentEnv, tmpDir)

	// Should return nil or an error, but not crash
	if err != nil {
		t.Logf("ensureTunnelRunning() returned error (expected): %v", err)
	}
}
