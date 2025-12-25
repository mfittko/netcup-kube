package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfittko/netcup-kube/internal/config"
	"github.com/mfittko/netcup-kube/internal/executor"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	
	cfg            *config.Config
	scriptExecutor *executor.Executor
	
	// Global flags
	envFile          string
	dryRun           bool
	dryRunWriteFiles bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "netcup-kube",
	Short: "Bootstrap and manage k3s clusters on Netcup servers",
	Long: `netcup-kube is a CLI tool for bootstrapping and managing production-ready
k3s clusters on Netcup root servers with optional vLAN worker nodes.

It provides commands to install k3s, configure Traefik, set up edge TLS via Caddy,
and manage worker node joins.`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize config
		cfg = config.New()
		
		// Load configuration in correct precedence order (lowest to highest priority):
		// 1. environment variables (lowest priority)
		// 2. env-file
		// 3. command-line flags (highest priority)
		
		// Load from environment first
		cfg.LoadFromEnvironment()
		
		// Load from env file (if specified or default exists) - this can override env vars
		if envFile == "" {
			// Try default location
			homeEnvFile := filepath.Join("config", "netcup-kube.env")
			if _, err := os.Stat(homeEnvFile); err == nil {
				envFile = homeEnvFile
			}
		}
		
		if envFile != "" {
			if err := cfg.LoadEnvFile(envFile); err != nil {
				return fmt.Errorf("failed to load env file: %w", err)
			}
		}
		
		// Apply dry-run flags last (these override everything)
		if dryRun {
			cfg.SetFlag("DRY_RUN", "true")
		}
		if dryRunWriteFiles {
			cfg.SetFlag("DRY_RUN_WRITE_FILES", "true")
		}
		
		// Initialize executor
		var err error
		scriptExecutor, err = executor.New()
		if err != nil {
			return fmt.Errorf("failed to initialize executor: %w", err)
		}
		
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&envFile, "env-file", "", "Path to environment file (default: config/netcup-kube.env if exists)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Enable dry-run mode (no actual changes)")
	rootCmd.PersistentFlags().BoolVar(&dryRunWriteFiles, "dry-run-write-files", false, "Dry-run but write config files")
	
	// Add subcommands
	rootCmd.AddCommand(bootstrapCmd)
	rootCmd.AddCommand(joinCmd)
	rootCmd.AddCommand(dnsCmd)
	rootCmd.AddCommand(pairCmd)
}

// filterGlobalFlags removes global flags from args and applies them to cfg.
// This is used for commands with DisableFlagParsing to manually handle global flags.
// Note: This function assumes cfg has been initialized by PersistentPreRunE.
func filterGlobalFlags(args []string) []string {
	if cfg == nil {
		// This should never happen in normal flow as PersistentPreRunE runs first,
		// but guard against it for safety
		fmt.Fprintf(os.Stderr, "error: config not initialized\n")
		return args
	}
	
	filteredArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dry-run" {
			cfg.SetFlag("DRY_RUN", "true")
		} else if arg == "--dry-run-write-files" {
			cfg.SetFlag("DRY_RUN_WRITE_FILES", "true")
		} else if arg == "--env-file" && i+1 < len(args) {
			// Skip --env-file and its value (already handled in PreRunE)
			i++
		} else if strings.HasPrefix(arg, "--env-file=") {
			// Skip --env-file=value (already handled in PreRunE)
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	return filteredArgs
}

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Install and configure k3s server + Traefik NodePort + optional Caddy & Dashboard",
	Long: `Bootstrap a k3s server node with production-ready configuration.

This command installs k3s in server mode, configures Traefik to use NodePort,
and optionally sets up Caddy for edge TLS and the Kubernetes Dashboard.

Examples:
  sudo netcup-kube bootstrap
  sudo netcup-kube bootstrap --dry-run
  sudo BASE_DOMAIN=example.com netcup-kube bootstrap`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set MODE to bootstrap (though it's already the default)
		cfg.SetFlag("MODE", "bootstrap")
		
		return scriptExecutor.Execute("bootstrap", args, cfg.ToEnvSlice())
	},
}

var joinCmd = &cobra.Command{
	Use:   "join",
	Short: "Join a k3s worker node to an existing cluster",
	Long: `Join this node to an existing k3s cluster as a worker (agent).

Requires SERVER_URL and TOKEN (or TOKEN_FILE) to be set via environment
variables or flags.

Examples:
  sudo SERVER_URL=https://x.x.x.x:6443 TOKEN=xxx netcup-kube join
  sudo netcup-kube join --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg.SetFlag("MODE", "join")
		
		return scriptExecutor.Execute("join", args, cfg.ToEnvSlice())
	},
}

var dnsCmd = &cobra.Command{
	Use:                "dns",
	Short:              "Configure edge TLS via Caddy",
	Long: `Configure edge TLS via Caddy using either DNS-01 wildcard (default)
or HTTP-01 for explicit hostnames.

DNS-01 mode requires Netcup DNS API credentials and creates a wildcard
certificate for *.BASE_DOMAIN.

HTTP-01 mode obtains certificates for specific hostnames.

Examples:
  # DNS-01 wildcard (default)
  sudo BASE_DOMAIN=example.com netcup-kube dns
  
  # HTTP-01 for specific hosts
  sudo netcup-kube dns --type edge-http --domains "kube.example.com,demo.example.com"
  
  # Add more domains to existing HTTP-01 config
  sudo netcup-kube dns --type edge-http --add-domains "new.example.com"`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if help was requested
		for _, arg := range args {
			if arg == "-h" || arg == "--help" || arg == "help" {
				// Pass through to the script to show its help
				return scriptExecutor.Execute("dns", args, cfg.ToEnvSlice())
			}
		}
		
		// Filter global flags and apply them to config
		filteredArgs := filterGlobalFlags(args)
		return scriptExecutor.Execute("dns", filteredArgs, cfg.ToEnvSlice())
	},
}

var pairCmd = &cobra.Command{
	Use:                "pair",
	Short:              "Print a copy/paste join command for a worker node",
	Long: `Print a copy/paste join command for a worker node.

Optionally opens UFW firewall on port 6443 for a specific source IP/CIDR.

This command reads the join token from /var/lib/rancher/k3s/server/node-token
and displays a ready-to-use join command for worker nodes.

Examples:
  sudo netcup-kube pair
  sudo netcup-kube pair --allow-from 159.195.64.217
  sudo netcup-kube pair --server-url https://152.53.136.34:6443`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if help was requested
		for _, arg := range args {
			if arg == "-h" || arg == "--help" || arg == "help" {
				// Pass through to the script to show its help
				return scriptExecutor.Execute("pair", args, cfg.ToEnvSlice())
			}
		}
		
		// Filter global flags and apply them to config
		filteredArgs := filterGlobalFlags(args)
		return scriptExecutor.Execute("pair", filteredArgs, cfg.ToEnvSlice())
	},
}
