package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mfittko/netcup-kube/internal/config"
	"github.com/mfittko/netcup-kube/internal/tunnel"
	"github.com/spf13/cobra"
)

const serverKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

func manualRemoteDNSAddDomainsCommand(domain string) string {
	return fmt.Sprintf("CONFIRM=true bin/netcup-kube remote run --no-tty -- dns --type edge-http --add-domains \"%s\"", domain)
}

var installCmd = &cobra.Command{
	Use:   "install <recipe> [recipe-options]",
	Short: "Install optional components and tools onto the k3s cluster",
	Long: `Install optional components (recipes) onto the k3s cluster.

This command installs recipes (add-ons) such as Argo CD, PostgreSQL, Redis,
Sealed Secrets, Prometheus, and more. Each recipe is installed via its
dedicated install script.

Available recipes:
  argo-cd                  Install Argo CD (GitOps continuous delivery tool)
  postgres                 Install PostgreSQL (Bitnami Helm chart)
  redis                    Install Redis (Bitnami Helm chart)
  sealed-secrets           Install Sealed Secrets (encrypt secrets for Git)
  redisinsight             Install RedisInsight (Redis GUI)
  kube-prometheus-stack    Install Grafana + Prometheus + Alertmanager
  dashboard                Install Kubernetes Dashboard (official web UI)

Examples:
  netcup-kube install argo-cd --help
  netcup-kube install argo-cd --host cd.example.com
  netcup-kube install redis --namespace platform --storage 20Gi`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Need at least the recipe name
		if len(args) < 1 {
			return cmd.Help()
		}

		// Check for help flag on the install command itself (not the recipe)
		if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
			return cmd.Help()
		}

		recipe := args[0]
		recipeArgs := args[1:]

		// Find project root
		projectRoot, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("could not find project root: %w", err)
		}

		// Check if recipe exists
		recipesDir := filepath.Join(projectRoot, "scripts", "recipes")
		recipeScript := filepath.Join(recipesDir, recipe, "install.sh")

		if _, err := os.Stat(recipeScript); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("unknown recipe: %s\nRun 'netcup-kube install --help' to see available recipes", recipe)
			}
			return fmt.Errorf("cannot access recipe script: %w", err)
		}

		// Ensure script is executable
		if err := os.Chmod(recipeScript, 0755); err != nil {
			return fmt.Errorf("failed to make recipe script executable: %w", err)
		}

		// Setup environment
		configDir := filepath.Join(projectRoot, "config")
		localKubeconfig := filepath.Join(configDir, "k3s.yaml")
		envFile := filepath.Join(configDir, "netcup-kube.env")

		// Check if this is a help request - if so, skip kubeconfig setup
		isHelpRequest := false
		for _, arg := range recipeArgs {
			if arg == "-h" || arg == "--help" || arg == "help" {
				isHelpRequest = true
				break
			}
		}

		// Parse --host flag for automatic domain management
		hostArg, adminHostArg := parseRecipeHostArgs(recipeArgs)

		// Ensure kubeconfig is available (unless just showing help)
		kubeconfig := os.Getenv("KUBECONFIG")
		if !isHelpRequest {
			// If KUBECONFIG isn't set, default to the repo's ./config/k3s.yaml when running locally.
			// When running on the server, prefer the node-local kubeconfig.
			if kubeconfig == "" {
				if _, err := os.Stat(serverKubeconfigPath); err == nil {
					kubeconfig = serverKubeconfigPath
				} else {
					kubeconfig = localKubeconfig
				}
			}

			// If we are using a local kubeconfig path and it's missing, fetch it via scp.
			// This also covers the case where the user set KUBECONFIG explicitly to a local path.
			if kubeconfig != serverKubeconfigPath {
				if _, err := os.Stat(kubeconfig); err != nil {
					fmt.Printf("Kubeconfig %s not found. Fetching from remote...\n", kubeconfig)
					if err := fetchKubeconfig(envFile, kubeconfig, filepath.Dir(kubeconfig)); err != nil {
						return err
					}
					fmt.Printf("Kubeconfig saved to %s\n", kubeconfig)
				}
			}
		}

		// Check if tunnel is needed and running (when not using the server's kubeconfig path, and not just showing help)
		if !isHelpRequest && kubeconfig != serverKubeconfigPath {
			if err := ensureTunnelRunning(envFile, projectRoot); err != nil {
				return err
			}
		}

		// Run the recipe
		recipeCmd := exec.Command(recipeScript, recipeArgs...)
		if kubeconfig != "" {
			recipeCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfig))
		} else {
			recipeCmd.Env = os.Environ()
		}
		recipeCmd.Stdin = os.Stdin
		recipeCmd.Stdout = os.Stdout
		recipeCmd.Stderr = os.Stderr

		if err := recipeCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("recipe execution failed: %w", err)
		}

		// If recipe succeeded and --host/--admin-host were specified, auto-add domain(s) to Caddy
		domainsToAdd := uniqueNonEmptyStrings([]string{hostArg, adminHostArg})
		if len(domainsToAdd) > 0 {
			// Only do this when running locally (not on the server)
			if _, err := os.Stat("/etc/caddy/Caddyfile"); err != nil {
				remoteBin := filepath.Join(projectRoot, "bin", "netcup-kube")
				if _, err := os.Stat(remoteBin); err == nil {
					// Create temp env file with CONFIRM=true
					tmpEnv, err := os.CreateTemp("", "netcup-kube-install.env.*")
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create temp env file: %v\n", err)
						return nil
					}
					tmpEnvPath := tmpEnv.Name()
					defer func() {
						if removeErr := os.Remove(tmpEnvPath); removeErr != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to remove temp env file: %v\n", removeErr)
						}
					}()

					if _, err := tmpEnv.WriteString("CONFIRM=true\n"); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to write temp env file: %v\n", err)
						_ = tmpEnv.Close()
						return nil
					}
					if closeErr := tmpEnv.Close(); closeErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to close temp env file: %v\n", closeErr)
						return nil
					}

					for _, domain := range domainsToAdd {
						fmt.Printf("\nAdding %s to Caddy edge-http domains...\n", domain)

						// Run the domain add command
						dnsCmd := exec.Command(remoteBin, buildRemoteDNSAddDomainsArgs(tmpEnvPath, domain)...)
						dnsCmd.Stdout = os.Stdout
						dnsCmd.Stderr = os.Stderr

						if err := dnsCmd.Run(); err == nil {
							fmt.Println("✓ Domain added successfully!")
						} else {
							fmt.Printf("⚠ Failed to add domain automatically. Run manually:\n")
							fmt.Printf("  %s\n", manualRemoteDNSAddDomainsCommand(domain))
						}
					}
				}
			}
		}

		return nil
	},
}

