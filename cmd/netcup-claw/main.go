package main

import (
	"fmt"
	"os"
	"time"

	"github.com/mfittko/netcup-kube/internal/openclaw"
	"github.com/mfittko/netcup-kube/internal/portforward"
	"github.com/spf13/cobra"
)

var (
	version = "dev"

	// Port-forward flags
	pfNamespace  string
	pfLocalPort  string
	pfRemotePort string
)

var rootCmd = &cobra.Command{
	Use:     "netcup-claw",
	Short:   "OpenClaw operational access CLI (tunnel-aware port-forward + pod ops)",
	Version: version,
	Long: `netcup-claw abstracts OpenClaw operational access tasks including
port-forwarding, pod command execution, logs, and health/status checks.

It automatically bootstraps the SSH tunnel when the Kubernetes API is
unreachable, providing a first-class operator experience.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// portForwardCmd is the top-level "port-forward" command
var portForwardCmd = &cobra.Command{
	Use:   "port-forward",
	Short: "Manage OpenClaw port-forward lifecycle",
	Long: `Manage the background kubectl port-forward to the OpenClaw service.

Sub-commands:
  start   - Start port-forward (idempotent; auto-starts tunnel if needed)
  stop    - Stop port-forward
  status  - Show port-forward status`,
}

var portForwardStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start background port-forward to OpenClaw",
	Long: `Start a background kubectl port-forward to the OpenClaw service.

Steps:
  1. Probe local Kubernetes API reachability
  2. If unreachable, ensure SSH tunnel is running
  3. Resolve OpenClaw service target (label lookup with fallback)
  4. Start background kubectl port-forward
  5. Validate local port readiness`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		resolver := openclaw.New(cfg, nil)

		// Resolve the service target
		svcTarget, err := resolver.ResolveService()
		if err != nil {
			return fmt.Errorf("failed to resolve OpenClaw service: %w", err)
		}

		mgr := pfManager(cfg)

		// Start (idempotent)
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start port-forward: %w", err)
		}

		st := mgr.Status()
		if st.State == portforward.StateRunning {
			fmt.Printf("port-forward running: localhost:%s -> %s in namespace %s (pid %d)\n",
				cfg.LocalPort, svcTarget, cfg.Namespace, st.PID)
			if st.LogFile != "" {
				fmt.Printf("log: %s\n", st.LogFile)
			}

			// Brief readiness probe
			if probeErr := portforward.ReadinessCheck(cfg.LocalPort, 3*time.Second); probeErr != nil {
				fmt.Fprintf(os.Stderr, "warning: port-forward started but local port not yet ready: %v\n", probeErr)
			}
		}
		return nil
	},
}

var portForwardStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop background port-forward",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		mgr := pfManager(cfg)

		if err := mgr.Stop(); err != nil {
			return fmt.Errorf("failed to stop port-forward: %w", err)
		}

		fmt.Printf("port-forward stopped (namespace: %s, port: %s)\n", cfg.Namespace, cfg.LocalPort)
		return nil
	},
}

var portForwardStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show port-forward status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		mgr := pfManager(cfg)
		st := mgr.Status()

		fmt.Printf("state:      %s\n", st.State)
		fmt.Printf("namespace:  %s\n", cfg.Namespace)
		fmt.Printf("port:       %s\n", cfg.LocalPort)
		if st.PID > 0 {
			fmt.Printf("pid:        %d\n", st.PID)
		}
		if st.LogFile != "" {
			fmt.Printf("log:        %s\n", st.LogFile)
		}

		if st.State != portforward.StateRunning {
			return fmt.Errorf("port-forward is not running (state: %s)", st.State)
		}
		return nil
	},
}

// runCmd executes an OpenClaw command on the main pod
var runCmd = &cobra.Command{
	Use:   "run <subcommand> [args...]",
	Short: "Run an OpenClaw subcommand on the main pod",
	Long: `Execute an OpenClaw command context on the main OpenClaw pod.

The subcommand and its arguments are passed through to the pod via kubectl exec.

Examples:
  netcup-claw run status
  netcup-claw run --help`,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		resolver := openclaw.New(cfg, nil)

		pod, err := resolver.ResolvePod()
		if err != nil {
			return fmt.Errorf("failed to resolve OpenClaw pod: %w", err)
		}

		execArgs := append([]string{
			"-n", cfg.Namespace,
			"exec", pod, "--",
		}, args...)

		return runKubectl(execArgs...)
	},
}

// logsCmd streams or fetches logs from the OpenClaw pod
var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Fetch or stream logs from the OpenClaw pod",
	Long: `Fetch or stream logs from the OpenClaw workload pod.

Flags are passed through to kubectl logs.

Examples:
  netcup-claw logs
  netcup-claw logs --follow
  netcup-claw logs --tail 100`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		resolver := openclaw.New(cfg, nil)

		pod, err := resolver.ResolvePod()
		if err != nil {
			return fmt.Errorf("failed to resolve OpenClaw pod: %w", err)
		}

		logArgs := append([]string{"-n", cfg.Namespace, "logs", pod}, args...)
		return runKubectl(logArgs...)
	},
}

// statusCmd shows a unified status view
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show unified OpenClaw status (tunnel, port-forward, service health)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()

		// 1. Kubernetes API reachability
		apiReachable := probeKubeAPI()
		fmt.Printf("kube-api:     %s\n", boolStatus(apiReachable))

		// 2. Port-forward status
		mgr := pfManager(cfg)
		pfStatus := mgr.Status()
		fmt.Printf("port-forward: %s", pfStatus.State)
		if pfStatus.PID > 0 {
			fmt.Printf(" (pid %d)", pfStatus.PID)
		}
		fmt.Println()

		// 3. OpenClaw service resolution
		resolver := openclaw.New(cfg, nil)
		svc, svcErr := resolver.ResolveService()
		if svcErr != nil {
			fmt.Printf("service:      error (%v)\n", svcErr)
		} else {
			fmt.Printf("service:      %s\n", svc)
		}

		_, podErr := resolver.ResolvePod()
		if podErr != nil {
			fmt.Printf("pod:          not found\n")
		} else {
			fmt.Printf("pod:          found\n")
		}

		// Overall health
		healthy := apiReachable && pfStatus.State == portforward.StateRunning && svcErr == nil && podErr == nil
		fmt.Printf("healthy:      %s\n", boolStatus(healthy))

		if !healthy {
			return fmt.Errorf("OpenClaw is not fully healthy")
		}
		return nil
	},
}

func init() {
	// Port-forward flags
	portForwardCmd.PersistentFlags().StringVarP(&pfNamespace, "namespace", "n", "", "Kubernetes namespace (default: openclaw)")
	portForwardCmd.PersistentFlags().StringVar(&pfLocalPort, "local-port", "", "Local port (default: 18789)")
	portForwardCmd.PersistentFlags().StringVar(&pfRemotePort, "remote-port", "", "Remote port (default: 18789)")

	portForwardCmd.AddCommand(portForwardStartCmd)
	portForwardCmd.AddCommand(portForwardStopCmd)
	portForwardCmd.AddCommand(portForwardStatusCmd)

	rootCmd.AddCommand(portForwardCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(statusCmd)
}

// openclawConfig builds the openclaw.Config from flags and environment
func openclawConfig() openclaw.Config {
	cfg := openclaw.DefaultConfig()
	if pfNamespace != "" {
		cfg.Namespace = pfNamespace
	} else if ns := os.Getenv("OPENCLAW_NAMESPACE"); ns != "" {
		cfg.Namespace = ns
	}
	if pfLocalPort != "" {
		cfg.LocalPort = pfLocalPort
	} else if lp := os.Getenv("OPENCLAW_LOCAL_PORT"); lp != "" {
		cfg.LocalPort = lp
	}
	if pfRemotePort != "" {
		cfg.RemotePort = pfRemotePort
	} else if rp := os.Getenv("OPENCLAW_REMOTE_PORT"); rp != "" {
		cfg.RemotePort = rp
	}
	return cfg
}

// pfManager creates a port-forward Manager from the openclaw config
func pfManager(cfg openclaw.Config) *portforward.Manager {
	return portforward.New(cfg.Namespace, cfg.FallbackSvc, cfg.LocalPort, cfg.RemotePort)
}

// boolStatus returns "ok" or "not ok" for boolean health values
func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "not ok"
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
