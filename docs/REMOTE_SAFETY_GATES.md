# Remote Execution Safety Gates

The Go-based remote execution engine preserves all safety gates from the original shell implementation.

## Interactive Confirmation (TTY)

By default, the `remote run` command forces a TTY (`-tt` flag to SSH), allowing interactive prompts to work correctly:

```bash
# Interactive mode (default) - prompts work
netcup-kube remote run bootstrap

# Equivalent to:
ssh -tt user@host 'sudo -E bash -lc "..." netcup-kube bootstrap'
```

This ensures that recipes requiring interactive confirmation can read from `/dev/tty` even when run remotely.

## Non-Interactive Confirmation (CONFIRM=true)

For non-interactive execution (e.g., CI/CD), use `--no-tty` with `CONFIRM=true` in an env file:

```bash
# Create env file with confirmation
cat > /tmp/bootstrap.env << EOF
CONFIRM=true
DRY_RUN=false
EOF

# Non-interactive mode with automatic confirmation
netcup-kube remote run --no-tty --env-file /tmp/bootstrap.env bootstrap
```

The env file is:
1. Uploaded to the remote host as `/tmp/netcup-kube-remote.env.<PID>`
2. Sourced before running the command (`set -a; source <env>; set +a`)
3. Automatically cleaned up after execution

## Safety Gate Preservation

### Original Behavior (bin/netcup-kube-remote)
- Uses `ssh -tt` for interactive mode
- Sources env files with `set -a; source <file>; set +a`
- Supports both TTY and non-TTY modes
- Cleans up temporary env files

### Go Implementation (netcup-kube remote)
- **Identical**: Uses `ssh -tt` for interactive mode (default)
- **Identical**: Sources env files with same bash pattern
- **Identical**: Supports `--no-tty` flag for non-interactive mode
- **Identical**: Cleans up temporary env files
- **Improved**: Safer argument passing with single-quote escaping

## Examples

### Interactive Bootstrap
```bash
# Prompts work, user can interactively confirm
netcup-kube remote --host example.com run bootstrap
```

### Non-Interactive Bootstrap (CI/CD)
```bash
# No prompts, auto-confirms with CONFIRM=true
cat > ci.env << EOF
CONFIRM=true
BASE_DOMAIN=example.com
ENABLE_UFW=true
EOF

netcup-kube remote --host example.com run --no-tty --env-file ci.env bootstrap
```

### Mixed Mode (Interactive with Env Override)
```bash
# Interactive prompts, but BASE_DOMAIN pre-configured
cat > config.env << EOF
BASE_DOMAIN=example.com
EOF

netcup-kube remote --host example.com run --env-file config.env bootstrap
```

## Destructive Operations

Recipes with destructive operations (e.g., `--uninstall`) require confirmation:

```bash
# Interactive: User prompted to confirm uninstall
netcup-kube remote run recipe install kube-prometheus-stack --uninstall

# Non-interactive: CONFIRM=true bypasses prompt
cat > uninstall.env << EOF
CONFIRM=true
EOF

netcup-kube remote run --no-tty --env-file uninstall.env recipe install kube-prometheus-stack --uninstall
```

## Implementation Details

### Argument Passing
Commands and arguments are safely escaped using single quotes:
```go
func shellEscape(s string) string {
    escaped := strings.ReplaceAll(s, "'", "'\\''")
    return fmt.Sprintf("'%s'", escaped)
}
```

This prevents shell injection even with complex arguments like:
```bash
netcup-kube remote run dns --domains "kube.example.com,app.example.com"
```

### Environment Sourcing
The remote runner script sources the env file before execution:
```bash
set -euo pipefail
env_file="${1:-}"
bin="${2:-}"
shift 2 || true

if [[ "${env_file}" != "__NONE__" && -n "${env_file}" ]]; then
  set -a
  source "${env_file}"
  set +a
fi

exec "${bin}" "$@"
```

This ensures environment variables are available to the netcup-kube process and any child processes.
