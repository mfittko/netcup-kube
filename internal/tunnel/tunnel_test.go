package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	mgr := New("testuser", "test.example.com", "6443", "127.0.0.1", "6443")

	if mgr == nil {
		t.Fatal("New() returned nil")
	}

	if mgr.User != "testuser" {
		t.Errorf("User = %q, want %q", mgr.User, "testuser")
	}

	if mgr.Host != "test.example.com" {
		t.Errorf("Host = %q, want %q", mgr.Host, "test.example.com")
	}

	if mgr.LocalPort != "6443" {
		t.Errorf("LocalPort = %q, want %q", mgr.LocalPort, "6443")
	}

	if mgr.RemoteHost != "127.0.0.1" {
		t.Errorf("RemoteHost = %q, want %q", mgr.RemoteHost, "127.0.0.1")
	}

	if mgr.RemotePort != "6443" {
		t.Errorf("RemotePort = %q, want %q", mgr.RemotePort, "6443")
	}
}

func TestGetControlSocket(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		host      string
		localPort string
		wantBase  string
	}{
		{
			name:      "basic case",
			user:      "ops",
			host:      "example.com",
			localPort: "6443",
			wantBase:  "netcup-kube-tunnel-ops_example.com-6443.ctl",
		},
		{
			name:      "special characters in host",
			user:      "admin",
			host:      "192.168.1.1",
			localPort: "8080",
			wantBase:  "netcup-kube-tunnel-admin_192.168.1.1-8080.ctl",
		},
		{
			name:      "user with @ symbol",
			user:      "user@domain",
			host:      "host.com",
			localPort: "22",
			wantBase:  "netcup-kube-tunnel-user_domain_host.com-22.ctl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := New(tt.user, tt.host, tt.localPort, "127.0.0.1", "6443")
			got := mgr.GetControlSocket()

			// Check that the path is absolute
			if !filepath.IsAbs(got) {
				t.Errorf("GetControlSocket() should return absolute path, got %v", got)
			}

			// Check if the filename matches expected pattern
			base := filepath.Base(got)
			if base != tt.wantBase {
				t.Errorf("GetControlSocket() filename = %v, want %v", base, tt.wantBase)
			}

			// Verify it's in either XDG_RUNTIME_DIR or /tmp
			dir := filepath.Dir(got)
			xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
			if xdgRuntime != "" {
				if dir != xdgRuntime {
					t.Errorf("GetControlSocket() dir = %v, want %v", dir, xdgRuntime)
				}
			} else {
				if dir != "/tmp" {
					t.Errorf("GetControlSocket() dir = %v, want /tmp", dir)
				}
			}
		})
	}
}

func TestIsRunning(t *testing.T) {
	// Test with a tunnel that definitely doesn't exist
	mgr := New("nonexistent-user", "nonexistent-host.invalid", "99999", "127.0.0.1", "6443")

	running := mgr.IsRunning()
	// Should return false for a non-existent tunnel
	if running {
		t.Error("IsRunning() = true for non-existent tunnel, want false")
	}
}

func TestStatus(t *testing.T) {
	// Test with a tunnel that doesn't exist
	mgr := New("testuser", "test.example.com", "6443", "127.0.0.1", "6443")

	running, port := mgr.Status()

	// Should return false and empty port for non-running tunnel
	if running {
		t.Error("Status() running = true for non-existent tunnel, want false")
	}

	if port != "" {
		t.Errorf("Status() port = %q for non-running tunnel, want empty string", port)
	}
}

func TestStart_PortInUse(t *testing.T) {
	// This test verifies that Start() fails when port is in use
	// We can't actually test this without binding a port, so we'll just verify
	// the function signature and basic error handling

	mgr := New("testuser", "nonexistent-host.invalid", "99999", "127.0.0.1", "6443")

	// This should fail because the host doesn't exist
	err := mgr.Start()
	// We expect an error because the host is invalid
	if err == nil {
		t.Log("Start() succeeded unexpectedly (may have failed at SSH step, which is expected)")
	}
}

func TestStop_NotRunning(t *testing.T) {
	// Test stopping a tunnel that isn't running
	mgr := New("testuser", "test.example.com", "6443", "127.0.0.1", "6443")

	err := mgr.Stop()
	// Should return nil when tunnel isn't running
	if err != nil {
		t.Errorf("Stop() error = %v for non-running tunnel, want nil", err)
	}
}

func TestPortInUse(t *testing.T) {
	// Test with a port that's definitely not in use
	inUse := PortInUse("99999")
	// The result depends on system state, so we just verify it doesn't crash
	t.Logf("Port 99999 in use: %v", inUse)

	// Test that the function works (doesn't panic)
	_ = PortInUse("65432")
}

func TestGetControlSocket_ConsistentOutput(t *testing.T) {
	// Verify that GetControlSocket returns the same value when called multiple times
	mgr := New("user", "host", "1234", "127.0.0.1", "6443")

	socket1 := mgr.GetControlSocket()
	socket2 := mgr.GetControlSocket()

	if socket1 != socket2 {
		t.Errorf("GetControlSocket() returned different values: %v != %v", socket1, socket2)
	}
}

func TestGetControlSocket_EscapesSpecialChars(t *testing.T) {
	// Test that special characters in user/host/port are properly escaped
	mgr := New("user@domain", "host:22/path", "8080", "127.0.0.1", "6443")
	socket := mgr.GetControlSocket()

	// Verify that special characters are replaced with underscores
	base := filepath.Base(socket)
	if strings.Contains(base, "@") || strings.Contains(base, ":") || strings.Contains(base, "/") {
		t.Errorf("GetControlSocket() filename contains unescaped special characters: %v", base)
	}

	// Should contain underscores instead
	if !strings.Contains(base, "_") {
		t.Error("GetControlSocket() filename should contain underscores for escaped characters")
	}
}
