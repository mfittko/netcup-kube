package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfittko/netcup-kube/internal/remote"
	"github.com/spf13/cobra"
)

var (
	remoteHost       string
	remoteUser       string
	remotePubKey     string
	remoteRepo       string
	remoteConfigPath string
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Execute commands on remote hosts",
	Long: `Remote execution engine for netcup-kube.

Replaces the shell-based bin/netcup-kube-remote with Go implementation
for safer, more reliable remote operations.`,
	SilenceUsage: true,
}

var remoteProvisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Prepare the target host (sudo user + repo clone/update)",
	Long: `Provision prepares a fresh Netcup Debian 13 host from root credentials.

This command:
- Sets up SSH key access via root@<host>
- Installs sudo + git on the server (apt)
- Creates a sudo-enabled user and configures authorized_keys
- Clones the netcup-kube repo

Examples:
  netcup-kube remote provision
  netcup-kube remote --host root.example.com --user ops provision
  ROOT_PASS=xxx netcup-kube remote --host 203.0.113.10 provision`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildRemoteConfig()
		if err := cfg.LoadConfigFromEnv(remoteConfigPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.Host == "" {
			return fmt.Errorf("no host provided and no MGMT_HOST/MGMT_IP found in config")
		}

		return remote.Provision(cfg)
	},
}

