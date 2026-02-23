package openclaw

import (
	"fmt"
	"strings"
)

const (
	// DefaultNamespace is the default Kubernetes namespace for OpenClaw
	DefaultNamespace = "openclaw"

	// DefaultLabelSelector is the default label selector for OpenClaw resources
	DefaultLabelSelector = "app.kubernetes.io/instance=openclaw"

	// DefaultFallbackService is the fallback service name when label lookup fails
	DefaultFallbackService = "svc/openclaw"

	// DefaultLocalPort is the default local port for port-forwarding
	DefaultLocalPort = "18789"

	// DefaultRemotePort is the default remote port for port-forwarding
	DefaultRemotePort = "18789"
)

// Config holds OpenClaw resolver configuration
type Config struct {
	Namespace      string
	LabelSelector  string
	FallbackSvc    string
	LocalPort      string
	RemotePort     string
}

// DefaultConfig returns the default OpenClaw configuration
func DefaultConfig() Config {
	return Config{
		Namespace:     DefaultNamespace,
		LabelSelector: DefaultLabelSelector,
		FallbackSvc:   DefaultFallbackService,
		LocalPort:     DefaultLocalPort,
		RemotePort:    DefaultRemotePort,
	}
}

// ExecFunc is the function signature for running external commands
type ExecFunc func(name string, args ...string) ([]byte, error)

// Resolver handles OpenClaw service and pod discovery
type Resolver struct {
	cfg      Config
	execFunc ExecFunc
}

// New creates a new Resolver with the given configuration and exec function.
// If execFunc is nil, a default exec function using os/exec is used.
func New(cfg Config, execFunc ExecFunc) *Resolver {
	if execFunc == nil {
		execFunc = defaultExec
	}
	return &Resolver{
		cfg:      cfg,
		execFunc: execFunc,
	}
}

// ResolveService resolves the OpenClaw service target.
// It first tries label-based discovery and falls back to the configured fallback service.
func (r *Resolver) ResolveService() (string, error) {
	// Try label-based discovery
	out, err := r.execFunc("kubectl",
		"-n", r.cfg.Namespace,
		"get", "svc",
		"-l", r.cfg.LabelSelector,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err == nil {
		name := strings.TrimSpace(string(out))
		if name != "" {
			return "svc/" + name, nil
		}
	}

	// Fallback to configured service
	return r.cfg.FallbackSvc, nil
}

// ResolvePod resolves the main OpenClaw pod name.
// It uses label-based discovery and returns an error if no pod is found.
func (r *Resolver) ResolvePod() (string, error) {
	out, err := r.execFunc("kubectl",
		"-n", r.cfg.Namespace,
		"get", "pod",
		"-l", r.cfg.LabelSelector,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return "", fmt.Errorf("failed to list pods in namespace %s: %w", r.cfg.Namespace, err)
	}

	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("no pod found with label %s in namespace %s", r.cfg.LabelSelector, r.cfg.Namespace)
	}

	return name, nil
}

// Config returns the resolver configuration
func (r *Resolver) Config() Config {
	return r.cfg
}

// PortForwardTarget returns the port-forward target string (namespace/service:port)
func (r *Resolver) PortForwardTarget(svcTarget string) string {
	return fmt.Sprintf("%s:%s", svcTarget, r.cfg.RemotePort)
}
