package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mfittko/netcup-kube/internal/config"
	"github.com/mfittko/netcup-kube/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	sshHost       string
	sshUser       string
	sshEnvFile    string
	sshNoEnv      bool
	sshLocalPort  string
	sshRemoteHost string
	sshRemotePort string
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Open SSH shell or manage SSH tunnel for kubectl access",
	Long: `Open an interactive SSH shell to the management node or manage an SSH tunnel.

Without subcommand:
  Opens an interactive SSH shell to the management node.

With tunnel subcommand:
  Manages an SSH tunnel using ControlMaster for reliable start/stop/status operations.
  The tunnel forwards local port (default 6443) to the k3s API server on the remote host.

Examples:
  # Open interactive SSH shell
  netcup-kube ssh
  netcup-kube ssh --host example.com --user ops

  # Manage tunnel
  netcup-kube ssh tunnel start
  netcup-kube ssh tunnel stop
  netcup-kube ssh tunnel status
  netcup-kube ssh tunnel start --local-port 6443`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load environment and apply defaults
		if err := loadSSHDefaults(); err != nil {
			return err
		}

		if sshHost == "" {
			return fmt.Errorf("no host provided and no TUNNEL_HOST/MGMT_HOST found in config")
		}

		// Open interactive SSH shell
		return openSSHShell()
	},
}

// SSH tunnel subcommand
var sshTunnelCmd = &cobra.Command{
	Use:   "tunnel [start|stop|status]",
	Short: "Manage SSH tunnel for kubectl access",
	Long: `Manage an SSH tunnel using ControlMaster for reliable start/stop/status operations.

The tunnel forwards local port (default 6443) to the k3s API server on the remote host.

Commands:
  start   - Start SSH tunnel (default if no command specified)
  stop    - Stop SSH tunnel
  status  - Check tunnel status

Examples:
  netcup-kube ssh tunnel start
  netcup-kube ssh tunnel stop
  netcup-kube ssh tunnel status
  netcup-kube ssh tunnel start --local-port 6443`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load environment and apply defaults
		if err := loadSSHDefaults(); err != nil {
			return err
		}

		if sshHost == "" {
			return fmt.Errorf("no host provided and no TUNNEL_HOST/MGMT_HOST found in config")
		}

		// Apply tunnel-specific defaults
		if sshLocalPort == "" {
			sshLocalPort = os.Getenv("TUNNEL_LOCAL_PORT")
			if sshLocalPort == "" {
				sshLocalPort = "6443"
			}
		}
		if sshRemoteHost == "" {
			sshRemoteHost = os.Getenv("TUNNEL_REMOTE_HOST")
			if sshRemoteHost == "" {
				sshRemoteHost = "127.0.0.1"
			}
		}
		if sshRemotePort == "" {
			sshRemotePort = os.Getenv("TUNNEL_REMOTE_PORT")
			if sshRemotePort == "" {
				sshRemotePort = "6443"
			}
		}

		// Determine action
		action := "start" // default
		if len(args) > 0 {
			action = args[0]
			if action != "start" && action != "stop" && action != "status" {
				return fmt.Errorf("unknown tunnel command: %s (valid: start, stop, status)", action)
			}
		}

		// Execute tunnel action
		switch action {
		case "start":
			return sshTunnelStart()
		case "stop":
			return sshTunnelStop()
		case "status":
			return sshTunnelStatus()
		default:
			return fmt.Errorf("unknown tunnel action: %s", action)
		}
	},
}

func loadSSHEnv() error {
	if sshNoEnv {
		return nil
	}

	envPath := sshEnvFile
	if envPath == "" {
		// Try default locations
		if _, err := os.Stat("config/netcup-kube.env"); err == nil {
			envPath = "config/netcup-kube.env"
		} else if _, err := os.Stat(".env"); err == nil {
			envPath = ".env"
		}
	}

	if envPath == "" {
		return nil
	}

	if _, err := os.Stat(envPath); err != nil {
		if sshEnvFile != "" {
			// User explicitly specified a file that doesn't exist
			return fmt.Errorf("env file not found: %s", envPath)
		}
		// Default file doesn't exist, that's OK
		return nil
	}

	// Load the env file
	env, err := config.LoadEnvFileToMap(envPath)
	if err != nil {
		return fmt.Errorf("failed to load env file: %w", err)
	}

	// Set environment variables
	for k, v := range env {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}

	return nil
}

func openSSHShell() error {
	fmt.Printf("Opening SSH shell to %s@%s\n", sshUser, sshHost)

	sshCmd := exec.Command("ssh", fmt.Sprintf("%s@%s", sshUser, sshHost))
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return sshCmd.Run()
}

func sshTunnelStart() error {
	mgr := tunnel.New(sshUser, sshHost, sshLocalPort, sshRemoteHost, sshRemotePort)

	// Check if already running
	if mgr.IsRunning() {
		fmt.Printf("Tunnel already running on localhost:%s -> %s:%s via %s@%s\n",
			sshLocalPort, sshRemoteHost, sshRemotePort, sshUser, sshHost)
		return nil
	}

	// Start the tunnel
	fmt.Printf("Starting tunnel on localhost:%s -> %s:%s via %s@%s\n",
		sshLocalPort, sshRemoteHost, sshRemotePort, sshUser, sshHost)

	if err := mgr.Start(); err != nil {
		if strings.Contains(err.Error(), "already in use") {
			return fmt.Errorf("ERROR: localhost:%s is already in use. Stop the existing process or choose a different --local-port", sshLocalPort)
		}
		return fmt.Errorf("failed to start tunnel: %w", err)
	}

	fmt.Printf("Started tunnel on localhost:%s -> %s:%s via %s@%s\n",
		sshLocalPort, sshRemoteHost, sshRemotePort, sshUser, sshHost)

	return nil
}

