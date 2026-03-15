package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mfittko/netcup-kube/internal/config"
	"github.com/mfittko/netcup-kube/internal/openclaw"
	"github.com/mfittko/netcup-kube/internal/portforward"
	"github.com/mfittko/netcup-kube/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	version = "dev"

	// Port-forward flags
	pfNamespace  string
	pfLocalPort  string
	pfRemotePort string

	// Tunnel flags
	tunHost       string
	tunUser       string
	tunLocalPort  string
	tunRemoteHost string
	tunRemotePort string

	agentsWorkspaceDir    string
	approvalsWorkspaceDir string
	approvalsDeployFile   string
	approvalsBackupPath   string
	cronWorkspaceDir      string
	cronDeployFile        string
	cronBackupPath        string
	cronPrune             bool
	cronDeleteByName      bool
	skillsWorkspaceDir    string
	skillsSourceDir       string
	skillsBackupPath      string
	skillName             string
	skillsPullAll         bool
	skillsExclude         []string
	secretsEnvFile        string
	secretsName           string
	secretsCreateMissing  bool
	secretsRestart        bool
	configWorkspaceDir    string
	configDeployFile      string
	configBackupPath      string
	configDeploySyncPVC   bool

	// Upgrade flags
	upgradeVersion       string
	upgradeDryRun        bool
	upgradeSkipPinUpdate bool
	upgradeForce         bool
)

const (
	openclawMainContainer = "main"
	openclawCLIPath       = "/app/openclaw.mjs"
	openclawConfigPath    = "/home/node/.openclaw/openclaw.json"
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

		// Step 1: Probe kube API
		if !probeKubeAPI() {
			// Step 2: API unreachable – ensure SSH tunnel is running
			tun := tunnelConfig()
			if tun.Host == "" {
				return fmt.Errorf("kube API is unreachable and no tunnel host configured (set TUNNEL_HOST or --tunnel-host)")
			}

			mgr := tunnel.New(tun.User, tun.Host, tun.LocalPort, tun.RemoteHost, tun.RemotePort)
			if !mgr.IsRunning() {
				fmt.Fprintf(os.Stderr, "kube API unreachable; starting SSH tunnel via %s@%s...\n", tun.User, tun.Host)
				if err := mgr.Start(); err != nil {
					return fmt.Errorf("failed to start SSH tunnel: %w", err)
				}
			}

			// Re-probe after tunnel start
			if !probeKubeAPI() {
				return fmt.Errorf("kube API still unreachable after starting SSH tunnel; check tunnel config and kubeconfig")
			}
		}

		// Step 3: Resolve service target
		resolver := openclaw.New(cfg, nil)
		svcTarget, err := resolver.ResolveService()
		if err != nil {
			return fmt.Errorf("failed to resolve OpenClaw service: %w", err)
		}

		// Step 4: Start port-forward (idempotent)
		mgr := pfManager(cfg, svcTarget)
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start port-forward: %w", err)
		}

		// Step 5: Report status and readiness
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
		mgr := pfManager(cfg, "")

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
		mgr := pfManager(cfg, "")
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

// runCmd executes a shell command on the main pod
var runCmd = &cobra.Command{
	Use:   "run <shell command...>",
	Short: "Run a shell command on the main OpenClaw pod",
	Long: `Execute a shell command on the main OpenClaw pod container.

The command is executed as:
  sh -lc "<your command>"

Examples:
  netcup-claw run ls -la /app
  netcup-claw run env | grep OPENCLAW
  netcup-claw run "cat /home/node/.openclaw/openclaw.json"
  netcup-claw run --help`,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		execArgs := buildShellRunKubectlArgs(cfg.Namespace, pod, args)

		return runKubectl(execArgs...)
	},
}

// openclawCmd executes OpenClaw CLI commands in the main pod
var openclawCmd = &cobra.Command{
	Use:   "openclaw <subcommand> [args...]",
	Short: "Run OpenClaw CLI commands on the main pod",
	Long: `Execute OpenClaw CLI commands in the main OpenClaw pod container.

Examples:
  netcup-claw openclaw status
  netcup-claw openclaw logs --follow
  netcup-claw openclaw security audit --deep`,
	Args:               cobra.MinimumNArgs(1),
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		execArgs := buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, args)
		useTTY := hasTerminalStdio()
		if useTTY {
			execArgs = withKubectlExecTTY(execArgs)
		} else if openclawArgsRequireTTY(args) {
			return fmt.Errorf("command requires an interactive TTY")
		}

		return runKubectl(execArgs...)
	},
}

func buildShellRunKubectlArgs(namespace, pod string, args []string) []string {
	command := strings.Join(args, " ")

	execArgs := []string{
		"-n", namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"sh",
		"-lc",
		command,
	}

	return execArgs
}

func buildOpenClawCLIKubectlArgs(namespace, pod string, args []string) []string {

	execArgs := []string{
		"-n", namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"node",
		"--no-warnings",
		openclawCLIPath,
	}

	return append(execArgs, args...)
}

func openclawArgsRequireTTY(args []string) bool {
	if len(args) >= 1 && args[0] == "onboard" {
		return true
	}
	if len(args) >= 3 && args[0] == "models" && args[1] == "auth" && args[2] == "login" {
		return true
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "login" {
		return true
	}
	return false
}

func hasTerminalStdio() bool {
	in, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	out, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (in.Mode()&os.ModeCharDevice) != 0 && (out.Mode()&os.ModeCharDevice) != 0
}

func withKubectlExecTTY(args []string) []string {
	if len(args) == 0 {
		return args
	}
	updated := make([]string, 0, len(args)+1)
	inserted := false
	for _, arg := range args {
		updated = append(updated, arg)
		if !inserted && arg == "exec" {
			updated = append(updated, "-it")
			inserted = true
		}
	}
	return updated
}

type agentListEntry struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func localAgentWorkspaceDir() string {
	if strings.TrimSpace(agentsWorkspaceDir) != "" {
		return agentsWorkspaceDir
	}
	return "scripts/recipes/openclaw/agent-workspace"
}

func resolveOpenClawPod() (openclaw.Config, string, error) {
	cfg := openclawConfig()
	if err := ensureKubeAPIReachableWithTunnel(); err != nil {
		return cfg, "", err
	}
	resolver := openclaw.New(cfg, nil)
	pod, err := resolver.ResolvePod()
	if err != nil {
		return cfg, "", fmt.Errorf("failed to resolve OpenClaw pod: %w", err)
	}
	return cfg, pod, nil
}

func fetchAgentList(cfg openclaw.Config, pod string) ([]agentListEntry, []byte, error) {
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"node",
		openclawCLIPath,
		"agents",
		"list",
		"--json",
	)
	if err != nil {
		return nil, nil, err
	}

	var agents []agentListEntry
	if err := json.Unmarshal(out, &agents); err != nil {
		return nil, nil, fmt.Errorf("failed to parse agents list json: %w", err)
	}

	return agents, out, nil
}

func fetchApprovalsSnapshot(cfg openclaw.Config, pod string) ([]byte, error) {
	out, err := runKubectlOutput(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, []string{"approvals", "get", "--json"})...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch approvals snapshot: %w", err)
	}
	return out, nil
}

func normalizeApprovalsPayload(payload []byte) ([]byte, error) {
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(payload, &asMap); err != nil {
		return nil, fmt.Errorf("invalid approvals JSON: %w", err)
	}

	if rawFile, ok := asMap["file"]; ok && len(rawFile) > 0 {
		var inner any
		if err := json.Unmarshal(rawFile, &inner); err != nil {
			return nil, fmt.Errorf("invalid approvals snapshot envelope (field 'file'): %w", err)
		}
		normalized, err := json.Marshal(inner)
		if err != nil {
			return nil, fmt.Errorf("failed to normalize approvals snapshot envelope: %w", err)
		}
		return normalized, nil
	}

	var direct any
	if err := json.Unmarshal(payload, &direct); err != nil {
		return nil, fmt.Errorf("invalid approvals JSON: %w", err)
	}
	normalized, err := json.Marshal(direct)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize approvals JSON: %w", err)
	}
	return normalized, nil
}

func prettyJSON(payload []byte) ([]byte, error) {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("invalid JSON for pretty print: %w", err)
	}
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to pretty print JSON: %w", err)
	}
	return append(pretty, '\n'), nil
}

func writeApprovalsBackup(backupPath string, payload []byte) (string, error) {
	return writeSnapshotBackup(backupPath, "exec-approvals", payload)
}

func writeSnapshotBackup(backupPath, prefix string, payload []byte) (string, error) {
	resolvedPath := strings.TrimSpace(backupPath)
	if resolvedPath == "" {
		return "", nil
	}

	isJSONFile := strings.EqualFold(filepath.Ext(resolvedPath), ".json")
	if isJSONFile {
		if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
			return "", fmt.Errorf("failed to create backup directory: %w", err)
		}
		if err := os.WriteFile(resolvedPath, payload, 0o644); err != nil {
			return "", fmt.Errorf("failed to write backup file: %w", err)
		}
		return resolvedPath, nil
	}

	if err := os.MkdirAll(resolvedPath, 0o755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	backupFile := filepath.Join(resolvedPath, fmt.Sprintf("%s-%s.json", prefix, time.Now().UTC().Format("20060102-150405")))
	if err := os.WriteFile(backupFile, payload, 0o644); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}
	return backupFile, nil
}

func localApprovalsWorkspaceDir() string {
	if strings.TrimSpace(approvalsWorkspaceDir) != "" {
		return approvalsWorkspaceDir
	}
	return "scripts/recipes/openclaw/approvals"
}

func localConfigWorkspaceDir() string {
	if strings.TrimSpace(configWorkspaceDir) != "" {
		return configWorkspaceDir
	}
	return "scripts/recipes/openclaw/config"
}

func localCronWorkspaceDir() string {
	if strings.TrimSpace(cronWorkspaceDir) != "" {
		return cronWorkspaceDir
	}
	return "scripts/recipes/openclaw/cron"
}

