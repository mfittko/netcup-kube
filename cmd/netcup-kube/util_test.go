package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetTunnelControlSocket(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		host      string
		localPort string
		want      string
	}{
		{
			name:      "basic case",
			user:      "ops",
			host:      "example.com",
			localPort: "6443",
			want:      "netcup-kube-tunnel-ops_example.com-6443.ctl",
		},
		{
			name:      "special characters in host",
			user:      "admin",
			host:      "192.168.1.1",
			localPort: "8080",
			want:      "netcup-kube-tunnel-admin_192.168.1.1-8080.ctl",
		},
		{
			name:      "user with @ symbol",
			user:      "user@domain",
			host:      "host.com",
			localPort: "22",
			want:      "netcup-kube-tunnel-user_domain_host.com-22.ctl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTunnelControlSocket(tt.user, tt.host, tt.localPort)
			// Check that the socket path contains the expected filename
			if !filepath.IsAbs(got) {
				t.Errorf("getTunnelControlSocket() should return absolute path, got %v", got)
			}
			base := filepath.Base(got)
			if base != tt.want {
				t.Errorf("getTunnelControlSocket() filename = %v, want %v", base, tt.want)
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantKeys map[string]string
		wantErr  bool
	}{
		{
			name: "simple key-value pairs",
			content: `KEY1=value1
KEY2=value2`,
			wantKeys: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
		{
			name: "with comments and empty lines",
			content: `# This is a comment
KEY1=value1

KEY2=value2
# Another comment`,
			wantKeys: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
		{
			name: "with quotes",
			content: `KEY1="quoted value"
KEY2='single quoted'`,
			wantKeys: map[string]string{
				"KEY1": "quoted value",
				"KEY2": "single quoted",
			},
			wantErr: false,
		},
		{
			name: "with whitespace",
			content: `  KEY1  =  value1  
KEY2=value2  `,
			wantKeys: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
		{
			name:     "empty file",
			content:  "",
			wantKeys: map[string]string{},
			wantErr:  false,
		},
		{
			name: "malformed lines ignored",
			content: `KEY1=value1
malformed line without equals
KEY2=value2`,
			wantKeys: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpfile, err := os.CreateTemp("", "test-env-*.env")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatal(err)
			}

			got, err := loadEnvFile(tmpfile.Name())
			if (err != nil) != tt.wantErr {
				t.Errorf("loadEnvFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.wantKeys) {
				t.Errorf("loadEnvFile() returned %d keys, want %d", len(got), len(tt.wantKeys))
			}

			for key, wantVal := range tt.wantKeys {
				gotVal, ok := got[key]
				if !ok {
					t.Errorf("loadEnvFile() missing key %q", key)
					continue
				}
				if gotVal != wantVal {
					t.Errorf("loadEnvFile() key %q = %q, want %q", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestLoadEnvFile_NonExistent(t *testing.T) {
	_, err := loadEnvFile("/nonexistent/path/to/file.env")
	if err == nil {
		t.Error("loadEnvFile() should return error for non-existent file")
	}
}

func TestFindProjectRoot(t *testing.T) {
	// Save current directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// This test validates that findProjectRoot can find the project root
	// The function looks for scripts/main.sh, so we need to be in a location where it exists
	// In test environment, we might be in cmd/netcup-kube or project root
	root, err := findProjectRoot()
	
	// If we get an error, try changing to project root first
	if err != nil {
		// Try going up directories to find project root
		testDir := oldWd
		for i := 0; i < 3; i++ {
			testDir = filepath.Dir(testDir)
			os.Chdir(testDir)
			root, err = findProjectRoot()
			if err == nil {
				break
			}
		}
		// Restore directory
		os.Chdir(oldWd)
		
		if err != nil {
			t.Skipf("Skipping test - not in a position to find project root: %v", err)
			return
		}
	}

	// Verify go.mod exists in the root
	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		t.Errorf("go.mod not found at expected root %s", root)
	}

	// Verify it's an absolute path
	if !filepath.IsAbs(root) {
		t.Errorf("findProjectRoot() should return absolute path, got %v", root)
	}
}
