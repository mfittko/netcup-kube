package remote

import (
	"os"
	"os/exec"
	"strings"
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

func TestEnsureRootAccess_AlreadyHasKey(t *testing.T) {
	fc := &fakeClient{testConnErr: nil}
	if err := ensureRootAccess(fc, "example.com", "/tmp/key.pub"); err != nil {
		t.Fatalf("ensureRootAccess error: %v", err)
	}
}

func TestEnsureRootAccess_NoSshpass(t *testing.T) {
	oldLook := lookPath
	defer func() { lookPath = oldLook }()
	lookPath = func(file string) (string, error) { return "", exec.ErrNotFound }

	fc := &fakeClient{testConnErr: exec.ErrNotFound}
	err := ensureRootAccess(fc, "example.com", "/tmp/key.pub")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "ssh-copy-id") {
		t.Fatalf("expected ssh-copy-id hint, got: %v", err)
	}
}

func TestEnsureRootAccess_SshpassButMissingRootPass(t *testing.T) {
	oldLook := lookPath
	defer func() { lookPath = oldLook }()
	lookPath = func(file string) (string, error) { return "/usr/bin/sshpass", nil }
	os.Unsetenv("ROOT_PASS")

	fc := &fakeClient{testConnErr: exec.ErrNotFound}
	err := ensureRootAccess(fc, "example.com", "/tmp/key.pub")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "ROOT_PASS") {
		t.Fatalf("expected ROOT_PASS error, got: %v", err)
	}
}

func TestEnsureRootAccess_SshpassWithRootPass_UnsetsEnv(t *testing.T) {
	oldExec := execCommand
	oldLook := lookPath
	defer func() { execCommand = oldExec; lookPath = oldLook }()

	// sshpass+ssh-copy-id succeeds
	execCommand = func(_ string, _ ...string) *exec.Cmd { return exec.Command("true") }
	lookPath = func(file string) (string, error) { return "/usr/bin/sshpass", nil }
	os.Setenv("ROOT_PASS", "secret")
	t.Cleanup(func() { os.Unsetenv("ROOT_PASS") })

	fc := &fakeClient{testConnErr: exec.ErrNotFound}
	if err := ensureRootAccess(fc, "example.com", "/tmp/key.pub"); err != nil {
		t.Fatalf("ensureRootAccess error: %v", err)
	}
	if os.Getenv("ROOT_PASS") != "" {
		t.Fatalf("expected ROOT_PASS to be unset")
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