func localSkillsWorkspaceDir() string {
	if strings.TrimSpace(skillsWorkspaceDir) != "" {
		return skillsWorkspaceDir
	}
	return "scripts/recipes/openclaw/skills"
}

func remoteSkillsRootDir() string {
	return "/home/node/.openclaw/workspace/skills"
}

func remoteSkillDir(skill string) string {
	return remoteSkillsRootDir() + "/" + skill
}

func listRemoteSkillNames(cfg openclaw.Config, pod string) ([]string, error) {
	root := remoteSkillsRootDir()
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"sh",
		"-lc",
		fmt.Sprintf("if [ -d %s ]; then for d in %s/*; do [ -d \"$d\" ] || continue; basename \"$d\"; done; fi", shellQuote(root), shellQuote(root)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote skills: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func filterSkillNames(all []string, excludes []string) []string {
	excluded := map[string]struct{}{}
	for _, raw := range excludes {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		excluded[name] = struct{}{}
	}

	filtered := make([]string, 0, len(all))
	for _, name := range all {
		if _, skip := excluded[name]; skip {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func resolveSelectedSkill(args []string) (string, error) {
	if len(args) > 0 {
		selected := strings.TrimSpace(args[0])
		if selected == "" {
			return "", fmt.Errorf("skill name cannot be empty")
		}
		return selected, nil
	}

	selected := strings.TrimSpace(skillName)
	if selected == "" {
		return "", fmt.Errorf("skill name cannot be empty")
	}
	return selected, nil
}

func copyRemoteSkillToLocal(cfg openclaw.Config, pod, skill, targetDir string) error {
	if strings.TrimSpace(skill) == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create target dir %s: %w", targetDir, err)
	}

	destination := filepath.Join(targetDir, skill)
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("failed to clean destination dir %s: %w", destination, err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("failed to create destination dir %s: %w", destination, err)
	}

	if err := runKubectl(
		"-n", cfg.Namespace,
		"cp",
		pod+":"+remoteSkillDir(skill)+"/.",
		destination,
		"-c", openclawMainContainer,
	); err != nil {
		return fmt.Errorf("failed to copy remote skill %s to %s: %w", skill, destination, err)
	}
	return nil
}

func backupRemoteSkillSnapshot(cfg openclaw.Config, pod, skill, backupPath string) (string, error) {
	resolvedPath := strings.TrimSpace(backupPath)
	if resolvedPath == "" {
		return "", nil
	}

	snapshotRoot := filepath.Join(resolvedPath, fmt.Sprintf("%s-%s", skill, time.Now().UTC().Format("20060102-150405")))
	if err := os.MkdirAll(snapshotRoot, 0o755); err != nil {
		return "", fmt.Errorf("failed to create skill backup dir %s: %w", snapshotRoot, err)
	}

	if err := copyRemoteSkillToLocal(cfg, pod, skill, snapshotRoot); err != nil {
		return "", err
	}

	return filepath.Join(snapshotRoot, skill), nil
}

func deployLocalSkillToRemote(cfg openclaw.Config, pod, skill, sourceDir string) error {
	if strings.TrimSpace(skill) == "" {
		return fmt.Errorf("skill name cannot be empty")
	}

	info, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to access skill source dir %s: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("skill source path is not a directory: %s", sourceDir)
	}

	if filepath.Base(sourceDir) != skill {
		return fmt.Errorf("skill source dir basename (%s) must match --skill (%s)", filepath.Base(sourceDir), skill)
	}

	if err := runKubectl(
		"-n", cfg.Namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"sh",
		"-lc",
		fmt.Sprintf("mkdir -p %s && rm -rf %s", shellQuote(remoteSkillsRootDir()), shellQuote(remoteSkillDir(skill))),
	); err != nil {
		return fmt.Errorf("failed to prepare remote skill path: %w", err)
	}

	if err := runKubectl(
		"-n", cfg.Namespace,
		"cp",
		sourceDir,
		pod+":"+remoteSkillsRootDir(),
		"-c", openclawMainContainer,
	); err != nil {
		return fmt.Errorf("failed to deploy local skill dir %s: %w", sourceDir, err)
	}

	return nil
}

func fetchCronJobsSnapshot(cfg openclaw.Config, pod string) ([]byte, error) {
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"exec",
		"-c", openclawMainContainer,
		pod,
		"--",
		"sh",
		"-lc",
		"cat /home/node/.openclaw/cron/jobs.json",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cron jobs snapshot: %w", err)
	}
	return out, nil
}

func normalizeCronJobsPayload(payload []byte) ([]byte, error) {
	var direct any
	if err := json.Unmarshal(payload, &direct); err != nil {
		return nil, fmt.Errorf("invalid cron jobs JSON: %w", err)
	}
	stripRuntimeCronFields(direct)
	normalized, err := json.Marshal(direct)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize cron jobs JSON: %w", err)
	}
	return normalized, nil
}

func stripRuntimeCronFields(payload any) {
	root, ok := payload.(map[string]any)
	if !ok {
		return
	}

	jobsRaw, ok := root["jobs"]
	if !ok {
		return
	}

	jobs, ok := jobsRaw.([]any)
	if !ok {
		return
	}

	for _, jobRaw := range jobs {
		job, ok := jobRaw.(map[string]any)
		if !ok {
			continue
		}

		delete(job, "state")
		delete(job, "createdAtMs")
		delete(job, "updatedAtMs")
		delete(job, "sessionKey")
		delete(job, "wakeMode")
	}
}

func writeCronJobsBackup(backupPath string, payload []byte) (string, error) {
	return writeSnapshotBackup(backupPath, "cron-jobs", payload)
}

type cronJobsFile struct {
	Jobs    []cronJobSpec `json:"jobs"`
	Version int           `json:"version"`
}

type cronJobSpec struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	DeleteAfterRun bool   `json:"deleteAfterRun"`
	AgentID        string `json:"agentId"`
	SessionTarget  string `json:"sessionTarget"`
	Payload        struct {
		Kind     string `json:"kind"`
		Message  string `json:"message"`
		Model    string `json:"model"`
		Thinking string `json:"thinking"`
	} `json:"payload"`
	Schedule struct {
		Kind string `json:"kind"`
		Expr string `json:"expr"`
		Tz   string `json:"tz"`
	} `json:"schedule"`
	Delivery struct {
		Mode       string `json:"mode"`
		Channel    string `json:"channel"`
		To         string `json:"to"`
		BestEffort bool   `json:"bestEffort"`
	} `json:"delivery"`
}

func parseCronJobsFile(payload []byte) (cronJobsFile, error) {
	var file cronJobsFile
	if err := json.Unmarshal(payload, &file); err != nil {
		return cronJobsFile{}, fmt.Errorf("invalid cron jobs JSON: %w", err)
	}
	return file, nil
}

func buildCronEditArgs(job cronJobSpec) ([]string, error) {
	if strings.TrimSpace(job.ID) == "" {
		return nil, fmt.Errorf("job id is required")
	}

	args := []string{"cron", "edit", job.ID}

	if name := strings.TrimSpace(job.Name); name != "" {
		args = append(args, "--name", name)
	}

	if strings.EqualFold(job.Schedule.Kind, "cron") {
		expr := strings.TrimSpace(job.Schedule.Expr)
		if expr == "" {
			return nil, fmt.Errorf("job %s missing cron expr", job.ID)
		}
		args = append(args, "--cron", expr)
		if tz := strings.TrimSpace(job.Schedule.Tz); tz != "" {
			args = append(args, "--tz", tz)
		}
	}

	if strings.TrimSpace(job.AgentID) != "" {
		args = append(args, "--agent", strings.TrimSpace(job.AgentID))
	}

	if sessionTarget := strings.TrimSpace(job.SessionTarget); sessionTarget != "" {
		args = append(args, "--session", sessionTarget)
	}

	if job.Enabled {
		args = append(args, "--enable")
	} else {
		args = append(args, "--disable")
	}

	mode := strings.TrimSpace(job.Delivery.Mode)
	if strings.EqualFold(mode, "announce") {
		args = append(args, "--announce")
	} else if strings.EqualFold(mode, "none") {
		args = append(args, "--no-deliver")
	}

	if channel := strings.TrimSpace(job.Delivery.Channel); channel != "" {
		args = append(args, "--channel", channel)
	}
	if to := strings.TrimSpace(job.Delivery.To); to != "" {
		args = append(args, "--to", to)
	}

	if job.Delivery.BestEffort {
		args = append(args, "--best-effort-deliver")
	} else {
		args = append(args, "--no-best-effort-deliver")
	}

	if strings.EqualFold(strings.TrimSpace(job.Payload.Kind), "agentTurn") {
		if msg := strings.TrimSpace(job.Payload.Message); msg != "" {
			args = append(args, "--message", msg)
		}
		if model := strings.TrimSpace(job.Payload.Model); model != "" {
			args = append(args, "--model", model)
		}
		if thinking := strings.TrimSpace(job.Payload.Thinking); thinking != "" {
			args = append(args, "--thinking", thinking)
		}
	}

	return args, nil
}

