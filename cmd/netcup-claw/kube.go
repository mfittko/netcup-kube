package main

import (
	"fmt"
	"os"
	"os/exec"
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

// runKubectl runs kubectl with the given arguments, connecting stdio
func runKubectl(args ...string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl error: %w", err)
	}
	return nil
}
