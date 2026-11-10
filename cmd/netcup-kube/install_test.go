package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfittko/netcup-kube/internal/tunnel"
)

func TestInstallDomainFollowup(t *testing.T) {
	if _, err := os.Stat("/etc/caddy/Caddyfile"); err == nil {
		t.Skip("automatic remote domains only run on a local installation")
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	oldCfg, oldExecutor, oldEnvFile, oldDry, oldWrite := cfg, scriptExecutor, envFile, dryRun, dryRunWriteFiles
	t.Cleanup(func() {
		_ = os.Chdir(oldDir)
		cfg, scriptExecutor, envFile, dryRun, dryRunWriteFiles = oldCfg, oldExecutor, oldEnvFile, oldDry, oldWrite
	})
	for _, mode := range []string{"flag", "environment", "env-file", "live"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			for path, content := range map[string]string{
				"scripts/main.sh":                    "#!/bin/sh\nexit 0\n",
				"scripts/recipes/fixture/install.sh": "#!/bin/sh\nprintf '%s' \"$DRY_RUN\" > \"$FIXTURE_RECIPE\"\n",
				"bin/netcup-kube":                    "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FIXTURE_DOMAINS\"\n",
				"config/fixture.env":                 "DRY_RUN=" + map[bool]string{true: "true", false: "false"}[mode == "env-file"] + "\n",
			} {
				full := filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Setenv("KUBECONFIG", serverKubeconfigPath)
			t.Setenv("FIXTURE_RECIPE", filepath.Join(dir, "recipe"))
			t.Setenv("FIXTURE_DOMAINS", filepath.Join(dir, "domains"))
			t.Setenv("DRY_RUN", map[bool]string{true: "true", false: "false"}[mode == "environment"])
			envFile, dryRun, dryRunWriteFiles = "", false, false
			args := []string{"fixture", "--host", "proxy.example.com", "--admin-host=admin.example.com"}
			if mode == "flag" {
				args = append(args, "--dry-run")
			}
			if mode == "env-file" {
				args = append(args, "--env-file", filepath.Join(dir, "config/fixture.env"))
			}
			if err := rootCmd.PersistentPreRunE(installCmd, args); err != nil {
				t.Fatal(err)
			}
			if err := installCmd.RunE(installCmd, args); err != nil {
				t.Fatal(err)
			}
			recipe, err := os.ReadFile(filepath.Join(dir, "recipe"))
			wantDry := mode != "live"
			if err != nil || (string(recipe) == "true") != wantDry {
				t.Fatal("recipe did not receive resolved dry-run configuration")
			}
			domains, err := os.ReadFile(filepath.Join(dir, "domains"))
			if wantDry {
				if !os.IsNotExist(err) {
					t.Fatal("dry-run invoked automatic domain mutation")
				}
			} else if err != nil || strings.Count(string(domains), "--add-domains") != 2 || !strings.Contains(string(domains), "proxy.example.com") || !strings.Contains(string(domains), "admin.example.com") {
				t.Fatal("live domain updates were lost")
			}
		})
	}
}

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

func TestResolveTunnelTarget(t *testing.T) {
	tests := []struct {
		name                     string
		env                      map[string]string
		wantUser, wantHost, port string
	}{
		{"tunnel overrides mgmt", map[string]string{"TUNNEL_HOST": "t.host", "TUNNEL_USER": "tuser", "MGMT_HOST": "m.host", "MGMT_USER": "muser", "MGMT_IP": "10.0.0.1"}, "tuser", "t.host", "6443"},
		{"fallback to mgmt", map[string]string{"MGMT_HOST": "m.host", "MGMT_USER": "muser", "MGMT_IP": "10.0.0.1"}, "muser", "m.host", "6443"},
		{"fallback to mgmt ip and ops", map[string]string{"MGMT_IP": "10.0.0.1"}, "ops", "10.0.0.1", "6443"},
		{"local port honored", map[string]string{"MGMT_HOST": "m.host", "TUNNEL_LOCAL_PORT": "16443"}, "ops", "m.host", "16443"},
		{"empty env", map[string]string{}, "ops", "", "6443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, host, port := resolveTunnelTarget(tt.env)
			if user != tt.wantUser || host != tt.wantHost || port != tt.port {
				t.Errorf("resolveTunnelTarget() = (%q, %q, %q), want (%q, %q, %q)", user, host, port, tt.wantUser, tt.wantHost, tt.port)
			}
		})
	}
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

func TestManualRemoteDNSAddDomainsCommand_UsesSeparator(t *testing.T) {
	host := "llm-proxy.example.com"
	cmd := manualRemoteDNSAddDomainsCommand(host)

	if !strings.Contains(cmd, " remote run --no-tty -- dns ") {
		t.Fatalf("expected manual command to include '--' separator: %q", cmd)
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
		{
			name:     "missing-host-value-does-not-consume-next-flag",
			args:     []string{"--host", "--admin-host", "llm-proxy-admin.example.com"},
			expHost:  "",
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