func buildCronAddArgs(job cronJobSpec) ([]string, error) {
	args := []string{"cron", "add"}

	if name := strings.TrimSpace(job.Name); name != "" {
		args = append(args, "--name", name)
	}

	if strings.EqualFold(job.Schedule.Kind, "cron") {
		expr := strings.TrimSpace(job.Schedule.Expr)
		if expr == "" {
			return nil, fmt.Errorf("job %s missing cron expr", job.ID)
		}
		args = append(args, "--cron", expr)
		if tz := strings.TrimSpace(job.Schedule.Tz); tz != "" {
			args = append(args, "--tz", tz)
		}
	}

	if strings.TrimSpace(job.AgentID) != "" {
		args = append(args, "--agent", strings.TrimSpace(job.AgentID))
	}

	if sessionTarget := strings.TrimSpace(job.SessionTarget); sessionTarget != "" {
		args = append(args, "--session", sessionTarget)
	}

	if !job.Enabled {
		args = append(args, "--disabled")
	}

	mode := strings.TrimSpace(job.Delivery.Mode)
	if strings.EqualFold(mode, "announce") {
		args = append(args, "--announce")
	} else if strings.EqualFold(mode, "none") {
		args = append(args, "--no-deliver")
	}

	if channel := strings.TrimSpace(job.Delivery.Channel); channel != "" {
		args = append(args, "--channel", channel)
	}
	if to := strings.TrimSpace(job.Delivery.To); to != "" {
		args = append(args, "--to", to)
	}

	if job.Delivery.BestEffort {
		args = append(args, "--best-effort-deliver")
	}

	if strings.EqualFold(strings.TrimSpace(job.Payload.Kind), "agentTurn") {
		if msg := strings.TrimSpace(job.Payload.Message); msg != "" {
			args = append(args, "--message", msg)
		}
		if model := strings.TrimSpace(job.Payload.Model); model != "" {
			args = append(args, "--model", model)
		}
		if thinking := strings.TrimSpace(job.Payload.Thinking); thinking != "" {
			args = append(args, "--thinking", thinking)
		}
	}

	return args, nil
}

func cronJobSpecEqualForSync(desired cronJobSpec, current cronJobSpec) bool {
	return reflect.DeepEqual(desired, current)
}

func runOpenClawCronDelete(cfg openclaw.Config, pod, jobID string) error {
	trimmedID := strings.TrimSpace(jobID)
	if trimmedID == "" {
		return fmt.Errorf("job id is required")
	}

	candidates := [][]string{
		{"cron", "remove", trimmedID},
		{"cron", "delete", trimmedID},
		{"cron", "rm", trimmedID},
	}

	errs := make([]string, 0, len(candidates))
	for _, args := range candidates {
		err := runKubectl(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, args)...)
		if err == nil {
			return nil
		}
		errs = append(errs, strings.Join(args, " ")+": "+err.Error())
	}

	return fmt.Errorf("failed to delete cron job %q (tried remove/delete/rm): %s", trimmedID, strings.Join(errs, " | "))
}

func findCronJobByName(file cronJobsFile, name string) (cronJobSpec, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return cronJobSpec{}, false
	}

	for _, job := range file.Jobs {
		if strings.TrimSpace(job.Name) == trimmed {
			return job, true
		}
	}

	return cronJobSpec{}, false
}

func openclawSecretKeys() []string {
	return []string{
		"OPENCLAW_GATEWAY_TOKEN",
		"DISCORD_BOT_TOKEN",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"AISSTREAM_API_KEY",
		"SAG_API_KEY",
	}
}

func processEnvMap() map[string]string {
	result := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		result[parts[0]] = parts[1]
	}
	return result
}

func resolveSecretsFromEnv(processValues, envFileValues map[string]string, keys []string) (map[string]string, []string) {
	resolved := make(map[string]string)
	missing := make([]string, 0)

	for _, key := range keys {
		value := strings.TrimSpace(processValues[key])
		if envFileValues != nil {
			if fileValue, ok := envFileValues[key]; ok {
				value = strings.TrimSpace(fileValue)
			}
		}
		if value == "" {
			missing = append(missing, key)
			continue
		}
		resolved[key] = value
	}

	return resolved, missing
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func deployedConfigMapName() string {
	return "openclaw"
}

func deployedConfigKey() string {
	return "openclaw.json"
}

func deployedConfigDeploymentName() string {
	return "openclaw"
}

func fetchDeployedConfig(cfg openclaw.Config) ([]byte, error) {
	pathExpr := "{.data.openclaw\\.json}"
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"get",
		"configmap",
		deployedConfigMapName(),
		"-o",
		"jsonpath="+pathExpr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deployed config from configmap %s: %w", deployedConfigMapName(), err)
	}
	return out, nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Backup or deploy OpenClaw deployed config",
	Long: `Manage the deployed OpenClaw config (ConfigMap-based) for the running workload.

Sub-commands:
  backup  - Pull current deployed openclaw.json into local backup path
  pull    - Pull current deployed openclaw.json into local workspace file
	validate - Validate a local openclaw.json against the running OpenClaw image schema
  deploy  - Push local openclaw.json into ConfigMap and restart rollout`,
}

var configBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Pull current deployed OpenClaw config into local backup path",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		payload, err := fetchDeployedConfig(cfg)
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(configBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localConfigWorkspaceDir(), "backup")
		}

		backupFile, err := writeSnapshotBackup(backupPath, "openclaw-config", payload)
		if err != nil {
			return err
		}

		fmt.Printf("backup complete: %s\n", backupFile)
		return nil
	},
}

var configPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull current deployed OpenClaw config into local workspace file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()
		payload, err := fetchDeployedConfig(cfg)
		if err != nil {
			return err
		}

		prettyPayload, err := prettyJSON(payload)
		if err != nil {
			return err
		}

		targetPath := strings.TrimSpace(configDeployFile)
		if targetPath == "" {
			targetPath = "scripts/recipes/openclaw/openclaw.json"
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("failed to create target directory for %s: %w", targetPath, err)
		}
		if err := os.WriteFile(targetPath, prettyPayload, 0o644); err != nil {
			return fmt.Errorf("failed to write pulled config to %s: %w", targetPath, err)
		}

		fmt.Printf("pull complete: %s\n", targetPath)
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate local OpenClaw config against the running image",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()

		inputPath := strings.TrimSpace(configDeployFile)
		if inputPath == "" {
			inputPath = "scripts/recipes/openclaw/openclaw.json"
		}

		payload, err := os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("failed to read config validate file %s: %w", inputPath, err)
		}

		var js map[string]any
		if err := json.Unmarshal(payload, &js); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", inputPath, err)
		}

		if err := validateOpenClawConfigPayload(cfg, payload); err != nil {
			return fmt.Errorf("config validation failed for %s: %w", inputPath, err)
		}

		fmt.Printf("config valid: %s\n", inputPath)
		return nil
	},
}

var configDeployCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"push"},
	Short:   "Deploy local OpenClaw config to ConfigMap and restart",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()

		inputPath := strings.TrimSpace(configDeployFile)
		if inputPath == "" {
			inputPath = "scripts/recipes/openclaw/openclaw.json"
		}

		payload, err := os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("failed to read config deploy file %s: %w", inputPath, err)
		}

		var js map[string]any
		if err := json.Unmarshal(payload, &js); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", inputPath, err)
		}

		if err := validateOpenClawConfigPayload(cfg, payload); err != nil {
			return fmt.Errorf("config validation failed for %s: %w", inputPath, err)
		}

		backupPath := strings.TrimSpace(configBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localConfigWorkspaceDir(), "backup")
		}

		if backupPath != "off" {
			existing, err := fetchDeployedConfig(cfg)
			if err != nil {
				return err
			}
			backupFile, err := writeSnapshotBackup(backupPath, "openclaw-config", existing)
			if err != nil {
				return err
			}
			if backupFile != "" {
				fmt.Printf("config backup saved: %s\n", backupFile)
			}
		}

		generated, err := runKubectlOutput(
			"-n", cfg.Namespace,
			"create",
			"configmap",
			deployedConfigMapName(),
			"--from-file="+deployedConfigKey()+"="+inputPath,
			"--dry-run=client",
			"-o",
			"yaml",
		)
		if err != nil {
			return fmt.Errorf("failed to render configmap yaml: %w", err)
		}

		tmpFile, err := os.CreateTemp("", "netcup-claw-openclaw-config-*.yaml")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		if _, err := tmpFile.Write(generated); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write temp configmap yaml: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to close temp configmap yaml: %w", err)
		}
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err := runKubectl("-n", cfg.Namespace, "apply", "-f", tmpPath); err != nil {
			return fmt.Errorf("failed to apply configmap: %w", err)
		}

		if configDeploySyncPVC {
			if err := syncOpenClawRuntimeConfig(cfg, payload); err != nil {
				return fmt.Errorf("failed to sync runtime config: %w", err)
			}
		}

		if err := runKubectl("-n", cfg.Namespace, "rollout", "restart", "deployment/"+deployedConfigDeploymentName()); err != nil {
			return fmt.Errorf("failed to restart deployment: %w", err)
		}

		if err := runKubectl("-n", cfg.Namespace, "rollout", "status", "deployment/"+deployedConfigDeploymentName(), "--timeout=180s"); err != nil {
			return fmt.Errorf("deployment rollout did not complete: %w", err)
		}

		fmt.Printf("deploy complete: %s\n", inputPath)
		return nil
	},
}

func resolveOpenClawMainImage(cfg openclaw.Config) (string, error) {
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"get",
		"deployment",
		deployedConfigDeploymentName(),
		"-o",
		`jsonpath={.spec.template.spec.containers[?(@.name=="main")].image}`,
	)
	if err != nil {
		return "", fmt.Errorf("failed to resolve openclaw image: %w", err)
	}
	image := strings.TrimSpace(string(out))
	if image == "" {
		return "", fmt.Errorf("openclaw main container image is empty")
	}
	return image, nil
}

func resolveOpenClawRuntimePVCName(cfg openclaw.Config) (string, error) {
	out, err := runKubectlOutput(
		"-n", cfg.Namespace,
		"get",
		"deployment",
		deployedConfigDeploymentName(),
		"-o",
		`jsonpath={.spec.template.spec.volumes[?(@.name=="data")].persistentVolumeClaim.claimName}`,
	)
	if err != nil {
		return "", fmt.Errorf("failed to resolve OpenClaw PVC name: %w", err)
	}
	pvcName := strings.TrimSpace(string(out))
	if pvcName == "" {
		return "", fmt.Errorf("OpenClaw PVC name is empty")
	}
	return pvcName, nil
}

func writeTempFile(prefix string, payload []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write temp file %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close temp file %s: %w", tmpPath, err)
	}
	return tmpPath, nil
}