func buildRemoteDNSAddDomainsArgs(envFile string, domain string) []string {
	return []string{"remote", "run", "--no-tty", "--env-file", envFile, "--", "dns", "--type", "edge-http", "--add-domains", domain}
}

func parseRecipeHostArgs(recipeArgs []string) (host string, adminHost string) {
	for i, arg := range recipeArgs {
		switch {
		case strings.HasPrefix(arg, "--host="):
			host = strings.TrimPrefix(arg, "--host=")
		case arg == "--host" && i+1 < len(recipeArgs) && !strings.HasPrefix(recipeArgs[i+1], "--"):
			host = recipeArgs[i+1]
		case strings.HasPrefix(arg, "--admin-host="):
			adminHost = strings.TrimPrefix(arg, "--admin-host=")
		case arg == "--admin-host" && i+1 < len(recipeArgs) && !strings.HasPrefix(recipeArgs[i+1], "--"):
			adminHost = recipeArgs[i+1]
		}
	}

	return host, adminHost
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique
}

func fetchKubeconfig(envFile, localKubeconfig, configDir string) error {
	// Check if env file exists
	if _, err := os.Stat(envFile); err != nil {
		return fmt.Errorf("ERROR: %s not found. Please create it from the example", envFile)
	}

	// Load env file to get MGMT_HOST and MGMT_USER
	env, err := config.LoadEnvFileToMap(envFile)
	if err != nil {
		return fmt.Errorf("failed to load env file: %w", err)
	}

	remoteHost := env["MGMT_HOST"]
	if remoteHost == "" {
		remoteHost = env["MGMT_IP"]
	}
	if remoteHost == "" {
		return fmt.Errorf("ERROR: MGMT_HOST not set in %s", envFile)
	}

	remoteUser := env["MGMT_USER"]
	if remoteUser == "" {
		remoteUser = "ops"
	}

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Fetch kubeconfig via scp
	fmt.Printf("Fetching kubeconfig from %s@%s:/etc/rancher/k3s/k3s.yaml\n", remoteUser, remoteHost)

	scpCmd := exec.Command("scp",
		fmt.Sprintf("%s@%s:/etc/rancher/k3s/k3s.yaml", remoteUser, remoteHost),
		localKubeconfig)
	scpCmd.Stdout = os.Stdout
	scpCmd.Stderr = os.Stderr

	if err := scpCmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch kubeconfig: %w", err)
	}

	return nil
}

