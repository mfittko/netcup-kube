package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var (
	tunnelHost       string
	tunnelUser       string
	tunnelLocalPort  string
	tunnelRemoteHost string
	tunnelRemotePort string
	tunnelEnvFile    string
	tunnelNoEnv      bool
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel [start|stop|status]",
	Short: "Manage SSH tunnel for local kubectl access",
	Long: `Manage SSH tunnel for local kubectl access to the remote k3s cluster.

This command manages an SSH tunnel using ControlMaster for reliable start/stop/status
operations. The tunnel forwards local port (default 6443) to the k3s API server
on the remote host.

Commands:
  start   - Start SSH tunnel (default command)
  stop    - Stop SSH tunnel
  status  - Check tunnel status

Examples:
  netcup-kube tunnel
  netcup-kube tunnel start --host example.com --user ops
  netcup-kube tunnel stop
  netcup-kube tunnel status`,
	RunE: func(cmd *cobra.Command, args []string) error {
		action := "start"
		if len(args) > 0 {
			action = args[0]
			if action != "start" && action != "stop" && action != "status" {
				return fmt.Errorf("unknown command: %s (valid: start, stop, status)", action)
			}
		}

		// Load environment
		if err := loadTunnelEnv(); err != nil {
			return err
		}

		// Apply defaults
		if tunnelHost == "" {
			tunnelHost = os.Getenv("TUNNEL_HOST")
			if tunnelHost == "" {
				// Try MGMT_HOST as fallback
				tunnelHost = os.Getenv("MGMT_HOST")
				if tunnelHost == "" {
					tunnelHost = os.Getenv("MGMT_IP")
				}
			}
		}
		if tunnelUser == "" {
			tunnelUser = os.Getenv("TUNNEL_USER")
			if tunnelUser == "" {
				tunnelUser = os.Getenv("MGMT_USER")
				if tunnelUser == "" {
					tunnelUser = "ops"
				}
			}
		}
		if tunnelLocalPort == "" {
			tunnelLocalPort = os.Getenv("TUNNEL_LOCAL_PORT")
			if tunnelLocalPort == "" {
				tunnelLocalPort = "6443"
			}
		}
		if tunnelRemoteHost == "" {
			tunnelRemoteHost = os.Getenv("TUNNEL_REMOTE_HOST")
			if tunnelRemoteHost == "" {
				tunnelRemoteHost = "127.0.0.1"
			}
		}
		if tunnelRemotePort == "" {
			tunnelRemotePort = os.Getenv("TUNNEL_REMOTE_PORT")
			if tunnelRemotePort == "" {
				tunnelRemotePort = "6443"
			}
		}

		if tunnelHost == "" {
			return fmt.Errorf("no host provided and no TUNNEL_HOST/MGMT_HOST found in config")
		}

		// Execute action
		switch action {
		case "start":
			return tunnelStart()
		case "stop":
			return tunnelStop()
		case "status":
			return tunnelStatus()
		default:
			return fmt.Errorf("unknown action: %s", action)
		}
	},
}

func loadTunnelEnv() error {
	if tunnelNoEnv {
		return nil
	}

	envPath := tunnelEnvFile
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
		if tunnelEnvFile != "" {
			// User explicitly specified a file that doesn't exist
			return fmt.Errorf("env file not found: %s", envPath)
		}
		// Default file doesn't exist, that's OK
		return nil
	}

	// Load the env file
	env, err := loadEnvFile(envPath)
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

