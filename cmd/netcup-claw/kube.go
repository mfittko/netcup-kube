package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// probeKubeAPI checks if the local Kubernetes API is reachable.
// It reads KUBECONFIG (or uses the default) and tries to reach the API server.
func probeKubeAPI() bool {
	// Quick HTTP probe to the kube API server configured in kubeconfig
	// We use a short timeout to avoid blocking the UX.
	client := &http.Client{Timeout: 2 * time.Second}
	kubeHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	if kubeHost == "" {
		kubeHost = "127.0.0.1"
	}
	kubePort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if kubePort == "" {
		kubePort = "6443"
	}
	url := fmt.Sprintf("https://%s:%s/livez", kubeHost, kubePort)
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode < 500
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
