package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

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
		hostArg := ""
		for i, arg := range recipeArgs {
			if strings.HasPrefix(arg, "--host=") {
				hostArg = strings.TrimPrefix(arg, "--host=")
				break
			} else if arg == "--host" && i+1 < len(recipeArgs) {
				hostArg = recipeArgs[i+1]
				break
			}
		}

		// Ensure kubeconfig is available (unless just showing help)
		kubeconfig := os.Getenv("KUBECONFIG")
		if !isHelpRequest && kubeconfig == "" {
			// Check if on the server
			if _, err := os.Stat("/etc/rancher/k3s/k3s.yaml"); err != nil {
				// Not on server; fetch from remote if not already cached locally
				if _, err := os.Stat(localKubeconfig); err != nil {
					fmt.Println("KUBECONFIG not set and local kubeconfig not found. Fetching from remote...")
					
					if err := fetchKubeconfig(envFile, localKubeconfig, configDir); err != nil {
						return err
					}
					
					fmt.Printf("Kubeconfig saved to %s\n", localKubeconfig)
				}
				
				kubeconfig = localKubeconfig
			}
		}

		// Check if tunnel is needed and running (when using local kubeconfig, and not just showing help)
		if !isHelpRequest && kubeconfig == localKubeconfig {
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

		// If recipe succeeded and --host was specified, auto-add domain to Caddy
		if hostArg != "" {
			// Only do this when running locally (not on the server)
			if _, err := os.Stat("/etc/caddy/Caddyfile"); err != nil {
				remoteBin := filepath.Join(projectRoot, "bin", "netcup-kube")
				if _, err := os.Stat(remoteBin); err == nil {
					fmt.Printf("\nAdding %s to Caddy edge-http domains...\n", hostArg)
					
					// Create temp env file with CONFIRM=true
					tmpEnv, err := os.CreateTemp("", "netcup-kube-install.env.*")
					if err == nil {
						defer os.Remove(tmpEnv.Name())
						tmpEnv.WriteString("CONFIRM=true\n")
						tmpEnv.Close()
						
						// Run the domain add command
						dnsCmd := exec.Command(remoteBin, "remote", "run", "--no-tty", "--env-file", tmpEnv.Name(), "dns", "--type", "edge-http", "--add-domains", hostArg)
						dnsCmd.Stdout = os.Stdout
						dnsCmd.Stderr = os.Stderr
						
						if err := dnsCmd.Run(); err == nil {
							fmt.Println("✓ Domain added successfully!")
						} else {
							fmt.Printf("⚠ Failed to add domain automatically. Run manually:\n")
							fmt.Printf("  CONFIRM=true bin/netcup-kube remote run --no-tty dns --type edge-http --add-domains \"%s\"\n", hostArg)
						}
					}
				}
			}
		}

		return nil
	},
}

func fetchKubeconfig(envFile, localKubeconfig, configDir string) error {
	// Check if env file exists
	if _, err := os.Stat(envFile); err != nil {
		return fmt.Errorf("ERROR: %s not found. Please create it from the example", envFile)
	}

	// Load env file to get MGMT_HOST and MGMT_USER
	env, err := loadEnvFile(envFile)
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
	env, err := loadEnvFile(envFile)
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

	// Check if tunnel is running using the control socket
	ctlSocket := getTunnelControlSocket(remoteUser, remoteHost, tunnelPort)
	
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", remoteUser, remoteHost))
	checkCmd.Stdout = nil
	checkCmd.Stderr = nil
	
	if err := checkCmd.Run(); err != nil {
		// Tunnel not running, start it
		fmt.Println("SSH tunnel not running. Starting tunnel...")
		
		tunnelScript := filepath.Join(projectRoot, "bin", "netcup-kube-tunnel")
		if _, err := os.Stat(tunnelScript); err != nil {
			// Fall back to using the Go tunnel command if available
			return startTunnelGo(remoteHost, remoteUser, tunnelPort)
		}
		
		startCmd := exec.Command(tunnelScript, "start")
		if err := startCmd.Run(); err != nil {
			return fmt.Errorf("failed to start SSH tunnel: %w", err)
		}
		
		// Give tunnel a moment to establish
		exec.Command("sleep", "1").Run()
		
		// Verify tunnel is now running
		checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", remoteUser, remoteHost))
		if err := checkCmd.Run(); err != nil {
			return fmt.Errorf("ERROR: Failed to start SSH tunnel")
		}
	}
	
	fmt.Printf("Using tunnel: localhost:%s -> %s:6443\n", tunnelPort, remoteHost)
	return nil
}

func startTunnelGo(host, user, localPort string) error {
	// This will be used when the tunnel command is integrated
	// For now, return an error suggesting to use the tunnel command
	return fmt.Errorf("tunnel not running. Please start it with: netcup-kube tunnel start")
}