func tunnelStart() error {
	ctlSocket := getTunnelControlSocket(tunnelUser, tunnelHost, tunnelLocalPort)

	// Check if already running
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", tunnelUser, tunnelHost))
	checkCmd.Stdout = nil
	checkCmd.Stderr = nil

	if err := checkCmd.Run(); err == nil {
		fmt.Printf("Tunnel already running on localhost:%s -> %s:%s via %s@%s\n",
			tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort, tunnelUser, tunnelHost)
		return nil
	}

	// Check if port is already in use
	if portInUse(tunnelLocalPort) {
		return fmt.Errorf("ERROR: localhost:%s is already in use. Stop the existing process or choose a different --local-port", tunnelLocalPort)
	}

	// Start the tunnel
	fmt.Printf("Starting tunnel on localhost:%s -> %s:%s via %s@%s\n",
		tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort, tunnelUser, tunnelHost)

	sshCmd := exec.Command("ssh",
		"-M", "-S", ctlSocket,
		"-fN",
		"-L", fmt.Sprintf("%s:%s:%s", tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort),
		fmt.Sprintf("%s@%s", tunnelUser, tunnelHost),
		"-o", "ControlPersist=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	)

	if err := sshCmd.Run(); err != nil {
		return fmt.Errorf("failed to start tunnel: %w", err)
	}

	fmt.Printf("Started tunnel on localhost:%s -> %s:%s via %s@%s\n",
		tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort, tunnelUser, tunnelHost)

	return nil
}

func tunnelStop() error {
	ctlSocket := getTunnelControlSocket(tunnelUser, tunnelHost, tunnelLocalPort)

	// Check if running
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", tunnelUser, tunnelHost))
	checkCmd.Stdout = nil
	checkCmd.Stderr = nil

	if err := checkCmd.Run(); err != nil {
		fmt.Printf("No tunnel running for localhost:%s via %s@%s.\n", tunnelLocalPort, tunnelUser, tunnelHost)
		return nil
	}

	// Stop the tunnel
	exitCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "exit", fmt.Sprintf("%s@%s", tunnelUser, tunnelHost))
	exitCmd.Stdout = nil
	exitCmd.Stderr = nil
	exitCmd.Run() // Ignore errors

	fmt.Printf("Stopped tunnel on localhost:%s via %s@%s.\n", tunnelLocalPort, tunnelUser, tunnelHost)
	return nil
}

func tunnelStatus() error {
	ctlSocket := getTunnelControlSocket(tunnelUser, tunnelHost, tunnelLocalPort)

	// Check status
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", tunnelUser, tunnelHost))
	output, err := checkCmd.CombinedOutput()

	if err == nil || strings.Contains(string(output), "Master running") {
		fmt.Printf("running:  localhost:%s -> %s:%s via %s@%s\n",
			tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort, tunnelUser, tunnelHost)
		fmt.Printf("socket:   %s\n", ctlSocket)
		fmt.Printf("control:  %s\n", strings.TrimSpace(string(output)))

		// Show what's listening on the local port
		showPortListeners(tunnelLocalPort)

		return nil
	}

	fmt.Printf("not running: localhost:%s -> %s:%s via %s@%s\n",
		tunnelLocalPort, tunnelRemoteHost, tunnelRemotePort, tunnelUser, tunnelHost)
	fmt.Printf("socket:      %s\n", ctlSocket)
	if len(output) > 0 {
		fmt.Printf("control:     %s\n", strings.TrimSpace(string(output)))
	} else {
		fmt.Printf("control:     <no output>\n")
	}

	if portInUse(tunnelLocalPort) {
		fmt.Printf("listen:      yes (something is bound to localhost:%s)\n", tunnelLocalPort)
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
	tunnelCmd.Flags().StringVar(&tunnelHost, "host", "", "Target SSH host")
	tunnelCmd.Flags().StringVar(&tunnelUser, "user", "", "SSH user")
	tunnelCmd.Flags().StringVar(&tunnelLocalPort, "local-port", "", "Local port to bind")
	tunnelCmd.Flags().StringVar(&tunnelRemoteHost, "remote-host", "", "Remote host to forward to")
	tunnelCmd.Flags().StringVar(&tunnelRemotePort, "remote-port", "", "Remote port to forward to")
	tunnelCmd.Flags().StringVar(&tunnelEnvFile, "env-file", "", "Load env file")
	tunnelCmd.Flags().BoolVar(&tunnelNoEnv, "no-env", false, "Skip loading env file")
}
