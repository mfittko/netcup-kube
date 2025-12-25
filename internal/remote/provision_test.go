package remote

import (
	"testing"
)

func TestBuildProvisionScript(t *testing.T) {
	script := buildProvisionScript("testuser", "ssh-ed25519 AAAA... test@localhost", "https://github.com/test/repo.git")

	// Check that script contains expected placeholders replaced
	if !contains(script, "testuser") {
		t.Error("Script should contain username")
	}
	if !contains(script, "ssh-ed25519 AAAA... test@localhost") {
		t.Error("Script should contain public key")
	}
	if !contains(script, "https://github.com/test/repo.git") {
		t.Error("Script should contain repo URL")
	}

	// Check for essential commands
	essentialCommands := []string{
		"apt-get update",
		"apt-get install",
		"sudo",
		"git",
		"adduser",
		"usermod -aG sudo",
		"git clone",
	}

	for _, cmd := range essentialCommands {
		if !contains(script, cmd) {
			t.Errorf("Script should contain command: %s", cmd)
		}
	}
}

func TestProvision_MissingPubKey(t *testing.T) {
	// Create a config with a non-existent public key
	cfg := NewConfig()
	cfg.Host = "test.example.com"
	cfg.User = "testuser"
	cfg.PubKeyPath = "/nonexistent/key.pub"

	// This should fail because the public key doesn't exist
	err := Provision(cfg)
	if err == nil {
		t.Error("Provision should fail with non-existent public key")
	}
	if err != nil && !contains(err.Error(), "public key not found") {
		t.Errorf("Expected 'public key not found' error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && findInString(s, substr)
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
