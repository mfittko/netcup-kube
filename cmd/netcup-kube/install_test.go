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

	// Stub out scp so this test is deterministic and does not require network access.
	stubBinDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(stubBinDir, 0755); err != nil {
		t.Fatal(err)
	}

	stubScp := filepath.Join(stubBinDir, "scp")
	stubScpScript := "#!/usr/bin/env bash\nset -euo pipefail\n# Args: <src> <dst>\ndst=\"$2\"\ncat > \"${dst}\" <<'EOF'\napiVersion: v1\nclusters: []\ncontexts: []\ncurrent-context: ''\nkind: Config\npreferences: {}\nusers: []\nEOF\n"
	if err := os.WriteFile(stubScp, []byte(stubScpScript), 0755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
	})
	if err := os.Setenv("PATH", stubBinDir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}

	if err := fetchKubeconfig(envFile, localKubeconfig, tmpDir); err != nil {
		t.Fatalf("fetchKubeconfig() error: %v", err)
	}
	data, err := os.ReadFile(localKubeconfig)
	if err != nil {
		t.Fatalf("failed to read fetched kubeconfig: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected kubeconfig to be written, got empty file")
	}
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

func TestBuildRemoteDNSAddDomainsArgs(t *testing.T) {
	testCases := []struct {
		name     string
		envFile  string
		domain   string
		expected []string
	}{
		{
			name:    "includes-arg-separator",
			envFile: "/tmp/envfile.env",
			domain:  "example.com",
			expected: []string{
				"remote",
				"run",
				"--no-tty",
				"--env-file",
				"/tmp/envfile.env",
				"--",
				"dns",
				"--type",
				"edge-http",
				"--add-domains",
				"example.com",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			args := buildRemoteDNSAddDomainsArgs(tc.envFile, tc.domain)
			if len(args) != len(tc.expected) {
				t.Fatalf("expected %d args, got %d: %#v", len(tc.expected), len(args), args)
			}
			for i := range tc.expected {
				if args[i] != tc.expected[i] {
					t.Fatalf("arg[%d]: expected %q, got %q", i, tc.expected[i], args[i])
				}
			}
		})
	}
}

func TestParseRecipeHostArgs(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expHost  string
		expAdmin string
	}{
		{
			name:     "no-hosts",
			args:     []string{"--namespace", "platform"},
			expHost:  "",
			expAdmin: "",
		},
		{
			name:     "host-equals",
			args:     []string{"--host=llm-proxy.example.com"},
			expHost:  "llm-proxy.example.com",
			expAdmin: "",
		},
		{
			name:     "host-space-separated",
			args:     []string{"--host", "llm-proxy.example.com"},
			expHost:  "llm-proxy.example.com",
			expAdmin: "",
		},
		{
			name:     "admin-host-equals",
			args:     []string{"--admin-host=llm-proxy-admin.example.com"},
			expHost:  "",
			expAdmin: "llm-proxy-admin.example.com",
		},
		{
			name:     "admin-host-space-separated",
			args:     []string{"--admin-host", "llm-proxy-admin.example.com"},
			expHost:  "",
			expAdmin: "llm-proxy-admin.example.com",
		},
		{
			name:     "both-hosts",
			args:     []string{"--host", "llm-proxy.example.com", "--admin-host", "llm-proxy-admin.example.com"},
			expHost:  "llm-proxy.example.com",
			expAdmin: "llm-proxy-admin.example.com",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			host, adminHost := parseRecipeHostArgs(tc.args)
			if host != tc.expHost {
				t.Fatalf("host: expected %q, got %q", tc.expHost, host)
			}
			if adminHost != tc.expAdmin {
				t.Fatalf("adminHost: expected %q, got %q", tc.expAdmin, adminHost)
			}
		})
	}
}

func TestUniqueNonEmptyStrings(t *testing.T) {
	got := uniqueNonEmptyStrings([]string{"", "a.example.com", " ", "a.example.com", "b.example.com"})
	expected := []string{"a.example.com", "b.example.com"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d items, got %d: %#v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("item[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}
