package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles SSH tunnel operations
type Manager struct {
	User       string
	Host       string
	LocalPort  string
	RemoteHost string
	RemotePort string
}

// New creates a new tunnel manager
func New(user, host, localPort, remoteHost, remotePort string) *Manager {
	return &Manager{
		User:       user,
		Host:       host,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
	}
}

// GetControlSocket returns the path to the SSH ControlMaster socket for the tunnel
func (m *Manager) GetControlSocket() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}

	key := fmt.Sprintf("%s@%s-%s", m.User, m.Host, m.LocalPort)
	key = strings.ReplaceAll(key, "@", "_")
	key = strings.ReplaceAll(key, ":", "_")
	key = strings.ReplaceAll(key, "/", "_")

	return filepath.Join(base, fmt.Sprintf("netcup-kube-tunnel-%s.ctl", key))
}

// IsRunning checks if the tunnel is currently running
func (m *Manager) IsRunning() bool {
	ctlSocket := m.GetControlSocket()
	checkCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "check", fmt.Sprintf("%s@%s", m.User, m.Host))
		checkCmd.Stderr = nil
	return checkCmd.Run() == nil
}

// Start starts the SSH tunnel
func (m *Manager) Start() error {
	// Check if already running
	if m.IsRunning() {
		return nil
	}

	// Check if port is already in use
	if portInUse(m.LocalPort) {
		return fmt.Errorf("localhost:%s is already in use", m.LocalPort)
	}

	// Start the tunnel
	ctlSocket := m.GetControlSocket()
	tunnelCmd := exec.Command("ssh",
		"-M", "-S", ctlSocket,
		"-fN",
		"-L", fmt.Sprintf("%s:%s:%s", m.LocalPort, m.RemoteHost, m.RemotePort),
		fmt.Sprintf("%s@%s", m.User, m.Host),
		"-o", "ControlPersist=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	)

	if err := tunnelCmd.Run(); err != nil {
		return fmt.Errorf("failed to start tunnel: %w", err)
	}

	return nil
}

// Stop stops the SSH tunnel
func (m *Manager) Stop() error {
	if !m.IsRunning() {
		return nil
	}

	ctlSocket := m.GetControlSocket()
	exitCmd := exec.Command("ssh", "-S", ctlSocket, "-O", "exit", fmt.Sprintf("%s@%s", m.User, m.Host))
	exitCmd.Stdout = nil
	exitCmd.Stderr = nil
	exitCmd.Run() // Ignore errors

	return nil
}

// Status returns information about the tunnel status
func (m *Manager) Status() (running bool, listenPort string) {
	running = m.IsRunning()
	if running {
		listenPort = m.LocalPort
	}
	return
}

// portInUse checks if a local port is in use
func portInUse(port string) bool {
	// Try lsof first
	cmd := exec.Command("sh", "-c", fmt.Sprintf("command -v lsof > /dev/null && lsof -nP -iTCP:%s -sTCP:LISTEN", port))
	if err := cmd.Run(); err == nil {
		return true
	}

	// Fallback to ss
	cmd = exec.Command("sh", "-c", fmt.Sprintf("command -v ss > /dev/null && ss -ltn '( sport = :%s )' | tail -n +2 | grep -q .", port))
	return cmd.Run() == nil
}

