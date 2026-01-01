package remote

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBuildProvisionScript(t *testing.T) {
	script := buildProvisionScript("testuser", "ssh-ed25519 AAAA... test@localhost", "https://github.com/test/repo.git", "example.com")

	// Check that script contains expected placeholders replaced
	if !strings.Contains(script, "testuser") {
		t.Error("Script should contain username")
	}
	if !strings.Contains(script, "ssh-ed25519 AAAA... test@localhost") {
		t.Error("Script should contain public key")
	}
	if !strings.Contains(script, "https://github.com/test/repo.git") {
		t.Error("Script should contain repo URL")
	}
	if !strings.Contains(script, "example.com") {
		t.Error("Script should contain host")
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
		if !strings.Contains(script, cmd) {
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
	if err != nil && !strings.Contains(err.Error(), "public key not found") {
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
	_ = os.Unsetenv("ROOT_PASS")

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
	if err := os.Setenv("ROOT_PASS", "secret"); err != nil {
		t.Fatalf("Setenv(ROOT_PASS) failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ROOT_PASS") })

	fc := &fakeClient{testConnErr: exec.ErrNotFound}
	if err := ensureRootAccess(fc, "example.com", "/tmp/key.pub"); err != nil {
		t.Fatalf("ensureRootAccess error: %v", err)
	}
	if os.Getenv("ROOT_PASS") != "" {
		t.Fatalf("expected ROOT_PASS to be unset")
	}
}

func TestProvision_PubKeyWithTrailingNewline(t *testing.T) {
	// Create a temporary pubkey file with trailing newline (typical SSH key format)
	tmpDir := t.TempDir()
	pubKeyPath := tmpDir + "/id_test.pub"
	pubKeyContent := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@localhost\n"
	if err := os.WriteFile(pubKeyPath, []byte(pubKeyContent), 0600); err != nil {
		t.Fatalf("Failed to create test pubkey: %v", err)
	}

	cfg := NewConfig()
	cfg.Host = "test.example.com"
	cfg.User = "testuser"
	cfg.PubKeyPath = pubKeyPath

	// This should fail at SSH connection, but the important thing is that
	// pubkey trimming doesn't cause the script to fail with syntax errors
	err := Provision(cfg)
	// We expect it to fail at SSH connection, not at pubkey validation
	if err != nil && strings.Contains(err.Error(), "multiple lines") {
		t.Errorf("Should not fail with 'multiple lines' error for trailing newline, got: %v", err)
	}
}

func TestProvision_EmptyPubKey(t *testing.T) {
	tmpDir := t.TempDir()
	pubKeyPath := tmpDir + "/empty.pub"
	if err := os.WriteFile(pubKeyPath, []byte("  \n  \n"), 0600); err != nil {
		t.Fatalf("Failed to create test pubkey: %v", err)
	}

	cfg := NewConfig()
	cfg.Host = "test.example.com"
	cfg.User = "testuser"
	cfg.PubKeyPath = pubKeyPath

	err := Provision(cfg)
	if err == nil {
		t.Error("Provision should fail with empty public key")
	}
	if err != nil && !strings.Contains(err.Error(), "empty") {
		t.Errorf("Expected 'empty' error, got: %v", err)
	}
}

func TestProvision_MultilinePubKey(t *testing.T) {
	tmpDir := t.TempDir()
	pubKeyPath := tmpDir + "/multiline.pub"
	if err := os.WriteFile(pubKeyPath, []byte("line1\nline2\n"), 0600); err != nil {
		t.Fatalf("Failed to create test pubkey: %v", err)
	}

	cfg := NewConfig()
	cfg.Host = "test.example.com"
	cfg.User = "testuser"
	cfg.PubKeyPath = pubKeyPath

	err := Provision(cfg)
	if err == nil {
		t.Error("Provision should fail with multiline public key")
	}
	if err != nil && !strings.Contains(err.Error(), "multiple lines") {
		t.Errorf("Expected 'multiple lines' error, got: %v", err)
	}
}
