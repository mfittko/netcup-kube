package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()
	
	if cfg.User != defaultUser {
		t.Errorf("User = %s, want %s", cfg.User, defaultUser)
	}
	if cfg.RepoURL != defaultRepoURL {
		t.Errorf("RepoURL = %s, want %s", cfg.RepoURL, defaultRepoURL)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	// Create a test config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.env")
	
	configContent := `MGMT_HOST=example.com
MGMT_IP=192.168.1.1
MGMT_USER=ops
DEFAULT_USER=admin
`
	
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	tests := []struct {
		name         string
		initialHost  string
		initialUser  string
		configPath   string
		wantHost     string
		wantUser     string
	}{
		{
			name:        "load host from MGMT_HOST",
			initialHost: "",
			initialUser: defaultUser,
			configPath:  configPath,
			wantHost:    "example.com",
			wantUser:    "ops",
		},
		{
			name:        "preserve existing host",
			initialHost: "custom.com",
			initialUser: defaultUser,
			configPath:  configPath,
			wantHost:    "custom.com",
			wantUser:    "ops",
		},
		{
			name:        "preserve existing user",
			initialHost: "",
			initialUser: "customuser",
			configPath:  configPath,
			wantHost:    "example.com",
			wantUser:    "customuser",
		},
		{
			name:        "non-existent config",
			initialHost: "test.com",
			initialUser: "testuser",
			configPath:  "/nonexistent/config.env",
			wantHost:    "test.com",
			wantUser:    "testuser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.Host = tt.initialHost
			cfg.User = tt.initialUser
			
			err := cfg.LoadConfigFromEnv(tt.configPath)
			if err != nil {
				t.Errorf("LoadConfigFromEnv() error = %v", err)
				return
			}
			
			if cfg.Host != tt.wantHost {
				t.Errorf("Host = %s, want %s", cfg.Host, tt.wantHost)
			}
			if cfg.User != tt.wantUser {
				t.Errorf("User = %s, want %s", cfg.User, tt.wantUser)
			}
		})
	}

	// Covers MGMT_IP fallback and ${DEFAULT_USER} expansion (MGMT_USER=${DEFAULT_USER})
	ipOnlyPath := filepath.Join(tmpDir, "ip-only.env")
	ipOnlyContent := `DEFAULT_USER=ops
MGMT_HOST=
MGMT_IP=1.2.3.4
MGMT_USER=${DEFAULT_USER}
`
	if err := os.WriteFile(ipOnlyPath, []byte(ipOnlyContent), 0644); err != nil {
		t.Fatalf("Failed to create ip-only config: %v", err)
	}

	cfg := NewConfig()
	if err := cfg.LoadConfigFromEnv(ipOnlyPath); err != nil {
		t.Fatalf("LoadConfigFromEnv(ip-only) error = %v", err)
	}
	if cfg.Host != "1.2.3.4" {
		t.Fatalf("Host = %s, want %s", cfg.Host, "1.2.3.4")
	}
	if cfg.User != "ops" {
		t.Fatalf("User = %s, want %s", cfg.User, "ops")
	}
}

func TestGetPubKey(t *testing.T) {
	// Create a temporary directory with a fake public key
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("Failed to create .ssh directory: %v", err)
	}

	pubKeyPath := filepath.Join(sshDir, "id_ed25519.pub")
	if err := os.WriteFile(pubKeyPath, []byte("ssh-ed25519 AAAA... test@localhost"), 0600); err != nil {
		t.Fatalf("Failed to create public key: %v", err)
	}

	// Save and restore HOME
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpDir)

	tests := []struct {
		name          string
		initialPubKey string
		wantErr       bool
	}{
		{
			name:          "find default key",
			initialPubKey: "",
			wantErr:       false,
		},
		{
			name:          "use specified key",
			initialPubKey: pubKeyPath,
			wantErr:       false,
		},
		{
			name:          "non-existent specified key",
			initialPubKey: "/nonexistent/key.pub",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.PubKeyPath = tt.initialPubKey
			
			key, err := cfg.GetPubKey()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPubKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if !tt.wantErr && key == "" {
				t.Error("GetPubKey() returned empty key")
			}
		})
	}
}

func TestGetRemoteRepoDir(t *testing.T) {
	cfg := NewConfig()
	cfg.User = "testuser"
	
	want := "/home/testuser/netcup-kube"
	got := cfg.GetRemoteRepoDir()
	
	if got != want {
		t.Errorf("GetRemoteRepoDir() = %s, want %s", got, want)
	}
}

func TestGetRemoteBinPath(t *testing.T) {
	cfg := NewConfig()
	cfg.User = "testuser"
	
	want := "/home/testuser/netcup-kube/bin/netcup-kube"
	got := cfg.GetRemoteBinPath()
	
	if got != want {
		t.Errorf("GetRemoteBinPath() = %s, want %s", got, want)
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "exists.txt")
	
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: existingFile,
			want: true,
		},
		{
			name: "non-existent file",
			path: filepath.Join(tmpDir, "nonexistent.txt"),
			want: false,
		},
		{
			name: "directory",
			path: tmpDir,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileExists(tt.path)
			if got != tt.want {
				t.Errorf("fileExists(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
