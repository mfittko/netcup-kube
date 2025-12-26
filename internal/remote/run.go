package remote

import (
	"fmt"
	"os"
	"strings"
)

// Run executes a netcup-kube command on the remote host
func Run(cfg *Config, opts RunOptions) error {
	// Create user SSH client
	client := NewSSHClient(cfg.Host, cfg.User)

	return runWithClient(client, cfg, opts)
}

func runWithClient(client Client, cfg *Config, opts RunOptions) error {
	// Ensure user access
	if err := ensureUserAccess(client, cfg); err != nil {
		return err
	}

	// Ensure remote repo exists
	if err := ensureRemoteRepo(client, cfg); err != nil {
		return err
	}

	// Validate command
	if len(opts.Args) < 1 {
		return fmt.Errorf("missing netcup-kube command arguments")
	}

	// Keep this list intentionally small: `remote run` is meant for the main lifecycle commands
	// that are safe and commonly used over SSH. If new top-level commands are added to netcup-kube,
	// update this allowlist accordingly.
	supportedCmds := []string{"bootstrap", "join", "pair", "dns", "install", "ssh", "help", "-h", "--help"}
	cmdValid := false
	for _, cmd := range supportedCmds {
		if opts.Args[0] == cmd {
			cmdValid = true
			break
		}
	}
	if !cmdValid {
		return fmt.Errorf("unsupported netcup-kube command for remote run: %s (supported: %v)", 
			opts.Args[0], supportedCmds)
	}

	// Sync git if requested
	if opts.Git.Branch != "" || opts.Git.Ref != "" || opts.Git.Pull {
		if err := RemoteGitSync(client, cfg.GetRemoteRepoDir(), opts.Git); err != nil {
			return fmt.Errorf("git sync failed: %w", err)
		}
	}

	// Check if remote binary exists
	remoteBin := cfg.GetRemoteBinPath()
	if err := client.Execute("test", []string{"-x", remoteBin}, false); err != nil {
		return fmt.Errorf(`remote netcup-kube binary not found or not executable: %s@%s:%s
Build/upload it first:
  netcup-kube remote build`, cfg.User, cfg.Host, remoteBin)
	}

	// Upload env file if specified
	remoteEnv := "__NONE__"
	if opts.EnvFile != "" {
		if !fileExists(opts.EnvFile) {
			return fmt.Errorf("--env-file not found: %s", opts.EnvFile)
		}

		remoteEnv = fmt.Sprintf("/tmp/netcup-kube-remote.env.%d", os.Getpid())
		fmt.Printf("[local] Uploading env file to %s@%s:%s\n", cfg.User, cfg.Host, remoteEnv)
		if err := client.Upload(opts.EnvFile, remoteEnv); err != nil {
			return fmt.Errorf("failed to upload env file: %w", err)
		}
		defer cleanupRemoteEnv(client, remoteEnv, opts.ForceTTY)
	}

	// Build the remote runner script
	runnerScript := `set -euo pipefail
env_file="${1:-}"
bin="${2:-}"
shift 2 || true

if [[ "${env_file}" != "__NONE__" && -n "${env_file}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
fi

exec "${bin}" "$@"
`

	// Build command arguments for the remote runner
	// We need to pass this as a single remote shell command string.
	//
	// Escaping layers (intentional):
	// - `runnerScript` is shell-escaped and passed as the argument to `bash -lc` on the remote host.
	// - Each user-provided arg is individually shell-escaped so it cannot inject additional shell tokens
	//   when we join the command string and feed it to `ssh`.
	// - The remote runner then execs the remote binary with the original argv preserved.
	cmdParts := []string{"sudo", "-E", "bash", "-lc", shellEscape(runnerScript), "bash", remoteEnv, remoteBin}
	
	// Escape each user argument for safe shell execution
	for _, arg := range opts.Args {
		cmdParts = append(cmdParts, shellEscape(arg))
	}

	// Build the full command string
	cmdString := strings.Join(cmdParts, " ")

	fmt.Printf("[local] Running on %s@%s: netcup-kube %s\n", cfg.User, cfg.Host, 
		joinArgs(opts.Args))

	return client.RunCommandString(cmdString, opts.ForceTTY)
}

// ensureUserAccess checks if we can SSH as the user
func ensureUserAccess(client Client, cfg *Config) error {
	if err := client.TestConnection(); err == nil {
		return nil
	}

	return fmt.Errorf(`SSH key does not work for %s@%s.
Run provisioning first (uses root once):
  netcup-kube remote provision`, cfg.User, cfg.Host)
}

// ensureRemoteRepo checks if the remote repository exists
func ensureRemoteRepo(client Client, cfg *Config) error {
	repoDir := cfg.GetRemoteRepoDir()
	if err := client.Execute("test", []string{"-d", repoDir}, false); err == nil {
		return nil
	}

	return fmt.Errorf(`remote repo not found at %s@%s:%s
Run provisioning first:
  netcup-kube remote provision`, cfg.User, cfg.Host, repoDir)
}

// cleanupRemoteEnv removes the temporary env file from the remote host
func cleanupRemoteEnv(client Client, remoteEnv string, forceTTY bool) {
	if remoteEnv == "__NONE__" {
		return
	}

	if err := client.Execute("sudo", []string{"rm", "-f", remoteEnv}, forceTTY); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean up remote env file %s: %v\n", remoteEnv, err)
	}
}

// joinArgs joins arguments for display
func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		// Quote args that contain spaces
		if containsSpace(arg) {
			result += fmt.Sprintf("%q", arg)
		} else {
			result += arg
		}
	}
	return result
}

// containsSpace checks if a string contains whitespace
func containsSpace(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			return true
		}
	}
	return false
}