var remoteGitCmd = &cobra.Command{
	Use:   "git",
	Short: "Remote git control for the repo (checkout/pull branch/ref)",
	Long: `Manage git state of the remote repository.

Examples:
  netcup-kube remote git --branch main --pull
  netcup-kube remote git --ref v1.0.0
  netcup-kube remote git --branch develop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildRemoteConfig()
		if err := cfg.LoadConfigFromEnv(remoteConfigPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.Host == "" {
			return fmt.Errorf("no host provided and no MGMT_HOST/MGMT_IP found in config")
		}

		client := remote.NewSSHClient(cfg.Host, cfg.User)

		// Ensure user access and repo exists
		if err := client.TestConnection(); err != nil {
			return fmt.Errorf("SSH connection failed. Run 'netcup-kube remote provision' first")
		}

		opts := remote.GitOptions{
			Branch:    gitBranch,
			Ref:       gitRef,
			Pull:      gitPull,
			PullIsSet: cmd.Flags().Changed("pull") || cmd.Flags().Changed("no-pull"),
		}

		// Default to pull for standalone git command
		if !opts.PullIsSet {
			opts.Pull = true
			opts.PullIsSet = true
		}

		return remote.RemoteGitSync(client, cfg.GetRemoteRepoDir(), opts)
	},
}

var remoteBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the Go CLI for the remote host (cross-compile locally and upload)",
	Long: `Build netcup-kube for the remote host architecture and upload.

This command:
- Detects the remote host architecture (amd64/arm64)
- Builds the Go CLI locally with cross-compilation
- Uploads the binary to the remote host

Examples:
  netcup-kube remote build
  netcup-kube remote build --branch main --pull`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildRemoteConfig()
		if err := cfg.LoadConfigFromEnv(remoteConfigPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.Host == "" {
			return fmt.Errorf("no host provided and no MGMT_HOST/MGMT_IP found in config")
		}

		client := remote.NewSSHClient(cfg.Host, cfg.User)

		// Ensure user access and repo exists
		if err := client.TestConnection(); err != nil {
			return fmt.Errorf("SSH connection failed. Run 'netcup-kube remote provision' first")
		}

		// Determine project root (try current directory first)
		projectRoot, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("could not find project root: %w", err)
		}

		opts := remote.GitOptions{
			Branch:    gitBranch,
			Ref:       gitRef,
			Pull:      gitPull,
			PullIsSet: cmd.Flags().Changed("pull") || cmd.Flags().Changed("no-pull"),
		}

		return remote.RemoteBuildAndUpload(client, cfg, projectRoot, opts)
	},
}

var (
	gitBranch string
	gitRef    string
	gitPull   bool
	runNoTTY  bool
	runEnvFile string
)

var remoteSmokeCmd = &cobra.Command{
	Use:   "smoke",
	Short: "Run a safe DRY_RUN smoke test on the remote management node",
	Long: `Run smoke tests in DRY_RUN mode on the remote host.

This command:
- Builds and uploads the netcup-kube binary
- Runs a series of non-interactive smoke tests
- Validates that the CLI works correctly on the remote host

Examples:
  netcup-kube remote smoke
  netcup-kube remote smoke --branch main --pull`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildRemoteConfig()
		if err := cfg.LoadConfigFromEnv(remoteConfigPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.Host == "" {
			return fmt.Errorf("no host provided and no MGMT_HOST/MGMT_IP found in config")
		}

		// Determine project root
		projectRoot, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("could not find project root: %w", err)
		}

		opts := remote.GitOptions{
			Branch:    gitBranch,
			Ref:       gitRef,
			Pull:      gitPull,
			PullIsSet: cmd.Flags().Changed("pull") || cmd.Flags().Changed("no-pull"),
		}

		return remote.Smoke(cfg, opts, projectRoot)
	},
}

var remoteRunCmd = &cobra.Command{
	Use:   "run [netcup-kube args...]",
	Short: "Run a netcup-kube command on the target host (forces TTY by default)",
	Long: `Execute a netcup-kube command on the remote host.

This command:
- Optionally syncs the remote repo to a specific branch/ref
- Uploads an env file if specified
- Runs the netcup-kube command with sudo
- Forces a TTY by default for interactive prompts

Examples:
  netcup-kube remote run bootstrap
  netcup-kube remote run pair
  netcup-kube remote run --env-file ./config/netcup-kube.env bootstrap
  netcup-kube remote run --branch main --pull bootstrap
  netcup-kube remote run --no-tty --env-file ./env/test.env bootstrap`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check for help flag first
		for _, arg := range args {
			if arg == "-h" || arg == "--help" || arg == "help" {
				return cmd.Help()
			}
		}

		// Parse flags manually since we have DisableFlagParsing
		parsedArgs, opts := parseRunArgs(args)

		cfg := buildRemoteConfig()
		if err := cfg.LoadConfigFromEnv(remoteConfigPath); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.Host == "" {
			return fmt.Errorf("no host provided and no MGMT_HOST/MGMT_IP found in config")
		}

		opts.Args = parsedArgs
		return remote.Run(cfg, opts)
	},
}

func buildRemoteConfig() *remote.Config {
	cfg := remote.NewConfig()
	
	if remoteHost != "" {
		cfg.Host = remoteHost
	}
	if remoteUser != "" {
		cfg.User = remoteUser
	}
	if remotePubKey != "" {
		cfg.PubKeyPath = remotePubKey
	}
	if remoteRepo != "" {
		cfg.RepoURL = remoteRepo
	}
	
	// Use default config path if not specified
	if remoteConfigPath == "" {
		remoteConfigPath = filepath.Join("config", "netcup-kube.env")
	}
	cfg.ConfigPath = remoteConfigPath

	return cfg
}

func parseRunArgs(args []string) ([]string, remote.RunOptions) {
	opts := remote.RunOptions{
		ForceTTY: true, // Default to TTY
	}

	remainingArgs := []string{}
	i := 0

	for i < len(args) {
		arg := args[i]

		switch arg {
		case "--no-tty":
			opts.ForceTTY = false
			i++
		case "--env-file":
			if i+1 < len(args) {
				opts.EnvFile = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--branch":
			if i+1 < len(args) {
				opts.Git.Branch = args[i+1]
				if !opts.Git.PullIsSet {
					opts.Git.Pull = true
				}
				i += 2
			} else {
				i++
			}
		case "--ref":
			if i+1 < len(args) {
				opts.Git.Ref = args[i+1]
				i += 2
			} else {
				i++
			}
		case "--pull":
			opts.Git.Pull = true
			opts.Git.PullIsSet = true
			i++
		case "--no-pull":
			opts.Git.Pull = false
			opts.Git.PullIsSet = true
			i++
		case "--":
			// Stop parsing flags
			i++
			remainingArgs = append(remainingArgs, args[i:]...)
			i = len(args)
		default:
			// All remaining args go to netcup-kube
			remainingArgs = append(remainingArgs, args[i:]...)
			i = len(args)
		}
	}

	return remainingArgs, opts
}

func findProjectRoot() (string, error) {
	// Try current directory first
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Check if scripts/main.sh exists in current directory
	if _, err := os.Stat(filepath.Join(currentDir, "scripts", "main.sh")); err == nil {
		return currentDir, nil
	}

	// If running from bin/, go up one level
	if filepath.Base(currentDir) == "bin" {
		parent := filepath.Dir(currentDir)
		if _, err := os.Stat(filepath.Join(parent, "scripts", "main.sh")); err == nil {
			return parent, nil
		}
	}

	// Try relative to the executable
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		projectRoot := filepath.Dir(exeDir)
		if _, err := os.Stat(filepath.Join(projectRoot, "scripts", "main.sh")); err == nil {
			return projectRoot, nil
		}
	}

	return currentDir, nil // Return current dir as fallback
}

func init() {
	// Add remote command flags
	remoteCmd.PersistentFlags().StringVar(&remoteHost, "host", "", "Remote host or IP address")
	remoteCmd.PersistentFlags().StringVar(&remoteUser, "user", "cubeadmin", "Remote sudo user")
	remoteCmd.PersistentFlags().StringVar(&remotePubKey, "pubkey", "", "Path to SSH public key")
	remoteCmd.PersistentFlags().StringVar(&remoteRepo, "repo", "https://github.com/mfittko/netcup-kube.git", "Repository URL")
	remoteCmd.PersistentFlags().StringVar(&remoteConfigPath, "config", "", "Path to config file (default: config/netcup-kube.env)")

	// Add git flags to commands that need them
	for _, cmd := range []*cobra.Command{remoteGitCmd, remoteBuildCmd, remoteSmokeCmd} {
		cmd.Flags().StringVar(&gitBranch, "branch", "", "Git branch name")
		cmd.Flags().StringVar(&gitRef, "ref", "", "Git ref (commit/tag)")
		cmd.Flags().BoolVar(&gitPull, "pull", false, "Pull latest changes")
		cmd.Flags().Bool("no-pull", false, "Do not pull changes")
	}

	// Add subcommands
	remoteCmd.AddCommand(remoteProvisionCmd)
	remoteCmd.AddCommand(remoteGitCmd)
	remoteCmd.AddCommand(remoteBuildCmd)
	remoteCmd.AddCommand(remoteSmokeCmd)
	remoteCmd.AddCommand(remoteRunCmd)
}