func sshTunnelStop() error {
	mgr := tunnel.New(sshUser, sshHost, sshLocalPort, sshRemoteHost, sshRemotePort)

	// Check if running
	if !mgr.IsRunning() {
		fmt.Printf("No tunnel running for localhost:%s via %s@%s.\n", sshLocalPort, sshUser, sshHost)
		return nil
	}

	// Stop the tunnel
	if err := mgr.Stop(); err != nil {
		return fmt.Errorf("failed to stop tunnel: %w", err)
	}

	fmt.Printf("Stopped tunnel on localhost:%s via %s@%s.\n", sshLocalPort, sshUser, sshHost)
	return nil
}

func sshTunnelStatus() error {
	mgr := tunnel.New(sshUser, sshHost, sshLocalPort, sshRemoteHost, sshRemotePort)
	ctlSocket := mgr.GetControlSocket()

	// Check status
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", sshUser, sshHost))
	output, err := checkCmd.CombinedOutput()

	if err == nil || strings.Contains(string(output), "Master running") {
		fmt.Printf("running:  localhost:%s -> %s:%s via %s@%s\n",
			sshLocalPort, sshRemoteHost, sshRemotePort, sshUser, sshHost)
		fmt.Printf("socket:   %s\n", ctlSocket)
		fmt.Printf("control:  %s\n", strings.TrimSpace(string(output)))

		// Show what's listening on the local port
		showPortListeners(sshLocalPort)

		return nil
	}

	fmt.Printf("not running: localhost:%s -> %s:%s via %s@%s\n",
		sshLocalPort, sshRemoteHost, sshRemotePort, sshUser, sshHost)
	fmt.Printf("socket:      %s\n", ctlSocket)
	if len(output) > 0 {
		fmt.Printf("control:     %s\n", strings.TrimSpace(string(output)))
	} else {
		fmt.Printf("control:     <no output>\n")
	}

	if portInUse(sshLocalPort) {
		fmt.Printf("listen:      yes (something is bound to localhost:%s)\n", sshLocalPort)
	} else {
		fmt.Printf("listen:      no\n")
	}

	return fmt.Errorf("tunnel not running")
}

func portInUse(port string) bool {
	// Try to detect OS and use appropriate command
	var cmd *exec.Cmd

	// Try lsof (macOS and some Linux)
	if _, err := exec.LookPath("lsof"); err == nil {
		cmd = exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err == nil {
			return true
		}
	}

	// Try ss (Linux)
	if _, err := exec.LookPath("ss"); err == nil {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("ss -ltn '( sport = :%s )' | tail -n +2 | grep -q .", port))
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err == nil {
			return true
		}
	}

	return false
}

func showPortListeners(port string) {
	// Try lsof first (macOS)
	if _, err := exec.LookPath("lsof"); err == nil {
		cmd := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN")
		output, err := cmd.CombinedOutput()
		if err == nil && len(output) > 0 {
			fmt.Printf("listen:\n%s", string(output))
			return
		}
	}

	// Try ss (Linux)
	if _, err := exec.LookPath("ss"); err == nil {
		cmd := exec.Command("ss", "-ltnp", fmt.Sprintf("( sport = :%s )", port))
		output, err := cmd.CombinedOutput()
		if err == nil && len(output) > 0 {
			fmt.Printf("listen:\n%s", string(output))
			return
		}
	}
}

func init() {
	// Add flags to main ssh command
	sshCmd.PersistentFlags().StringVar(&sshHost, "host", "", "Target SSH host")
	sshCmd.PersistentFlags().StringVar(&sshUser, "user", "", "SSH user")
	sshCmd.PersistentFlags().StringVar(&sshEnvFile, "env-file", "", "Load env file")
	sshCmd.PersistentFlags().BoolVar(&sshNoEnv, "no-env", false, "Skip loading env file")

	// Add flags specific to tunnel subcommand
	sshTunnelCmd.Flags().StringVar(&sshLocalPort, "local-port", "", "Local port to bind")
	sshTunnelCmd.Flags().StringVar(&sshRemoteHost, "remote-host", "", "Remote host to forward to")
	sshTunnelCmd.Flags().StringVar(&sshRemotePort, "remote-port", "", "Remote port to forward to")

	// Add tunnel as a subcommand of ssh
	sshCmd.AddCommand(sshTunnelCmd)
}

// loadSSHDefaults loads environment variables and applies defaults for SSH host and user
func loadSSHDefaults() error {
	// Load environment
	if err := loadSSHEnv(); err != nil {
		return err
	}

	// Apply defaults for host
	if sshHost == "" {
		sshHost = os.Getenv("TUNNEL_HOST")
		if sshHost == "" {
			sshHost = os.Getenv("MGMT_HOST")
			if sshHost == "" {
				sshHost = os.Getenv("MGMT_IP")
			}
		}
	}

	// Apply defaults for user
	if sshUser == "" {
		sshUser = os.Getenv("TUNNEL_USER")
		if sshUser == "" {
			sshUser = os.Getenv("MGMT_USER")
			if sshUser == "" {
				sshUser = "ops"
			}
		}
	}

	return nil
}
