package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSSHClient(t *testing.T) {
	client := NewSSHClient("example.com", "testuser")

	if client.Host != "example.com" {
		t.Errorf("Host = %s, want example.com", client.Host)
	}
	if client.User != "testuser" {
		t.Errorf("User = %s, want testuser", client.User)
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "'hello'",
		},
		{
			name:  "string with spaces",
			input: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "string with single quote",
			input: "it's",
			want:  "'it'\\''s'",
		},
		{
			name:  "string with multiple single quotes",
			input: "it's a 'test'",
			want:  "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "string with special chars",
			input: "test; rm -rf /",
			want:  "'test; rm -rf /'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellEscape(tt.input)
			if got != tt.want {
				t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildRemoteCommand(t *testing.T) {
	client := &SSHClient{
		Host: "example.com",
		User: "testuser",
	}

	tests := []struct {
		name    string
		command string
		args    []string
		env     map[string]string
		want    string
	}{
		{
			name:    "simple command",
			command: "ls",
			args:    []string{"-la"},
			env:     nil,
			want:    "'ls' '-la'",
		},
		{
			name:    "command with env",
			command: "echo",
			args:    []string{"test"},
			env:     map[string]string{"VAR": "value"},
			want:    "'VAR'='value' 'echo' 'test'",
		},
		{
			name:    "command with special characters",
			command: "echo",
			args:    []string{"hello; rm -rf /"},
			env:     nil,
			want:    "'echo' 'hello; rm -rf /'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.buildRemoteCommand(tt.command, tt.args, tt.env)

			// Check that all expected parts are in the result
			if !strings.Contains(got, shellEscape(tt.command)) {
				t.Errorf("buildRemoteCommand result missing command %q", tt.command)
			}
			for _, arg := range tt.args {
				if !strings.Contains(got, shellEscape(arg)) {
					t.Errorf("buildRemoteCommand result missing arg %q", arg)
				}
			}
		})
	}
}

func TestFindPublicKey(t *testing.T) {
	// Create a temporary directory with a fake key pair
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("Failed to create .ssh directory: %v", err)
	}

	// Create both private and public key
	privKeyPath := filepath.Join(sshDir, "id_ed25519")
	pubKeyPath := filepath.Join(sshDir, "id_ed25519.pub")

	if err := os.WriteFile(privKeyPath, []byte("fake private key"), 0600); err != nil {
		t.Fatalf("Failed to create private key: %v", err)
	}
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAA... test@localhost"), 0600); err != nil {
		t.Fatalf("Failed to create public key: %v", err)
	}

	// Save and restore HOME
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpDir)

	client := NewSSHClient("example.com", "testuser")
	if client.IdentityFile == "" {
		t.Error("NewSSHClient should find identity file in HOME/.ssh")
	} else if !strings.Contains(client.IdentityFile, "id_ed25519") {
		t.Errorf("Expected identity file to contain id_ed25519, got %s", client.IdentityFile)
	}
}