func buildConfigValidationPodManifest(podName, image string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  restartPolicy: Never
  containers:
  - name: validate
    image: %s
    command: ["sh", "-lc", "mkdir -p /home/node/.openclaw && sleep 600"]
`, podName, image))
}

func buildConfigSyncPodManifest(podName, image, pvcName string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  restartPolicy: Never
  containers:
  - name: sync
    image: %s
    command: ["sh", "-lc", "mkdir -p /mnt/openclaw && sleep 600"]
    volumeMounts:
    - name: data
      mountPath: /mnt/openclaw
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, podName, image, pvcName))
}

func createTempPod(cfg openclaw.Config, podName string, manifest []byte) error {
	manifestPath, err := writeTempFile("netcup-claw-openclaw-pod-*.yaml", manifest)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(manifestPath)
	}()

	if err := runKubectl("-n", cfg.Namespace, "apply", "-f", manifestPath); err != nil {
		return fmt.Errorf("failed to create temp pod %s: %w", podName, err)
	}
	if err := runKubectl("-n", cfg.Namespace, "wait", "--for=condition=Ready", "pod/"+podName, "--timeout=90s"); err != nil {
		return fmt.Errorf("temp pod %s did not become ready: %w", podName, err)
	}
	return nil
}

func deleteTempPod(cfg openclaw.Config, podName string) {
	_, _ = runKubectlCombinedOutput("-n", cfg.Namespace, "delete", "pod", podName, "--ignore-not-found=true", "--wait=false")
}

func validateOpenClawConfigPayload(cfg openclaw.Config, payload []byte) error {
	if err := ensureKubeAPIReachableWithTunnel(); err != nil {
		return err
	}

	image, err := resolveOpenClawMainImage(cfg)
	if err != nil {
		return err
	}

	podName := fmt.Sprintf("openclaw-config-validate-%d", time.Now().UTC().UnixNano())
	defer deleteTempPod(cfg, podName)

	if err := createTempPod(cfg, podName, buildConfigValidationPodManifest(podName, image)); err != nil {
		return err
	}

	tmpPath, err := writeTempFile("netcup-claw-openclaw-config-*.json", payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := runKubectl("-n", cfg.Namespace, "cp", tmpPath, podName+":"+openclawConfigPath, "-c", "validate"); err != nil {
		return fmt.Errorf("failed to copy config into validation pod: %w", err)
	}

	out, err := runKubectlCombinedOutput(
		"-n", cfg.Namespace,
		"exec",
		"-c", "validate",
		podName,
		"--",
		"node",
		"--no-warnings",
		openclawCLIPath,
		"config",
		"validate",
		"--json",
	)
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%s", trimmed)
	}
	return nil
}

func syncOpenClawRuntimeConfig(cfg openclaw.Config, payload []byte) error {
	if err := ensureKubeAPIReachableWithTunnel(); err != nil {
		return err
	}

	image, err := resolveOpenClawMainImage(cfg)
	if err != nil {
		return err
	}
	pvcName, err := resolveOpenClawRuntimePVCName(cfg)
	if err != nil {
		return err
	}

	podName := fmt.Sprintf("openclaw-config-sync-%d", time.Now().UTC().UnixNano())
	defer deleteTempPod(cfg, podName)

	if err := createTempPod(cfg, podName, buildConfigSyncPodManifest(podName, image, pvcName)); err != nil {
		return err
	}

	tmpPath, err := writeTempFile("netcup-claw-openclaw-config-*.json", payload)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := runKubectl("-n", cfg.Namespace, "cp", tmpPath, podName+":/tmp/openclaw.json", "-c", "sync"); err != nil {
		return fmt.Errorf("failed to copy config into sync pod: %w", err)
	}

	backupSuffix := time.Now().UTC().Format("20060102-150405")
	script := fmt.Sprintf(
		"set -eu; target=/mnt/openclaw/openclaw.json; if [ -f \"$target\" ]; then cp \"$target\" \"$target.bak.%s\"; fi; cp /tmp/openclaw.json \"$target\"; chmod 0600 \"$target\"",
		backupSuffix,
	)
	if err := runKubectl("-n", cfg.Namespace, "exec", "-c", "sync", podName, "--", "sh", "-lc", script); err != nil {
		return fmt.Errorf("failed to write config into runtime PVC: %w", err)
	}

	return nil
}

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Backup or deploy agent workspace markdown files",
	Long: `Manage OpenClaw agent workspace markdown files against the running pod.

Sub-commands:
  backup  - Pull existing agent workspace *.md files into local backup/
  deploy  - Push local agents/<agentId>/*.md overrides to agent workspaces`,
}

var agentsBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Pull existing workspace markdown files for all agents into backup/",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		agents, raw, err := fetchAgentList(cfg, pod)
		if err != nil {
			return fmt.Errorf("failed to list agents: %w", err)
		}

		workspaceRoot := localAgentWorkspaceDir()
		backupRoot := filepath.Join(workspaceRoot, "backup")
		if err := os.MkdirAll(backupRoot, 0o755); err != nil {
			return fmt.Errorf("failed to create backup root %s: %w", backupRoot, err)
		}
		if err := os.WriteFile(filepath.Join(backupRoot, "agents.list.json"), raw, 0o644); err != nil {
			return fmt.Errorf("failed to write agents.list.json: %w", err)
		}

		filesBackedUp := 0
		for _, agent := range agents {
			if strings.TrimSpace(agent.ID) == "" || strings.TrimSpace(agent.Workspace) == "" {
				continue
			}

			agentBackupDir := filepath.Join(backupRoot, agent.ID)
			if err := os.MkdirAll(agentBackupDir, 0o755); err != nil {
				return fmt.Errorf("failed to create backup directory %s: %w", agentBackupDir, err)
			}

			listOut, err := runKubectlOutput(
				"-n", cfg.Namespace,
				"exec",
				"-c", openclawMainContainer,
				pod,
				"--",
				"sh",
				"-lc",
				fmt.Sprintf("find %s -maxdepth 1 -type f -name '*.md' -printf '%%f\\n' 2>/dev/null || true", shellQuote(agent.Workspace)),
			)
			if err != nil {
				return fmt.Errorf("failed to list workspace markdown files for agent %s: %w", agent.ID, err)
			}

			var names []string
			for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
				name := strings.TrimSpace(line)
				if name == "" {
					continue
				}
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				content, err := runKubectlOutput(
					"-n", cfg.Namespace,
					"exec",
					"-c", openclawMainContainer,
					pod,
					"--",
					"sh",
					"-lc",
					fmt.Sprintf("cat %s", shellQuote(agent.Workspace+"/"+name)),
				)
				if err != nil {
					return fmt.Errorf("failed to read %s for agent %s: %w", name, agent.ID, err)
				}

				if err := os.WriteFile(filepath.Join(agentBackupDir, name), content, 0o644); err != nil {
					return fmt.Errorf("failed to write backup file for agent %s (%s): %w", agent.ID, name, err)
				}
				filesBackedUp++
			}
		}

		fmt.Printf("backup complete: %d files -> %s\n", filesBackedUp, backupRoot)
		return nil
	},
}

var agentsDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy local per-agent override markdown files to running agent workspaces",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		agents, _, err := fetchAgentList(cfg, pod)
		if err != nil {
			return fmt.Errorf("failed to list agents: %w", err)
		}

		workspaceRoot := localAgentWorkspaceDir()
		overridesRoot := filepath.Join(workspaceRoot, "agents")
		if stat, err := os.Stat(overridesRoot); err != nil || !stat.IsDir() {
			return fmt.Errorf("agent overrides directory not found: %s", overridesRoot)
		}

		applied := 0
		for _, agent := range agents {
			if strings.TrimSpace(agent.ID) == "" || strings.TrimSpace(agent.Workspace) == "" {
				continue
			}

			agentOverrideDir := filepath.Join(overridesRoot, agent.ID)
			entries, err := os.ReadDir(agentOverrideDir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("failed to read overrides for agent %s: %w", agent.ID, err)
			}

			if err := runKubectl(
				"-n", cfg.Namespace,
				"exec",
				"-c", openclawMainContainer,
				pod,
				"--",
				"sh",
				"-lc",
				fmt.Sprintf("mkdir -p %s", shellQuote(agent.Workspace)),
			); err != nil {
				return fmt.Errorf("failed to ensure workspace directory for agent %s: %w", agent.ID, err)
			}

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !strings.HasSuffix(strings.ToLower(name), ".md") {
					continue
				}

				sourcePath := filepath.Join(agentOverrideDir, name)
				tmpPath := agent.Workspace + "/." + name + ".netcup-claw"
				targetPath := agent.Workspace + "/" + name

				if err := runKubectl(
					"-n", cfg.Namespace,
					"cp",
					sourcePath,
					pod+":"+tmpPath,
					"-c", openclawMainContainer,
				); err != nil {
					return fmt.Errorf("failed to copy override %s for agent %s: %w", name, agent.ID, err)
				}

				if err := runKubectl(
					"-n", cfg.Namespace,
					"exec",
					"-c", openclawMainContainer,
					pod,
					"--",
					"sh",
					"-lc",
					fmt.Sprintf("mv %s %s && chmod 0644 %s", shellQuote(tmpPath), shellQuote(targetPath), shellQuote(targetPath)),
				); err != nil {
					return fmt.Errorf("failed to place override %s for agent %s: %w", name, agent.ID, err)
				}

				applied++
			}
		}

		fmt.Printf("deploy complete: %d files applied from %s\n", applied, overridesRoot)
		return nil
	},
}

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "Backup or deploy OpenClaw approvals state",
	Long: `Manage OpenClaw approvals state against the running pod.

Sub-commands:
  backup  - Pull current approvals snapshot into local backup path
  pull    - Pull current approvals snapshot into local workspace file
  deploy  - Push local approvals JSON to runtime with optional pre-change backup`,
}

var approvalsBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Pull current approvals snapshot into local backup path",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		snapshot, err := fetchApprovalsSnapshot(cfg, pod)
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(approvalsBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localApprovalsWorkspaceDir(), "backup")
		}

		backupFile, err := writeApprovalsBackup(backupPath, snapshot)
		if err != nil {
			return err
		}

		fmt.Printf("backup complete: %s\n", backupFile)
		return nil
	},
}

var approvalsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull current approvals snapshot into local workspace file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		snapshot, err := fetchApprovalsSnapshot(cfg, pod)
		if err != nil {
			return err
		}

		normalizedPayload, err := normalizeApprovalsPayload(snapshot)
		if err != nil {
			return err
		}

		prettyPayload, err := prettyJSON(normalizedPayload)
		if err != nil {
			return err
		}

		targetPath := strings.TrimSpace(approvalsDeployFile)
		if targetPath == "" {
			targetPath = filepath.Join(localApprovalsWorkspaceDir(), "approvals.json")
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("failed to create target directory for %s: %w", targetPath, err)
		}
		if err := os.WriteFile(targetPath, prettyPayload, 0o644); err != nil {
			return fmt.Errorf("failed to write pulled approvals to %s: %w", targetPath, err)
		}

		fmt.Printf("pull complete: %s\n", targetPath)
		return nil
	},
}

var approvalsDeployCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"push"},
	Short:   "Deploy local approvals JSON to runtime",
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := strings.TrimSpace(approvalsDeployFile)
		if inputPath == "" {
			inputPath = filepath.Join(localApprovalsWorkspaceDir(), "approvals.json")
		}
		if inputPath == "" {
			return fmt.Errorf("approvals deploy file is required")
		}

		if _, err := os.Stat(inputPath); err != nil {
			return fmt.Errorf("failed to read approvals file %s: %w", inputPath, err)
		}

		payload, err := os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("failed to read approvals file %s: %w", inputPath, err)
		}

		normalizedPayload, err := normalizeApprovalsPayload(payload)
		if err != nil {
			return err
		}

		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(approvalsBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localApprovalsWorkspaceDir(), "backup")
		}

		if backupPath != "off" {
			snapshot, err := fetchApprovalsSnapshot(cfg, pod)
			if err != nil {
				return err
			}
			backupFile, err := writeApprovalsBackup(backupPath, snapshot)
			if err != nil {
				return err
			}
			if backupFile != "" {
				fmt.Printf("approvals backup saved: %s\n", backupFile)
			}
		}

		tmpLocalFile, err := os.CreateTemp("", "netcup-claw-approvals-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temporary approvals file: %w", err)
		}
		tmpLocalPath := tmpLocalFile.Name()
		if _, err := tmpLocalFile.Write(normalizedPayload); err != nil {
			_ = tmpLocalFile.Close()
			_ = os.Remove(tmpLocalPath)
			return fmt.Errorf("failed to write temporary approvals file: %w", err)
		}
		if err := tmpLocalFile.Close(); err != nil {
			_ = os.Remove(tmpLocalPath)
			return fmt.Errorf("failed to close temporary approvals file: %w", err)
		}
		defer func() {
			_ = os.Remove(tmpLocalPath)
		}()

		remoteTempPath := "/tmp/netcup-claw-approvals.json"
		if err := runKubectl(
			"-n", cfg.Namespace,
			"cp",
			tmpLocalPath,
			pod+":"+remoteTempPath,
			"-c", openclawMainContainer,
		); err != nil {
			return fmt.Errorf("failed to upload approvals file: %w", err)
		}

		if err := runKubectl(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, []string{"approvals", "set", "--file", remoteTempPath, "--json"})...); err != nil {
			return fmt.Errorf("failed to apply approvals file: %w", err)
		}

		_ = runKubectl(
			"-n", cfg.Namespace,
			"exec",
			"-c", openclawMainContainer,
			pod,
			"--",
			"sh",
			"-lc",
			fmt.Sprintf("rm -f %s", shellQuote(remoteTempPath)),
		)

		fmt.Printf("deploy complete: %s\n", inputPath)
		return nil
	},
}

var cronCmd = &cobra.Command{
	Use:   "cron",
	Short: "Backup, pull, or sync OpenClaw cron jobs",
	Long: `Manage OpenClaw cron jobs state against the running pod.

Sub-commands:
  backup  - Pull current cron jobs snapshot into local backup path
  pull    - Pull current cron jobs snapshot into local workspace file
	deploy  - Sync local jobs to scheduler (default, with optional pre-change backup)
	sync    - Upsert local jobs via openclaw cron edit/add to refresh scheduler state
	delete  - Delete a specific runtime cron job by id (or by name lookup)`,
}

func syncCronJobsFromFile(inputPath string) error {
	payload, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read cron jobs file %s: %w", inputPath, err)
	}

	normalizedPayload, err := normalizeCronJobsPayload(payload)
	if err != nil {
		return err
	}

	file, err := parseCronJobsFile(normalizedPayload)
	if err != nil {
		return err
	}

	cfg, pod, err := resolveOpenClawPod()
	if err != nil {
		return err
	}

	currentSnapshot, err := fetchCronJobsSnapshot(cfg, pod)
	if err != nil {
		return err
	}

	normalizedCurrent, err := normalizeCronJobsPayload(currentSnapshot)
	if err != nil {
		return err
	}

	currentFile, err := parseCronJobsFile(normalizedCurrent)
	if err != nil {
		return err
	}

	currentByID := make(map[string]cronJobSpec, len(currentFile.Jobs))
	currentByName := make(map[string]cronJobSpec, len(currentFile.Jobs))
	for _, job := range currentFile.Jobs {
		if id := strings.TrimSpace(job.ID); id != "" {
			currentByID[id] = job
		}
		if name := strings.TrimSpace(job.Name); name != "" {
			if _, exists := currentByName[name]; !exists {
				currentByName[name] = job
			}
		}
	}

	updated := 0
	created := 0
	skipped := 0
	pruned := 0
	desiredIDs := make(map[string]struct{}, len(file.Jobs))
	for _, job := range file.Jobs {
		if id := strings.TrimSpace(job.ID); id != "" {
			desiredIDs[id] = struct{}{}
		}
	}

	for _, job := range file.Jobs {
		resolved := job
		current, existsByID := currentByID[strings.TrimSpace(job.ID)]
		if !existsByID {
			if byName, ok := currentByName[strings.TrimSpace(job.Name)]; ok {
				current = byName
				resolved.ID = byName.ID
				existsByID = true
			}
		}

		if existsByID {
			if id := strings.TrimSpace(current.ID); id != "" {
				desiredIDs[id] = struct{}{}
			}

			if cronJobSpecEqualForSync(resolved, current) {
				skipped++
				continue
			}

			editArgs, buildErr := buildCronEditArgs(resolved)
			if buildErr != nil {
				return fmt.Errorf("job %q (%s): %w", resolved.Name, resolved.ID, buildErr)
			}

			if err := runKubectl(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, editArgs)...); err != nil {
				return fmt.Errorf("failed to sync job %q (%s): %w", resolved.Name, resolved.ID, err)
			}
			updated++
			continue
		}

		addArgs, buildErr := buildCronAddArgs(resolved)
		if buildErr != nil {
			return fmt.Errorf("job %q (%s): %w", resolved.Name, resolved.ID, buildErr)
		}

		if err := runKubectl(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, addArgs)...); err != nil {
			return fmt.Errorf("failed to create job %q (%s): %w", resolved.Name, resolved.ID, err)
		}
		created++
	}

	if cronPrune {
		for _, current := range currentFile.Jobs {
			id := strings.TrimSpace(current.ID)

			if _, ok := desiredIDs[id]; ok {
				continue
			}

			if id == "" {
				continue
			}

			if err := runOpenClawCronDelete(cfg, pod, id); err != nil {
				return fmt.Errorf("failed to prune job %q (%s): %w", strings.TrimSpace(current.Name), id, err)
			}
			pruned++
		}
	}

	fmt.Printf("sync complete: %d updated, %d created, %d unchanged, %d pruned\n", updated, created, skipped, pruned)
	if cronPrune {
		fmt.Println("prune enabled: runtime jobs missing from local file were deleted")
	}
	if created > 0 {
		fmt.Println("note: newly created jobs receive runtime-generated IDs; run 'netcup-claw cron pull' to refresh local IDs")
	}
	return nil
}

var cronBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Pull current cron jobs snapshot into local backup path",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		snapshot, err := fetchCronJobsSnapshot(cfg, pod)
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(cronBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localCronWorkspaceDir(), "backup")
		}

		backupFile, err := writeCronJobsBackup(backupPath, snapshot)
		if err != nil {
			return err
		}

		fmt.Printf("backup complete: %s\n", backupFile)
		return nil
	},
}

var cronPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull current cron jobs snapshot into local workspace file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		snapshot, err := fetchCronJobsSnapshot(cfg, pod)
		if err != nil {
			return err
		}

		normalizedPayload, err := normalizeCronJobsPayload(snapshot)
		if err != nil {
			return err
		}

		prettyPayload, err := prettyJSON(normalizedPayload)
		if err != nil {
			return err
		}

		targetPath := strings.TrimSpace(cronDeployFile)
		if targetPath == "" {
			targetPath = filepath.Join(localCronWorkspaceDir(), "jobs.json")
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("failed to create target directory for %s: %w", targetPath, err)
		}
		if err := os.WriteFile(targetPath, prettyPayload, 0o644); err != nil {
			return fmt.Errorf("failed to write pulled cron jobs to %s: %w", targetPath, err)
		}

		fmt.Printf("pull complete: %s\n", targetPath)
		return nil
	},
}

var cronDeployCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"push"},
	Short:   "Sync local cron jobs JSON to scheduler (default path)",
	Long: `Apply local cron jobs to runtime using scheduler-safe sync behavior.

This command now uses the same semantics as:
  netcup-claw cron sync

Optional backup still runs first unless --backup-path=off.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := strings.TrimSpace(cronDeployFile)
		if inputPath == "" {
			inputPath = filepath.Join(localCronWorkspaceDir(), "jobs.json")
		}

		if _, err := os.Stat(inputPath); err != nil {
			return fmt.Errorf("failed to read cron jobs file %s: %w", inputPath, err)
		}

		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(cronBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localCronWorkspaceDir(), "backup")
		}

		if backupPath != "off" {
			snapshot, err := fetchCronJobsSnapshot(cfg, pod)
			if err != nil {
				return err
			}
			backupFile, err := writeCronJobsBackup(backupPath, snapshot)
			if err != nil {
				return err
			}
			if backupFile != "" {
				fmt.Printf("cron jobs backup saved: %s\n", backupFile)
			}
		}

		fmt.Println("deploy uses sync semantics (scheduler-safe) by default")
		return syncCronJobsFromFile(inputPath)
	},
}

var cronSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Upsert cron jobs via openclaw cron edit/add",
	Long: `Apply local cron jobs to the running scheduler via OpenClaw cron commands.

This updates scheduler-owned state/job payloads by calling:
  openclaw cron edit <id> ...
and creates missing jobs via:
  openclaw cron add ...

It is useful when file-based deploy does not fully refresh scheduler in-memory state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := strings.TrimSpace(cronDeployFile)
		if inputPath == "" {
			inputPath = filepath.Join(localCronWorkspaceDir(), "jobs.json")
		}
		return syncCronJobsFromFile(inputPath)
	},
}

var cronDeleteCmd = &cobra.Command{
	Use:   "delete <job-id>",
	Short: "Delete a runtime cron job by id or name",
	Long: `Delete a single runtime cron job.

By default this command expects a job id:
  netcup-claw cron delete <job-id>

With --name it resolves the current runtime job id by exact name first:
  netcup-claw cron delete --name "Daily GitHub Morning Brief (sofatutor)"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		jobRef := strings.TrimSpace(args[0])
		if jobRef == "" {
			return fmt.Errorf("job reference cannot be empty")
		}

		jobID := jobRef
		if cronDeleteByName {
			snapshot, err := fetchCronJobsSnapshot(cfg, pod)
			if err != nil {
				return err
			}

			normalizedCurrent, err := normalizeCronJobsPayload(snapshot)
			if err != nil {
				return err
			}

			currentFile, err := parseCronJobsFile(normalizedCurrent)
			if err != nil {
				return err
			}

			match, ok := findCronJobByName(currentFile, jobRef)
			if !ok {
				return fmt.Errorf("no runtime cron job found with name %q", jobRef)
			}

			jobID = strings.TrimSpace(match.ID)
			if jobID == "" {
				return fmt.Errorf("runtime cron job %q has empty id", jobRef)
			}
		}

		if err := runOpenClawCronDelete(cfg, pod, jobID); err != nil {
			return err
		}

		fmt.Printf("delete complete: %s\n", jobID)
		return nil
	},
}

func buildOpenAICodexLoginArgs() []string {
	return []string{"models", "auth", "login", "--provider", "openai-codex"}
}

var codexLoginCmd = &cobra.Command{
	Use:     "codex-login",
	Aliases: []string{"reauth", "reauth-codex"},
	Short:   "Run the OpenAI Codex OAuth re-auth flow in the OpenClaw pod",
	Long: `Start the interactive OpenAI Codex OAuth login flow inside the running
OpenClaw pod.

This is a thin wrapper around:
  netcup-claw openclaw models auth login --provider openai-codex

It requires an interactive terminal because OpenClaw will print a browser URL
and prompt for the redirect URL after sign-in.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !hasTerminalStdio() {
			return fmt.Errorf("codex-login requires an interactive TTY")
		}

		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		execArgs := withKubectlExecTTY(buildOpenClawCLIKubectlArgs(cfg.Namespace, pod, buildOpenAICodexLoginArgs()))
		return runKubectl(execArgs...)
	},
}

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Backup, pull, or deploy OpenClaw skill directories",
	Long: `Manage OpenClaw skill code under /home/node/.openclaw/workspace/skills.

Sub-commands:
  list   - List runtime skills present in OpenClaw workspace
  backup - Pull runtime skill into timestamped local backup path
  pull   - Pull runtime skill(s) into repository workspace path
  deploy - Push local repository skill to runtime (with optional backup)`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List runtime skill directories",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		names, err := listRemoteSkillNames(cfg, pod)
		if err != nil {
			return err
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	},
}

var skillsBackupCmd = &cobra.Command{
	Use:   "backup [skill]",
	Short: "Backup runtime skill directory into local timestamped snapshot",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		selectedSkill, err := resolveSelectedSkill(args)
		if err != nil {
			return err
		}

		backupPath := strings.TrimSpace(skillsBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localSkillsWorkspaceDir(), "backup")
		}

		backupDir, err := backupRemoteSkillSnapshot(cfg, pod, selectedSkill, backupPath)
		if err != nil {
			return err
		}

		fmt.Printf("backup complete: %s\n", backupDir)
		return nil
	},
}

var skillsPullCmd = &cobra.Command{
	Use:   "pull [skill]",
	Short: "Pull runtime skill directory/directories into repository workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		workspaceRoot := localSkillsWorkspaceDir()
		if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
			return fmt.Errorf("failed to create skills workspace dir %s: %w", workspaceRoot, err)
		}

		if skillsPullAll {
			if len(args) > 0 {
				return fmt.Errorf("do not pass a positional skill when using --all")
			}

			remoteSkills, listErr := listRemoteSkillNames(cfg, pod)
			if listErr != nil {
				return listErr
			}

			targets := filterSkillNames(remoteSkills, skillsExclude)
			if len(targets) == 0 {
				fmt.Println("no skills selected for pull (all excluded or none present)")
				return nil
			}

			for _, name := range targets {
				if err := copyRemoteSkillToLocal(cfg, pod, name, workspaceRoot); err != nil {
					return err
				}
				fmt.Printf("pulled: %s\n", filepath.Join(workspaceRoot, name))
			}

			fmt.Printf("pull complete: %d skills\n", len(targets))
			return nil
		}

		selectedSkill, err := resolveSelectedSkill(args)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(workspaceRoot, selectedSkill)
		if err := copyRemoteSkillToLocal(cfg, pod, selectedSkill, workspaceRoot); err != nil {
			return err
		}

		fmt.Printf("pull complete: %s\n", targetPath)
		return nil
	},
}

var skillsDeployCmd = &cobra.Command{
	Use:     "deploy [skill]",
	Aliases: []string{"push"},
	Short:   "Deploy local skill directory to runtime",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
		}

		selectedSkill, err := resolveSelectedSkill(args)
		if err != nil {
			return err
		}

		sourceDir := strings.TrimSpace(skillsSourceDir)
		if sourceDir == "" {
			sourceDir = filepath.Join(localSkillsWorkspaceDir(), selectedSkill)
		}

		backupPath := strings.TrimSpace(skillsBackupPath)
		if backupPath == "" {
			backupPath = filepath.Join(localSkillsWorkspaceDir(), "backup")
		}

		if backupPath != "off" {
			backupDir, backupErr := backupRemoteSkillSnapshot(cfg, pod, selectedSkill, backupPath)
			if backupErr != nil {
				return backupErr
			}
			if backupDir != "" {
				fmt.Printf("skill backup saved: %s\n", backupDir)
			}
		}

		if err := deployLocalSkillToRemote(cfg, pod, selectedSkill, sourceDir); err != nil {
			return err
		}

		fmt.Printf("deploy complete: %s -> %s\n", sourceDir, remoteSkillDir(selectedSkill))
		return nil
	},
}

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage OpenClaw Kubernetes secret values",
}

var secretsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync OpenClaw secret keys from local environment and .env",
	Long: `Sync key/value pairs into the OpenClaw Kubernetes Secret.

Precedence:
  1) Values from --env-file (default: .env)
  2) Process environment variables

Only known OpenClaw-related keys are synced. Existing secret keys not in this
set are preserved when patching.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()

		processValues := processEnvMap()
		var envFileValues map[string]string
		envFilePath := strings.TrimSpace(secretsEnvFile)
		if envFilePath != "" {
			if _, err := os.Stat(envFilePath); err == nil {
				loaded, loadErr := config.LoadEnvFileToMap(envFilePath)
				if loadErr != nil {
					return fmt.Errorf("failed to load env file %s: %w", envFilePath, loadErr)
				}
				envFileValues = loaded
			}
		}

		resolved, missing := resolveSecretsFromEnv(processValues, envFileValues, openclawSecretKeys())
		if len(resolved) == 0 {
			return fmt.Errorf("no secret values resolved; set env vars or provide --env-file")
		}

		patchPayload := map[string]any{"stringData": resolved}
		patchBytes, err := json.Marshal(patchPayload)
		if err != nil {
			return fmt.Errorf("failed to build secret patch payload: %w", err)
		}

		if err := runKubectl(
			"-n", cfg.Namespace,
			"patch",
			"secret",
			secretsName,
			"--type",
			"merge",
			"-p",
			string(patchBytes),
		); err != nil {
			if !secretsCreateMissing {
				return fmt.Errorf("failed to patch secret %s: %w", secretsName, err)
			}

			createArgs := []string{"-n", cfg.Namespace, "create", "secret", "generic", secretsName}
			for _, key := range sortedKeys(resolved) {
				createArgs = append(createArgs, "--from-literal="+key+"="+resolved[key])
			}
			if createErr := runKubectl(createArgs...); createErr != nil {
				return fmt.Errorf("failed to patch or create secret %s: %w", secretsName, err)
			}
			fmt.Printf("created secret: %s (namespace: %s, keys synced: %d)\n", secretsName, cfg.Namespace, len(resolved))
		} else {
			fmt.Printf("patched secret: %s (namespace: %s, keys synced: %d)\n", secretsName, cfg.Namespace, len(resolved))
		}

		if len(missing) > 0 {
			sort.Strings(missing)
			fmt.Printf("skipped missing keys: %s\n", strings.Join(missing, ", "))
		}

		if secretsRestart {
			fmt.Printf("restarting deployment/%s in namespace %s...\n", deployedConfigDeploymentName(), cfg.Namespace)
			if err := runKubectl("-n", cfg.Namespace, "rollout", "restart", "deployment/"+deployedConfigDeploymentName()); err != nil {
				return fmt.Errorf("secret synced but failed to restart deployment: %w", err)
			}
			if err := runKubectl("-n", cfg.Namespace, "rollout", "status", "deployment/"+deployedConfigDeploymentName(), "--timeout=180s"); err != nil {
				return fmt.Errorf("deployment restart triggered but rollout did not complete: %w", err)
			}
			fmt.Println("deployment restart complete")
		} else {
			fmt.Println("note: restart OpenClaw deployment to reload environment variables")
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// upgrade command
// ---------------------------------------------------------------------------

const (
	helmRepoName    = "openclaw"
	helmRepoURL     = "https://serhanekicii.github.io/openclaw-helm"
	helmChartRef    = "openclaw/openclaw"
	helmReleaseName = "openclaw"
	recipesConfRel  = "scripts/recipes/recipes.conf"
	recipesConfKey  = "CHART_VERSION_OPENCLAW"
)

// helmRelease holds the fields we care about from `helm list -o json`.
type helmRelease struct {
	Name       string `json:"name"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
	Status     string `json:"status"`
}

// helmSearchEntry holds a single row from `helm search repo -o json`.
type helmSearchEntry struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	AppVersion string `json:"app_version"`
}

// chartVersionFromChart extracts the version suffix from a chart string like "openclaw-1.3.18".
func chartVersionFromChart(chart string) string {
	idx := strings.LastIndex(chart, "-")
	if idx < 0 {
		return chart
	}
	return chart[idx+1:]
}

// helmRepoEnsure ensures the openclaw Helm repo is added and updated.
func helmRepoEnsure() error {
	// Idempotent add
	cmd := exec.Command("helm", "repo", "add", helmRepoName, helmRepoURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run() // may already exist, ignore error

	cmd = exec.Command("helm", "repo", "update", helmRepoName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm repo update failed: %w", err)
	}
	return nil
}

// helmLatestStableVersion queries the Helm repo for the latest chart version.
func helmLatestStableVersion() (string, string, error) {
	out, err := exec.Command("helm", "search", "repo", helmChartRef, "-o", "json").Output()
	if err != nil {
		return "", "", fmt.Errorf("helm search repo failed: %w", err)
	}

	var entries []helmSearchEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return "", "", fmt.Errorf("failed to parse helm search output: %w", err)
	}

	for _, e := range entries {
		if e.Name == helmChartRef {
			return e.Version, e.AppVersion, nil
		}
	}
	return "", "", fmt.Errorf("chart %s not found in search results", helmChartRef)
}

// helmCurrentRelease queries the deployed Helm release for openclaw.
func helmCurrentRelease(namespace string) (*helmRelease, error) {
	out, err := exec.Command("helm", "list", "-n", namespace, "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("helm list failed: %w", err)
	}

	var releases []helmRelease
	if err := json.Unmarshal(out, &releases); err != nil {
		return nil, fmt.Errorf("failed to parse helm list output: %w", err)
	}

	for i := range releases {
		if releases[i].Name == helmReleaseName {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("no Helm release named %q found in namespace %s", helmReleaseName, namespace)
}

// updateRecipesConfPin updates CHART_VERSION_OPENCLAW in recipes.conf.
func updateRecipesConfPin(newVersion string) error {
	return updateRecipesConfPinAt(recipesConfRel, newVersion)
}

// updateRecipesConfPinAt updates CHART_VERSION_OPENCLAW in the given file path.
func updateRecipesConfPinAt(path, newVersion string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	re := regexp.MustCompile(`^(` + regexp.QuoteMeta(recipesConfKey) + `)=(.*)$`)
	var lines []string
	updated := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			lines = append(lines, m[1]+"="+newVersion)
			updated = true
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	if !updated {
		return fmt.Errorf("key %s not found in %s", recipesConfKey, path)
	}

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// detectRunningImageTag queries the actual running image tag of the main container.
// Returns empty string if detection fails (non-fatal).
func detectRunningImageTag(namespace string) string {
	out, err := runKubectlOutput(
		"-n", namespace,
		"get", "deploy", deployedConfigDeploymentName(),
		"-o", "jsonpath={.spec.template.spec.containers[?(@.name==\"main\")].image}",
	)
	if err != nil {
		return ""
	}
	image := strings.TrimSpace(string(out))
	// Image format: ghcr.io/openclaw/openclaw:2026.2.17
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		return image[idx+1:]
	}
	return ""
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade OpenClaw Helm release to the latest stable chart version",
	Long: `Upgrade the deployed OpenClaw Helm release to the latest stable version.

Steps:
  1. Ensure openclaw Helm repo is added and up-to-date
  2. Query the latest stable chart version
  3. Compare with the currently deployed release
  4. Perform helm upgrade --reset-then-reuse-values --version <target>
  5. Wait for rollout to complete
  6. Update the CHART_VERSION_OPENCLAW pin in recipes.conf

Use --version to target a specific chart version instead of latest.
Use --dry-run to preview the upgrade without applying it.
Use --skip-pin-update to skip updating recipes.conf.

Examples:
  netcup-claw upgrade
  netcup-claw upgrade --dry-run
  netcup-claw upgrade --version 1.3.20
  netcup-claw upgrade --skip-pin-update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := openclawConfig()

		// Step 1: Ensure Helm repo
		fmt.Println("Updating Helm repo...")
		if err := helmRepoEnsure(); err != nil {
			return err
		}

		// Step 2: Determine target version
		targetVersion := strings.TrimSpace(upgradeVersion)
		var latestAppVersion string
		if targetVersion == "" {
			v, av, err := helmLatestStableVersion()
			if err != nil {
				return fmt.Errorf("failed to determine latest stable version: %w", err)
			}
			targetVersion = v
			latestAppVersion = av
		}

		// Step 3: Get currently deployed version
		rel, err := helmCurrentRelease(cfg.Namespace)
		if err != nil {
			return fmt.Errorf("failed to query current release: %w", err)
		}
		currentVersion := chartVersionFromChart(rel.Chart)

		fmt.Printf("\ncurrent: chart=%s  app=%s  status=%s\n", currentVersion, rel.AppVersion, rel.Status)
		if latestAppVersion != "" {
			fmt.Printf("target:  chart=%s  app=%s\n", targetVersion, latestAppVersion)
		} else {
			fmt.Printf("target:  chart=%s\n", targetVersion)
		}

		// Check the actual running image tag to detect stale images from
		// prior --reuse-values upgrades.
		runningAppVersion := detectRunningImageTag(cfg.Namespace)
		if runningAppVersion != "" && runningAppVersion != rel.AppVersion {
			fmt.Printf("running: app=%s (image tag differs from chart metadata)\n", runningAppVersion)
		}

		chartMatch := currentVersion == targetVersion
		imageMatch := runningAppVersion == "" || runningAppVersion == latestAppVersion
		if chartMatch && imageMatch && rel.Status == "deployed" && !upgradeForce {
			fmt.Println("\nalready at target version — nothing to do")
			return nil
		}
		if chartMatch && !imageMatch && !upgradeForce {
			fmt.Printf("\nchart version matches but running image is stale (%s != %s)\n", runningAppVersion, latestAppVersion)
			fmt.Println("re-upgrading to apply chart-default image tag...")
		}

		// Step 4: Perform upgrade
		if upgradeDryRun {
			fmt.Printf("\ndry-run: would run 'helm upgrade %s %s --reset-then-reuse-values --version %s -n %s --wait --timeout 5m'\n",
				helmReleaseName, helmChartRef, targetVersion, cfg.Namespace)
			if !upgradeSkipPinUpdate {
				fmt.Printf("dry-run: would update %s=%s in %s\n", recipesConfKey, targetVersion, recipesConfRel)
			}
			return nil
		}

		fmt.Printf("\nupgrading %s -> %s ...\n", currentVersion, targetVersion)
		upgradeArgs := []string{
			"upgrade", helmReleaseName, helmChartRef,
			"--reset-then-reuse-values",
			"--version", targetVersion,
			"-n", cfg.Namespace,
			"--wait",
			"--timeout", "5m",
		}
		upgradeCmd := exec.Command("helm", upgradeArgs...)
		upgradeCmd.Stdout = os.Stdout
		upgradeCmd.Stderr = os.Stderr
		if err := upgradeCmd.Run(); err != nil {
			return fmt.Errorf("helm upgrade failed: %w", err)
		}

		fmt.Println("upgrade complete")

		// Step 5: Wait for rollout
		fmt.Println("waiting for rollout...")
		if err := runKubectl("-n", cfg.Namespace, "rollout", "status",
			"deployment/"+deployedConfigDeploymentName(), "--timeout=180s"); err != nil {
			return fmt.Errorf("rollout did not complete: %w", err)
		}

		// Step 6: Update recipes.conf pin
		if !upgradeSkipPinUpdate {
			if err := updateRecipesConfPin(targetVersion); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update %s: %v\n", recipesConfRel, err)
			} else {
				fmt.Printf("updated %s=%s in %s\n", recipesConfKey, targetVersion, recipesConfRel)
			}
		}

		return nil
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
		cfg, pod, err := resolveOpenClawPod()
		if err != nil {
			return err
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
		_ = ensureKubeAPIReachableWithTunnel()

		// 1. SSH Tunnel status
		tun := tunnelConfig()
		var tunnelRunning bool
		if tun.Host != "" {
			tunMgr := tunnel.New(tun.User, tun.Host, tun.LocalPort, tun.RemoteHost, tun.RemotePort)
			tunnelRunning = tunMgr.IsRunning()
			fmt.Printf("tunnel:       %s", boolStatus(tunnelRunning))
			if tunnelRunning {
				fmt.Printf(" (localhost:%s -> %s:%s via %s@%s)", tun.LocalPort, tun.RemoteHost, tun.RemotePort, tun.User, tun.Host)
			}
			fmt.Println()
		} else {
			fmt.Printf("tunnel:       unconfigured (set TUNNEL_HOST to enable)\n")
		}

		// 2. Kubernetes API reachability
		apiReachable := probeKubeAPI()
		fmt.Printf("kube-api:     %s\n", boolStatus(apiReachable))

		// 3. Port-forward status
		mgr := pfManager(cfg, "")
		pfStatus := mgr.Status()
		fmt.Printf("port-forward: %s", pfStatus.State)
		if pfStatus.PID > 0 {
			fmt.Printf(" (pid %d)", pfStatus.PID)
		}
		fmt.Println()

		// 4. OpenClaw service resolution
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

		// Overall health: API reachable (directly or via tunnel) + pf running + svc + pod resolved
		apiOrTunnel := apiReachable || tunnelRunning
		healthy := apiOrTunnel && pfStatus.State == portforward.StateRunning && svcErr == nil && podErr == nil
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

	// Tunnel flags (global; used by port-forward start and status)
	rootCmd.PersistentFlags().StringVar(&tunHost, "tunnel-host", "", "SSH tunnel host (default: $TUNNEL_HOST or $MGMT_HOST)")
	rootCmd.PersistentFlags().StringVar(&tunUser, "tunnel-user", "", "SSH tunnel user (default: $TUNNEL_USER or ops)")
	rootCmd.PersistentFlags().StringVar(&tunLocalPort, "tunnel-local-port", "", "SSH tunnel local port (default: $TUNNEL_LOCAL_PORT or 6443)")
	rootCmd.PersistentFlags().StringVar(&tunRemoteHost, "tunnel-remote-host", "", "SSH tunnel remote host (default: $TUNNEL_REMOTE_HOST or 127.0.0.1)")
	rootCmd.PersistentFlags().StringVar(&tunRemotePort, "tunnel-remote-port", "", "SSH tunnel remote port (default: $TUNNEL_REMOTE_PORT or 6443)")

	portForwardCmd.AddCommand(portForwardStartCmd)
	portForwardCmd.AddCommand(portForwardStopCmd)
	portForwardCmd.AddCommand(portForwardStatusCmd)

	rootCmd.AddCommand(portForwardCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(openclawCmd)
	configCmd.PersistentFlags().StringVar(&configWorkspaceDir, "workspace-dir", "", "Local config workspace root (default: scripts/recipes/openclaw/config)")
	configCmd.PersistentFlags().StringVar(&configBackupPath, "backup-path", "", "Directory or file path for config backups (default: <workspace-dir>/backup, use 'off' to disable on deploy)")
	configDeployCmd.Flags().StringVar(&configDeployFile, "file", "", "Local OpenClaw config JSON file to deploy (default: scripts/recipes/openclaw/openclaw.json)")
	configValidateCmd.Flags().StringVar(&configDeployFile, "file", "", "Local OpenClaw config JSON file to validate (default: scripts/recipes/openclaw/openclaw.json)")
	configDeployCmd.Flags().BoolVar(&configDeploySyncPVC, "sync-runtime", true, "Write the validated config directly into the runtime PVC before rollout restart")
	configCmd.AddCommand(configBackupCmd)
	configCmd.AddCommand(configPullCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configDeployCmd)
	rootCmd.AddCommand(configCmd)
	approvalsCmd.PersistentFlags().StringVar(&approvalsWorkspaceDir, "workspace-dir", "", "Local approvals workspace root (default: scripts/recipes/openclaw/approvals)")
	approvalsCmd.PersistentFlags().StringVar(&approvalsBackupPath, "backup-path", "", "Directory or file path for approvals backups (default: <workspace-dir>/backup, use 'off' to disable on deploy)")
	approvalsDeployCmd.Flags().StringVar(&approvalsDeployFile, "file", "", "Local approvals JSON file to deploy (default: <workspace-dir>/approvals.json)")
	approvalsCmd.AddCommand(approvalsBackupCmd)
	approvalsCmd.AddCommand(approvalsPullCmd)
	approvalsCmd.AddCommand(approvalsDeployCmd)
	rootCmd.AddCommand(approvalsCmd)
	cronCmd.PersistentFlags().StringVar(&cronWorkspaceDir, "workspace-dir", "", "Local cron workspace root (default: scripts/recipes/openclaw/cron)")
	cronCmd.PersistentFlags().StringVar(&cronBackupPath, "backup-path", "", "Directory or file path for cron jobs backups (default: <workspace-dir>/backup, use 'off' to disable pre-sync backup in deploy)")
	cronDeployCmd.Flags().StringVar(&cronDeployFile, "file", "", "Local cron jobs JSON file to sync via deploy (default: <workspace-dir>/jobs.json)")
	cronSyncCmd.Flags().StringVar(&cronDeployFile, "file", "", "Local cron jobs JSON file to sync (default: <workspace-dir>/jobs.json)")
	cronDeployCmd.Flags().BoolVar(&cronPrune, "prune", false, "Delete runtime cron jobs that are missing from local file during sync")
	cronSyncCmd.Flags().BoolVar(&cronPrune, "prune", false, "Delete runtime cron jobs that are missing from local file during sync")
	cronDeleteCmd.Flags().BoolVar(&cronDeleteByName, "name", false, "Treat argument as exact runtime job name instead of id")
	cronCmd.AddCommand(cronBackupCmd)
	cronCmd.AddCommand(cronPullCmd)
	cronCmd.AddCommand(cronDeployCmd)
	cronCmd.AddCommand(cronSyncCmd)
	cronCmd.AddCommand(cronDeleteCmd)
	rootCmd.AddCommand(cronCmd)
	rootCmd.AddCommand(codexLoginCmd)
	skillsCmd.PersistentFlags().StringVar(&skillName, "skill", "hormuz-ais-watch", "Default skill directory name under OpenClaw workspace (overridden by positional [skill])")
	skillsCmd.PersistentFlags().StringVar(&skillsWorkspaceDir, "workspace-dir", "", "Local skills workspace root (default: scripts/recipes/openclaw/skills)")
	skillsCmd.PersistentFlags().StringVar(&skillsBackupPath, "backup-path", "", "Directory for skill backups (default: <workspace-dir>/backup, use 'off' to disable on deploy)")
	skillsPullCmd.Flags().BoolVar(&skillsPullAll, "all", false, "Pull all runtime skills into local workspace")
	skillsPullCmd.Flags().StringSliceVar(&skillsExclude, "exclude", []string{"hormuz-ais-watch"}, "Skill names to exclude when using --all (repeatable)")
	skillsDeployCmd.Flags().StringVar(&skillsSourceDir, "source-dir", "", "Local skill directory to deploy (default: <workspace-dir>/<skill>)")
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsBackupCmd)
	skillsCmd.AddCommand(skillsPullCmd)
	skillsCmd.AddCommand(skillsDeployCmd)
	rootCmd.AddCommand(skillsCmd)
	secretsSyncCmd.Flags().StringVar(&secretsEnvFile, "env-file", ".env", "Local env file with secret values (takes precedence over process env)")
	secretsSyncCmd.Flags().StringVar(&secretsName, "name", "openclaw-credentials", "Kubernetes Secret name to patch/create")
	secretsSyncCmd.Flags().BoolVar(&secretsCreateMissing, "create-missing", true, "Create the secret if it does not exist")
	secretsSyncCmd.Flags().BoolVar(&secretsRestart, "restart", false, "Restart deployment/openclaw after a successful secret sync")
	secretsCmd.AddCommand(secretsSyncCmd)
	rootCmd.AddCommand(secretsCmd)
	agentsCmd.PersistentFlags().StringVar(&agentsWorkspaceDir, "workspace-dir", "", "Local agent-workspace root (default: scripts/recipes/openclaw/agent-workspace)")
	agentsCmd.AddCommand(agentsBackupCmd)
	agentsCmd.AddCommand(agentsDeployCmd)
	rootCmd.AddCommand(agentsCmd)
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Target chart version (default: latest stable)")
	upgradeCmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Preview upgrade without applying")
	upgradeCmd.Flags().BoolVar(&upgradeSkipPinUpdate, "skip-pin-update", false, "Skip updating CHART_VERSION_OPENCLAW in recipes.conf")
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "Force upgrade even if chart version matches")
	rootCmd.AddCommand(upgradeCmd)
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

// tunnelParams holds SSH tunnel connection parameters
type tunnelParams struct {
	Host       string
	User       string
	LocalPort  string
	RemoteHost string
	RemotePort string
}

// tunnelConfig builds tunnel connection parameters from flags and environment.
// Mirrors the precedence used by netcup-kube ssh tunnel.
func tunnelConfig() tunnelParams {
	p := tunnelParams{}

	// Host
	p.Host = tunHost
	if p.Host == "" {
		p.Host = os.Getenv("TUNNEL_HOST")
	}
	if p.Host == "" {
		p.Host = os.Getenv("MGMT_HOST")
	}
	if p.Host == "" {
		p.Host = os.Getenv("MGMT_IP")
	}

	// User
	p.User = tunUser
	if p.User == "" {
		p.User = os.Getenv("TUNNEL_USER")
	}
	if p.User == "" {
		p.User = os.Getenv("MGMT_USER")
	}
	if p.User == "" {
		p.User = "ops"
	}

	// Local port
	p.LocalPort = tunLocalPort
	if p.LocalPort == "" {
		p.LocalPort = os.Getenv("TUNNEL_LOCAL_PORT")
	}
	if p.LocalPort == "" {
		p.LocalPort = "6443"
	}

	// Remote host
	p.RemoteHost = tunRemoteHost
	if p.RemoteHost == "" {
		p.RemoteHost = os.Getenv("TUNNEL_REMOTE_HOST")
	}
	if p.RemoteHost == "" {
		p.RemoteHost = "127.0.0.1"
	}

	// Remote port
	p.RemotePort = tunRemotePort
	if p.RemotePort == "" {
		p.RemotePort = os.Getenv("TUNNEL_REMOTE_PORT")
	}
	if p.RemotePort == "" {
		p.RemotePort = "6443"
	}

	return p
}

// pfManager creates a port-forward Manager from the openclaw config.
// If target is empty, cfg.FallbackSvc is used.
func pfManager(cfg openclaw.Config, target string) *portforward.Manager {
	if strings.TrimSpace(target) == "" {
		target = cfg.FallbackSvc
	}
	return portforward.New(cfg.Namespace, target, cfg.LocalPort, cfg.RemotePort)
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
