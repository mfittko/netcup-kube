package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mfittko/netcup-kube/internal/tunnel"
)

// probeKubeAPI checks if the local Kubernetes API is reachable by running
// kubectl with a short request timeout. This is kubeconfig-aware and handles
// TLS/auth automatically, avoiding false negatives from raw HTTP probes.
func probeKubeAPI() bool {
	cmd := exec.Command("kubectl", "--request-timeout=3s", "get", "--raw=/livez")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func ensureKubeAPIReachableWithTunnel() error {
	if probeKubeAPI() {
		return nil
	}

	tun := tunnelConfig()
	if strings.TrimSpace(tun.Host) == "" {
		return fmt.Errorf("kube API is unreachable and no tunnel host configured (set TUNNEL_HOST or --tunnel-host)")
	}

	mgr := tunnel.New(tun.User, tun.Host, tun.LocalPort, tun.RemoteHost, tun.RemotePort)
	if !mgr.IsRunning() {
		fmt.Fprintf(os.Stderr, "kube API unreachable; starting SSH tunnel via %s@%s...\n", tun.User, tun.Host)
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start SSH tunnel: %w", err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if probeKubeAPI() {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	return fmt.Errorf("kube API still unreachable after tunnel recovery")
}

// runKubectl runs kubectl with the given arguments, connecting stdio
func runKubectl(args ...string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if recoverErr := ensureKubeAPIReachableWithTunnel(); recoverErr == nil {
			retry := exec.Command("kubectl", args...)
			retry.Stdin = os.Stdin
			retry.Stdout = os.Stdout
			retry.Stderr = os.Stderr
			if retryErr := retry.Run(); retryErr == nil {
				return nil
			}
		}
		return fmt.Errorf("kubectl error: %w", err)
	}
	return nil
}

// runKubectlOutput runs kubectl and returns combined output bytes.
func runKubectlOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if recoverErr := ensureKubeAPIReachableWithTunnel(); recoverErr == nil {
			retry := exec.Command("kubectl", args...)
			var retryStderr bytes.Buffer
			retry.Stderr = &retryStderr
			retryOut, retryErr := retry.Output()
			if retryErr == nil {
				return retryOut, nil
			}
			return nil, fmt.Errorf("kubectl error: %w (stderr: %s)", retryErr, strings.TrimSpace(retryStderr.String()))
		}
		return nil, fmt.Errorf("kubectl error: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// runKubectlCombinedOutput runs kubectl and returns combined stdout/stderr.
func runKubectlCombinedOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if recoverErr := ensureKubeAPIReachableWithTunnel(); recoverErr == nil {
			retry := exec.Command("kubectl", args...)
			retryOut, retryErr := retry.CombinedOutput()
			if retryErr == nil {
				return retryOut, nil
			}
			return retryOut, fmt.Errorf("kubectl error: %w", retryErr)
		}
		return out, fmt.Errorf("kubectl error: %w", err)
	}
	return out, nil
}
