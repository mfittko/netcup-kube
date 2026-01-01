package remote

import (
	"fmt"
	"os"
	"strings"
)

// Provision prepares the remote host with a sudo user and clones the repository
func Provision(cfg *Config) error {
	// Get public key
	pubKeyPath, err := cfg.GetPubKey()
	if err != nil {
		return err
	}

	pubKeyContent, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key: %w", err)
	}

	// Trim whitespace (especially trailing newlines) from the pubkey to avoid
	// breaking the shell script's grep/printf commands with embedded newlines.
	pubKey := strings.TrimSpace(string(pubKeyContent))
	if pubKey == "" {
		return fmt.Errorf("public key file is empty: %s", pubKeyPath)
	}
	// Validate it's a single line (no embedded newlines after trimming)
	if strings.Contains(pubKey, "\n") {
		return fmt.Errorf("public key file contains multiple lines: %s", pubKeyPath)
	}

	// Create root SSH client
	rootClient := NewSSHClient(cfg.Host, "root")

	// Ensure root access
	fmt.Printf("Testing SSH access to root@%s...\n", cfg.Host)
	if err := ensureRootAccess(rootClient, cfg.Host, pubKeyPath); err != nil {
		return err
	}

	// Build and run the provisioning script
	script := buildProvisionScript(cfg.User, pubKey, cfg.RepoURL, cfg.Host)

	fmt.Printf("[remote] Provisioning %s@%s...\n", cfg.User, cfg.Host)
	if err := rootClient.ExecuteScript(script, nil); err != nil {
		return fmt.Errorf("provisioning failed: %w", err)
	}

	return nil
}

// ensureRootAccess ensures we can SSH to root, copying keys if needed
func ensureRootAccess(client Client, host string, pubKeyPath string) error {
	// Test if we already have access
	if err := client.TestConnection(); err == nil {
		fmt.Printf("SSH key already works for root@%s\n", host)
		return nil
	}

	// Check if sshpass is available for password auth
	if _, err := lookPath("sshpass"); err == nil {
		// Try to use sshpass
		rootPass := os.Getenv("ROOT_PASS")
		if rootPass == "" {
			// No password provided; instruct user to set ROOT_PASS or use ssh-copy-id
			return fmt.Errorf("ROOT_PASS environment variable is empty or not set. Either set ROOT_PASS or run:\n  ssh-copy-id -o StrictHostKeyChecking=no -i %s root@%s", pubKeyPath, host)
		}

		fmt.Println("Pushing SSH key to root with sshpass+ssh-copy-id")
		// Use SSHPASS env var instead of passing the password on the command line (-p),
		// which could be visible to other users via process listings.
		cmd := execCommand("sshpass", "-e", "ssh-copy-id",
			"-o", "StrictHostKeyChecking=no",
			"-f", "-i", pubKeyPath,
			fmt.Sprintf("root@%s", host))
		cmd.Env = append(os.Environ(), "SSHPASS="+rootPass)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to copy SSH key: %w", err)
		}

		// Best-effort cleanup: clears the env var for subsequent commands in this process.
		// Note: this does not guarantee the value is removed from memory.
		_ = os.Unsetenv("ROOT_PASS")
		return nil
	}

	// No automated method available
	return fmt.Errorf(`passwordless SSH for root not set up yet
Install sshpass to allow password authentication, or run:
  ssh-copy-id -o StrictHostKeyChecking=no -i %s root@%s
Then re-run the provision command`, pubKeyPath, host)
}

// buildProvisionScript creates the provisioning script
func buildProvisionScript(user, pubKey, repoURL, host string) string {
	template := `set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y --no-install-recommends sudo git curl ca-certificates

# Create user if missing
if ! id -u __NEW_USER__ >/dev/null 2>&1; then
  adduser --disabled-password --gecos "" __NEW_USER__
fi
usermod -aG sudo __NEW_USER__
install -d -m 0700 -o __NEW_USER__ -g __NEW_USER__ /home/__NEW_USER__/.ssh

# Append key once (treat pubkey literally, no regex interpretation)
if ! grep -Fxq "__PUBKEY__" /home/__NEW_USER__/.ssh/authorized_keys 2>/dev/null; then
  printf '%s\n' "__PUBKEY__" >> /home/__NEW_USER__/.ssh/authorized_keys
fi
chown __NEW_USER__:__NEW_USER__ /home/__NEW_USER__/.ssh/authorized_keys
chmod 0600 /home/__NEW_USER__/.ssh/authorized_keys

# Passwordless sudo for the new user
cat >/etc/sudoers.d/90-__NEW_USER__ <<EOF
__NEW_USER__ ALL=(ALL) NOPASSWD:ALL
EOF
chmod 0440 /etc/sudoers.d/90-__NEW_USER__

# Clone or update netcup-kube
if [[ ! -d /home/__NEW_USER__/netcup-kube ]]; then
  sudo -u __NEW_USER__ git clone __REPO_URL__ /home/__NEW_USER__/netcup-kube
else
  # Only fetch here; pulling can fail if the repo is on a local branch
  cd /home/__NEW_USER__/netcup-kube && sudo -u __NEW_USER__ git fetch --all -p
fi

# Print completion message
cat <<EOM
[remote] Provisioning complete.
Now run on your local machine (recommended):
  netcup-kube remote run bootstrap

Or SSH into the server:
  ssh __NEW_USER__@__HOST__
Then on the server:
  sudo /home/__NEW_USER__/netcup-kube/bin/netcup-kube bootstrap
EOM
`

	script := strings.ReplaceAll(template, "__NEW_USER__", user)
	script = strings.ReplaceAll(script, "__PUBKEY__", pubKey)
	script = strings.ReplaceAll(script, "__REPO_URL__", repoURL)
	script = strings.ReplaceAll(script, "__HOST__", host)

	return script
}
