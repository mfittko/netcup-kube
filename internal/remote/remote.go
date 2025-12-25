package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	execCommand = exec.Command
	lookPath = exec.LookPath
	mkdirTemp = os.MkdirTemp
	removeAll = os.RemoveAll
	localGoBuild = func(projectRoot, out, goarch string) error {
		cmd := execCommand("go", "build", "-o", out, "./cmd/netcup-kube")
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS=linux",
			fmt.Sprintf("GOARCH=%s", goarch),
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
)

const (
	defaultUser     = "cubeadmin"
	defaultRepoURL  = "https://github.com/mfittko/netcup-kube.git"
	remoteRepoDir   = "/home/%s/netcup-kube"
	remoteBinPath   = "/home/%s/netcup-kube/bin/netcup-kube"
)

// Config holds the configuration for remote operations
type Config struct {
	Host       string
	User       string
	PubKeyPath string
	RepoURL    string
	ConfigPath string
}

// GitOptions holds options for git operations
type GitOptions struct {
	Branch    string
	Ref       string
	Pull      bool
	PullIsSet bool
}

// RunOptions holds options for the run command
type RunOptions struct {
	Git     GitOptions
	ForceTTY bool
	EnvFile string
	Args    []string
}

// NewConfig creates a new remote config with defaults
func NewConfig() *Config {
	return &Config{
		User:    defaultUser,
		RepoURL: defaultRepoURL,
	}
}

// LoadConfigFromEnv loads host configuration from environment file if available
func (c *Config) LoadConfigFromEnv(configPath string) error {
	if configPath == "" || !fileExists(configPath) {
		return nil
	}

	// Read the config file to extract MGMT_HOST/MGMT_IP and MGMT_USER/DEFAULT_USER
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	vars := make(map[string]string)
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}

	// Set host if not already set
	if c.Host == "" {
		if host, ok := vars["MGMT_HOST"]; ok && host != "" {
			c.Host = host
		} else if ip, ok := vars["MGMT_IP"]; ok && ip != "" {
			c.Host = ip
		}
	}

	// Set user if still default
	if c.User == defaultUser {
		if user, ok := vars["MGMT_USER"]; ok && user != "" {
			c.User = user
		} else if user, ok := vars["DEFAULT_USER"]; ok && user != "" {
			c.User = user
		}
	}

	return nil
}

// GetPubKey returns the public key path, searching for default keys if not set
func (c *Config) GetPubKey() (string, error) {
	if c.PubKeyPath != "" {
		if fileExists(c.PubKeyPath) {
			return c.PubKeyPath, nil
		}
		return "", fmt.Errorf("public key not found: %s", c.PubKeyPath)
	}

	// Search for default keys
	home := os.Getenv("HOME")
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519.pub"),
		filepath.Join(home, ".ssh", "id_rsa.pub"),
	}

	for _, cand := range candidates {
		if fileExists(cand) {
			c.PubKeyPath = cand
			return cand, nil
		}
	}

	return "", fmt.Errorf("no public key found. Generate one with: ssh-keygen -t ed25519 -C '%s@%s'", 
		os.Getenv("USER"), getHostname())
}

// GetRemoteRepoDir returns the remote repository directory path
func (c *Config) GetRemoteRepoDir() string {
	return fmt.Sprintf(remoteRepoDir, c.User)
}

// GetRemoteBinPath returns the remote binary path
func (c *Config) GetRemoteBinPath() string {
	return fmt.Sprintf(remoteBinPath, c.User)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getHostname returns the current hostname
func getHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return name
}

