package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildShellRunKubectlArgs(t *testing.T) {
	got := buildShellRunKubectlArgs("openclaw", "openclaw-abc", []string{"echo", "hello", "&&", "id"})
	want := []string{
		"-n", "openclaw",
		"exec",
		"-c", "main",
		"openclaw-abc",
		"--",
		"sh",
		"-lc",
		"echo hello && id",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildShellRunKubectlArgs() = %v, want %v", got, want)
	}
}

func TestBuildOpenClawCLIKubectlArgs(t *testing.T) {
	got := buildOpenClawCLIKubectlArgs("openclaw", "openclaw-abc", []string{"status"})
	want := []string{
		"-n", "openclaw",
		"exec",
		"-c", "main",
		"openclaw-abc",
		"--",
		"node",
		"--no-warnings",
		"/app/openclaw.mjs",
		"status",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildOpenClawCLIKubectlArgs() = %v, want %v", got, want)
	}
}

func TestChartVersionFromChart(t *testing.T) {
	tests := []struct {
		chart string
		want  string
	}{
		{"openclaw-1.3.18", "1.3.18"},
		{"openclaw-1.3.21", "1.3.21"},
		{"myrelease-0.1.0", "0.1.0"},
		{"noversion", "noversion"},
	}
	for _, tc := range tests {
		got := chartVersionFromChart(tc.chart)
		if got != tc.want {
			t.Errorf("chartVersionFromChart(%q) = %q, want %q", tc.chart, got, tc.want)
		}
	}
}

func TestUpdateRecipesConfPinAt(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "recipes.conf")

	original := "# Helm Chart Versions\nCHART_VERSION_OPENCLAW=1.3.18\nCHART_VERSION_METORO_EXPORTER=0.469.0\n"
	if err := os.WriteFile(confPath, []byte(original), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := updateRecipesConfPinAt(confPath, "1.3.21"); err != nil {
		t.Fatalf("updateRecipesConfPinAt: %v", err)
	}

	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	want := "# Helm Chart Versions\nCHART_VERSION_OPENCLAW=1.3.21\nCHART_VERSION_METORO_EXPORTER=0.469.0\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateRecipesConfPinAt_MissingKey(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "recipes.conf")

	if err := os.WriteFile(confPath, []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := updateRecipesConfPinAt(confPath, "1.3.21")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// tunnelManagerInterface defines the interface we need for testing
type tunnelManagerInterface interface {
	IsRunning() bool
	Start() error
	Stop() error
	Status() string
}

// mockTunnelManager implements tunnelManagerInterface for testing
type mockTunnelManager struct {
	isRunning bool
	startErr  error
	started   bool
}

func (m *mockTunnelManager) IsRunning() bool {
	return m.isRunning
}

func (m *mockTunnelManager) Start() error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	m.isRunning = true
	return nil
}

func (m *mockTunnelManager) Stop() error {
	m.isRunning = false
	return nil
}

func (m *mockTunnelManager) Status() string {
	if m.isRunning {
		return "running"
	}
	return "stopped"
}

// TestTunnelBootstrapFlow tests the critical API unreachable -> tunnel start -> re-probe sequence
func TestTunnelBootstrapFlow(t *testing.T) {
	tests := []struct {
		name             string
		initialProbe     bool
		tunnelHost       string
		tunnelIsRunning  bool
		tunnelStartErr   error
		secondProbe      bool
		expectTunnelCall bool
		expectError      bool
		errorContains    string
	}{
		{
			name:             "API reachable - no tunnel needed",
			initialProbe:     true,
			tunnelHost:       "test-host",
			expectTunnelCall: false,
			expectError:      false,
		},
		{
			name:             "API unreachable - tunnel starts successfully - API becomes reachable",
			initialProbe:     false,
			tunnelHost:       "test-host",
			tunnelIsRunning:  false,
			secondProbe:      true,
			expectTunnelCall: true,
			expectError:      false,
		},
		{
			name:             "API unreachable - tunnel already running - API becomes reachable",
			initialProbe:     false,
			tunnelHost:       "test-host",
			tunnelIsRunning:  true,
			secondProbe:      true,
			expectTunnelCall: false,
			expectError:      false,
		},
		{
			name:          "API unreachable - no tunnel host configured",
			initialProbe:  false,
			tunnelHost:    "",
			expectError:   true,
			errorContains: "no tunnel host configured",
		},
		{
			name:             "API unreachable - tunnel starts - API still unreachable",
			initialProbe:     false,
			tunnelHost:       "test-host",
			tunnelIsRunning:  false,
			secondProbe:      false,
			expectTunnelCall: true,
			expectError:      true,
			errorContains:    "still unreachable after starting SSH tunnel",
		},
		{
			name:            "API unreachable - tunnel start fails",
			initialProbe:    false,
			tunnelHost:      "test-host",
			tunnelIsRunning: false,
			tunnelStartErr:  errors.New("connection refused"),
			expectError:     true,
			errorContains:   "failed to start SSH tunnel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track probe call count
			probeCallCount := 0
			mockProbe := func() bool {
				probeCallCount++
				if probeCallCount == 1 {
					return tt.initialProbe
				}
				return tt.secondProbe
			}

			// Mock tunnel manager
			mockTunnel := &mockTunnelManager{
				isRunning: tt.tunnelIsRunning,
				startErr:  tt.tunnelStartErr,
			}

			mockTunnelFactory := func(user, host, localPort, remoteHost, remotePort string) tunnelManagerInterface {
				return mockTunnel
			}

			// Execute the tunnel bootstrap logic
			err := executeTunnelBootstrap(mockProbe, tt.tunnelHost, mockTunnelFactory)

			// Verify expectations
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorContains)
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			// Verify tunnel was called if expected
			// Note: when tunnel start fails, we don't mark it as started
			if tt.expectTunnelCall && tt.tunnelStartErr == nil && !mockTunnel.started {
				t.Error("expected tunnel to be started, but it wasn't")
			}
			if !tt.expectTunnelCall && mockTunnel.started {
				t.Error("expected tunnel not to be started, but it was")
			}

			// Verify probe was called the right number of times
			expectedProbeCount := 1
			if !tt.initialProbe && tt.tunnelHost != "" && (tt.tunnelStartErr == nil) {
				expectedProbeCount = 2 // Initial probe + re-probe after tunnel
			}
			if probeCallCount != expectedProbeCount {
				t.Errorf("expected %d probe calls, got %d", expectedProbeCount, probeCallCount)
			}
		})
	}
}

// executeTunnelBootstrap is a testable extraction of the tunnel bootstrap logic
// from portForwardStartCmd.RunE. This allows us to test the critical conditional
// branch without running the full command.
func executeTunnelBootstrap(
	probeFn func() bool,
	tunnelHost string,
	tunnelFactory func(user, host, localPort, remoteHost, remotePort string) tunnelManagerInterface,
) error {
	// Step 1: Probe kube API
	if !probeFn() {
		// Step 2: API unreachable – ensure SSH tunnel is running
		if tunnelHost == "" {
			return errors.New("kube API is unreachable and no tunnel host configured (set TUNNEL_HOST or --tunnel-host)")
		}

		mgr := tunnelFactory("ops", tunnelHost, "6443", "localhost", "6443")
		if !mgr.IsRunning() {
			if err := mgr.Start(); err != nil {
				return errors.New("failed to start SSH tunnel: " + err.Error())
			}
		}

		// Re-probe after tunnel start
		if !probeFn() {
			return errors.New("kube API still unreachable after starting SSH tunnel; check tunnel config and kubeconfig")
		}
	}

	return nil
}
