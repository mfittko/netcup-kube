package portforward

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// State represents the port-forward lifecycle state
type State string

const (
	// StateStopped means port-forward is not running
	StateStopped State = "stopped"
	// StateStarting means port-forward is being started
	StateStarting State = "starting"
	// StateRunning means port-forward is running
	StateRunning State = "running"
	// StateFailed means port-forward failed to start or died unexpectedly
	StateFailed State = "failed"
)

// Status holds port-forward status information
type Status struct {
	State     State  `json:"state"`
	PID       int    `json:"pid,omitempty"`
	LocalPort string `json:"local_port"`
	LogFile   string `json:"log_file,omitempty"`
}

// stateFile is the on-disk representation of port-forward state
type stateFile struct {
	State     State  `json:"state"`
	PID       int    `json:"pid,omitempty"`
	LocalPort string `json:"local_port"`
	LogFile   string `json:"log_file,omitempty"`
}

// Manager handles the lifecycle of a background kubectl port-forward process.
type Manager struct {
	Namespace  string
	Target     string
	LocalPort  string
	RemotePort string

	// stateDir is the directory for PID/log/state files. Defaults to /tmp.
	stateDir string

	// execFunc allows injection for testing
	execFunc StartFunc

	// processChecker allows injection for testing
	processChecker ProcessChecker
}

// StartFunc launches the kubectl port-forward process and returns its PID.
// The function receives: namespace, target, localPort, remotePort, logFilePath.
type StartFunc func(namespace, target, localPort, remotePort, logFile string) (int, error)

// ProcessChecker checks if a process is alive
type ProcessChecker func(pid int) bool

// Option is a functional option for Manager
type Option func(*Manager)

// WithStateDir sets the state directory for PID/log/state files
func WithStateDir(dir string) Option {
	return func(m *Manager) {
		m.stateDir = dir
	}
}

// WithStartFunc sets a custom start function (for testing)
func WithStartFunc(fn StartFunc) Option {
	return func(m *Manager) {
		m.execFunc = fn
	}
}

// WithProcessChecker sets a custom process checker (for testing)
func WithProcessChecker(fn ProcessChecker) Option {
	return func(m *Manager) {
		m.processChecker = fn
	}
}

// New creates a new port-forward Manager
func New(namespace, target, localPort, remotePort string, opts ...Option) *Manager {
	m := &Manager{
		Namespace:      namespace,
		Target:         target,
		LocalPort:      localPort,
		RemotePort:     remotePort,
		stateDir:       defaultStateDir(),
		execFunc:       defaultStartFunc,
		processChecker: defaultProcessChecker,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start starts the port-forward in the background. It is idempotent: if already
// running, it returns immediately without starting a duplicate process.
func (m *Manager) Start() error {
	// Check if already running
	st, _ := m.readState()
	if st != nil && (st.State == StateRunning || st.State == StateStarting) {
		if st.PID > 0 && m.processChecker(st.PID) {
			return nil // Already running, idempotent
		}
	}

	if isPortListening(m.LocalPort) {
		return fmt.Errorf("local port %s is already in use; stop the existing forward or use a different local port", m.LocalPort)
	}

	// Transition to starting
	logFile := m.logFilePath()
	if err := m.writeState(&stateFile{
		State:     StateStarting,
		LocalPort: m.LocalPort,
		LogFile:   logFile,
	}); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	// Launch background process
	pid, err := m.execFunc(m.Namespace, m.Target, m.LocalPort, m.RemotePort, logFile)
	if err != nil {
		_ = m.writeState(&stateFile{State: StateFailed, LocalPort: m.LocalPort})
		return fmt.Errorf("failed to start port-forward: %w", err)
	}

	// Transition to running
	if err := m.writeState(&stateFile{
		State:     StateRunning,
		PID:       pid,
		LocalPort: m.LocalPort,
		LogFile:   logFile,
	}); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	time.Sleep(200 * time.Millisecond)
	if !m.processChecker(pid) {
		_ = m.writeState(&stateFile{
			State:     StateFailed,
			PID:       pid,
			LocalPort: m.LocalPort,
			LogFile:   logFile,
		})
		logTail := readLogTail(logFile, 2048)
		if logTail != "" {
			return fmt.Errorf("port-forward process exited immediately (pid %d): %s", pid, logTail)
		}
		return fmt.Errorf("port-forward process exited immediately (pid %d)", pid)
	}

	return nil
}

// Stop stops the running port-forward process. It is idempotent: if not running,
// it returns immediately.
func (m *Manager) Stop() error {
	st, err := m.readState()
	if err != nil || st == nil || st.State == StateStopped {
		return nil // Already stopped, idempotent
	}

	if st.PID > 0 {
		if err := killProcess(st.PID); err != nil {
			// Process may have already exited
			if !strings.Contains(err.Error(), "process already finished") &&
				!strings.Contains(err.Error(), "no such process") {
				return fmt.Errorf("failed to stop port-forward (pid %d): %w", st.PID, err)
			}
		}
	}

	if err := m.writeState(&stateFile{State: StateStopped, LocalPort: m.LocalPort}); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

// Status returns the current status of the port-forward
func (m *Manager) Status() Status {
	st, err := m.readState()
	if err != nil || st == nil {
		return Status{State: StateStopped, LocalPort: m.LocalPort}
	}

	// Validate that the tracked process is still alive
	if st.State == StateRunning && st.PID > 0 {
		if !m.processChecker(st.PID) {
			// Process died; update state
			failed := &stateFile{
				State:     StateFailed,
				PID:       st.PID,
				LocalPort: st.LocalPort,
				LogFile:   st.LogFile,
			}
			if failed.LocalPort == "" {
				failed.LocalPort = m.LocalPort
			}
			_ = m.writeState(failed)
			return Status{
				State:     StateFailed,
				PID:       failed.PID,
				LocalPort: failed.LocalPort,
				LogFile:   failed.LogFile,
			}
		}
	}

	return Status{
		State:     st.State,
		PID:       st.PID,
		LocalPort: st.LocalPort,
		LogFile:   st.LogFile,
	}
}

// stateFilePath returns the path to the state file
func (m *Manager) stateFilePath() string {
	key := fmt.Sprintf("netcup-claw-pf-%s-%s.json", sanitize(m.Namespace), sanitize(m.LocalPort))
	return filepath.Join(m.stateDir, key)
}

// logFilePath returns the path to the log file
func (m *Manager) logFilePath() string {
	key := fmt.Sprintf("netcup-claw-pf-%s-%s.log", sanitize(m.Namespace), sanitize(m.LocalPort))
	return filepath.Join(m.stateDir, key)
}

// readState reads the state from disk. Returns nil without error if the file doesn't exist.
func (m *Manager) readState() (*stateFile, error) {
	path := m.stateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &st, nil
}

// writeState writes the state to disk
func (m *Manager) writeState(st *stateFile) error {
	path := m.stateFilePath()
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// sanitize replaces characters that are unsafe in filenames
func sanitize(s string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		":", "_",
		" ", "_",
	)
	return replacer.Replace(s)
}

// defaultStateDir returns the default directory for state files
func defaultStateDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return "/tmp"
}

// ReadinessCheck probes the local port for readiness with a timeout.
// Returns nil when the port is accepting connections within the deadline.
func ReadinessCheck(localPort string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isPortListening(localPort) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port-forward on :%s not ready after %s", localPort, timeout)
}

// isPortListening checks if the local port is accepting TCP connections
func isPortListening(port string) bool {
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return false
	}
	return tcpProbe("127.0.0.1", port)
}

func readLogTail(path string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}

	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}

	return strings.TrimSpace(string(data))
}
