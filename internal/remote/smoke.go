package remote

import (
	"fmt"
	"os"
)

// Smoke runs a safe DRY_RUN smoke test on the remote management node
func Smoke(cfg *Config, opts GitOptions, projectRoot string) error {
	if cfg == nil || cfg.Host == "" {
		return fmt.Errorf("missing host")
	}

	client := NewSSHClient(cfg.Host, cfg.User)
	return smokeWithClient(client, cfg, opts, projectRoot)
}

func smokeWithClient(client Client, cfg *Config, opts GitOptions, projectRoot string) error {

	// Ensure user access and repo exists
	if err := client.TestConnection(); err != nil {
		return fmt.Errorf("SSH connection failed. Run 'netcup-kube remote provision' first")
	}

	// Build and upload the binary first
	if err := RemoteBuildAndUpload(client, cfg, projectRoot, opts); err != nil {
		return err
	}

	// Create temporary env files for smoke test
	tmpEnv, err := createSmokeEnvFile()
	if err != nil {
		return fmt.Errorf("failed to create smoke env file: %w", err)
	}
	defer os.Remove(tmpEnv)

	tmpEnvJoin, err := createSmokeJoinEnvFile()
	if err != nil {
		return fmt.Errorf("failed to create smoke join env file: %w", err)
	}
	defer os.Remove(tmpEnvJoin)

	fmt.Printf("[local] Running DRY_RUN smoke test on %s@%s (non-interactive)\n", cfg.User, cfg.Host)

	// Run smoke tests with --no-tty so they don't block on prompts
	tests := []struct {
		name    string
		envFile string
		args    []string
	}{
		{
			name:    "help",
			envFile: tmpEnv,
			args:    []string{"--help"},
		},
		{
			name:    "dns help",
			envFile: tmpEnv,
			args:    []string{"dns", "--help"},
		},
		{
			name:    "pair help",
			envFile: tmpEnv,
			args:    []string{"pair", "--help"},
		},
		{
			name:    "bootstrap",
			envFile: tmpEnv,
			args:    []string{"bootstrap"},
		},
		{
			name:    "join",
			envFile: tmpEnvJoin,
			args:    []string{"join"},
		},
	}

	for _, test := range tests {
		fmt.Printf("[smoke] Running: %s\n", test.name)
		
		runOpts := RunOptions{
			ForceTTY: false,
			EnvFile:  test.envFile,
			Args:     test.args,
		}

		if err := runWithClient(client, cfg, runOpts); err != nil {
			return fmt.Errorf("smoke test '%s' failed: %w", test.name, err)
		}
	}

	fmt.Println("[local] Smoke test complete (DRY_RUN).")
	return nil
}

func createSmokeEnvFile() (string, error) {
	content := `DRY_RUN=true
DRY_RUN_WRITE_FILES=false
ENABLE_UFW=false
EDGE_PROXY=none
DASH_ENABLE=false
CONFIRM=true
`

	tmpFile, err := os.CreateTemp("", "netcup-kube-smoke-*.env")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

func createSmokeJoinEnvFile() (string, error) {
	content := `DRY_RUN=true
DRY_RUN_WRITE_FILES=false
ENABLE_UFW=false
EDGE_PROXY=none
DASH_ENABLE=false
CONFIRM=true
SERVER_URL=https://1.2.3.4:6443
TOKEN=dummytoken
`

	tmpFile, err := os.CreateTemp("", "netcup-kube-smoke-join-*.env")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}