// RemoteGitSync synchronizes the remote repository
func RemoteGitSync(client Client, repoDir string, opts GitOptions) error {
	branch := opts.Branch
	ref := opts.Ref
	pull := opts.Pull

	// Use placeholders to preserve empty arguments
	branchArg := "__NONE__"
	if branch != "" {
		branchArg = branch
	}
	
	refArg := "__NONE__"
	if ref != "" {
		refArg = ref
	}

	pullArg := "false"
	if pull {
		pullArg = "true"
	}

	script := `set -euo pipefail
repo="${1:?repo dir required}"
branch="${2:-__NONE__}"
ref="${3:-__NONE__}"
pull="${4:-true}"

[[ "${branch}" == "__NONE__" ]] && branch=""
[[ "${ref}" == "__NONE__" ]] && ref=""

cd "${repo}"
git fetch --all -p

if [[ -n "${ref}" ]]; then
  echo "[remote] checkout ref: ${ref}"
  git checkout --detach "${ref}"
elif [[ -n "${branch}" ]]; then
  echo "[remote] checkout branch: ${branch}"
  if git show-ref --verify --quiet "refs/heads/${branch}"; then
    git checkout "${branch}"
  else
    if ! git show-ref --verify --quiet "refs/remotes/origin/${branch}"; then
      echo "[remote] ERROR: origin/${branch} not found" >&2
      exit 1
    fi
    git checkout -b "${branch}" --track "origin/${branch}"
  fi
fi

if [[ -n "${branch}" && -z "${ref}" ]]; then
  # Make sure the branch tracks the correct remote branch
  git branch --set-upstream-to="origin/${branch}" "${branch}" >/dev/null 2>&1 || true
fi

if [[ "${pull}" == "true" && -z "${ref}" ]]; then
  if [[ -n "${branch}" ]]; then
    echo "[remote] pull: origin ${branch} (ff-only)"
    git pull --ff-only origin "${branch}"
  else
    echo "[remote] NOTE: --pull requested but no --branch/--ref provided; skipping pull." >&2
  fi
fi
`

	return client.ExecuteScript(script, []string{repoDir, branchArg, refArg, pullArg})
}

// RemoteBuildAndUpload builds the Go binary locally and uploads it to the remote host
func RemoteBuildAndUpload(client Client, cfg *Config, projectRoot string, opts GitOptions) error {
	// Sync git if requested
	if opts.Branch != "" || opts.Ref != "" || opts.Pull {
		if err := RemoteGitSync(client, cfg.GetRemoteRepoDir(), opts); err != nil {
			return fmt.Errorf("git sync failed: %w", err)
		}
	}

	// Check for local go toolchain
	if _, err := lookPath("go"); err != nil {
		return fmt.Errorf("missing local 'go' toolchain. Install Go 1.23+ and retry")
	}

	// Detect remote architecture
	goarch, err := remoteDetectGoarch(client)
	if err != nil {
		return err
	}

	// Build locally
	tmpDir, err := mkdirTemp("", "netcup-kube")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer removeAll(tmpDir)

	out := filepath.Join(tmpDir, "netcup-kube")
	fmt.Printf("[local] Building netcup-kube for linux/%s\n", goarch)

	if err := localGoBuild(projectRoot, out, goarch); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	remoteBin := cfg.GetRemoteBinPath()
	remoteBinDir := filepath.Dir(remoteBin)

	fmt.Printf("[local] Uploading %s to %s@%s:%s\n", out, cfg.User, cfg.Host, remoteBin)

	// Create remote bin directory
	if err := client.Execute("install", []string{"-d", "-m", "0755", remoteBinDir}, false); err != nil {
		return fmt.Errorf("failed to create remote bin directory: %w", err)
	}

	// Upload the binary
	if err := client.Upload(out, remoteBin); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	// Make it executable
	if err := client.Execute("chmod", []string{"+x", remoteBin}, false); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	fmt.Printf("[local] Done. Remote CLI: %s\n", remoteBin)
	return nil
}

// remoteDetectGoarch detects the remote architecture
func remoteDetectGoarch(client Client) (string, error) {
	output, err := client.OutputCommand("uname", []string{"-m"})
	if err != nil {
		return "", fmt.Errorf("failed to detect remote architecture: %w", err)
	}

	arch := strings.TrimSpace(string(output))
	switch arch {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote architecture: %s", arch)
	}
}