func ensureTunnelRunning(envFile, projectRoot string) error {
	// Load env file to get tunnel settings
	env, err := config.LoadEnvFileToMap(envFile)
	if err != nil {
		// If env file doesn't exist, use defaults
		env = make(map[string]string)
	}

	remoteHost := env["MGMT_HOST"]
	if remoteHost == "" {
		remoteHost = env["MGMT_IP"]
	}
	remoteUser := env["MGMT_USER"]
	if remoteUser == "" {
		remoteUser = "ops"
	}
	tunnelPort := env["TUNNEL_LOCAL_PORT"]
	if tunnelPort == "" {
		tunnelPort = "6443"
	}

	if remoteHost == "" || remoteUser == "" {
		// Can't check tunnel without host/user info
		return nil
	}

	// Create tunnel manager
	mgr := tunnel.New(remoteUser, remoteHost, tunnelPort, "127.0.0.1", "6443")

	// Check if tunnel is running
	if mgr.IsRunning() {
		fmt.Printf("Using tunnel: localhost:%s -> %s:6443\n", tunnelPort, remoteHost)
		return nil
	}

	// Tunnel not running, start it
	fmt.Println("SSH tunnel not running. Starting tunnel...")

	if err := mgr.Start(); err != nil {
		troubleshooting := fmt.Sprintf("\n\nTroubleshooting:\n  - Verify MGMT_HOST=%s and MGMT_USER=%s are correct\n  - Check SSH access: ssh %s@%s\n  - Start tunnel manually: netcup-kube ssh tunnel start",
			remoteHost, remoteUser, remoteUser, remoteHost)
		return fmt.Errorf("failed to start SSH tunnel: %w%s", err, troubleshooting)
	}

	// Give tunnel a moment to establish
	time.Sleep(1 * time.Second)

	// Verify tunnel is now running
	if !mgr.IsRunning() {
		troubleshooting := fmt.Sprintf("\n\nTroubleshooting:\n  - Check SSH access: ssh %s@%s\n  - View tunnel status: netcup-kube ssh tunnel status\n  - Start tunnel manually: netcup-kube ssh tunnel start",
			remoteUser, remoteHost)
		return fmt.Errorf("tunnel failed to start (verify SSH access and known_hosts)%s", troubleshooting)
	}

	fmt.Printf("Using tunnel: localhost:%s -> %s:6443\n", tunnelPort, remoteHost)
	return nil
}
