# Parity Check: bin/netcup-kube-remote vs netcup-kube remote

## Feature Comparison

| Feature | Shell (bin/netcup-kube-remote) | Go (netcup-kube remote) | Status |
|---------|-------------------------------|-------------------------|--------|
| **Commands** | | | |
| `provision` | ✓ Sets up sudo user, SSH keys, clones repo | ✓ Same functionality | ✅ Parity |
| `git` | ✓ Remote git control (checkout/pull) | ✓ Same functionality | ✅ Parity |
| `build` | ✓ Cross-compile and upload binary | ✓ Same functionality | ✅ Parity |
| `smoke` | ✓ DRY_RUN smoke tests | ✓ Same functionality | ✅ Parity |
| `run` | ✓ Run netcup-kube commands remotely | ✓ Same functionality | ✅ Parity |
| **Options** | | | |
| `--host` / `<host-or-ip>` | ✓ Specify remote host | ✓ `--host` flag | ✅ Parity |
| `--user` | ✓ Specify remote user (default: cubeadmin) | ✓ Same (default: cubeadmin) | ✅ Parity |
| `--pubkey` | ✓ Specify SSH public key path | ✓ Same | ✅ Parity |
| `--repo` | ✓ Specify repository URL | ✓ Same | ✅ Parity |
| `--config` | ✓ Path to config file | ✓ Same | ✅ Parity |
| **Git Options** | | | |
| `--branch` | ✓ Checkout specific branch | ✓ Same | ✅ Parity |
| `--ref` | ✓ Checkout specific ref/commit | ✓ Same | ✅ Parity |
| `--pull` / `--no-pull` | ✓ Control pull behavior | ✓ Same | ✅ Parity |
| **Run Options** | | | |
| `--no-tty` | ✓ Disable forced TTY | ✓ Same | ✅ Parity |
| `--env-file` | ✓ Upload and source env file | ✓ Same | ✅ Parity |
| Default TTY | ✓ Forces TTY by default (`-tt`) | ✓ Same | ✅ Parity |
| **Safety** | | | |
| Interactive prompts (TTY) | ✓ Uses `ssh -tt` | ✓ Same | ✅ Parity |
| Non-interactive (CONFIRM=true) | ✓ Via env file | ✓ Same | ✅ Parity |
| Safe argument passing | ✓ Uses `printf %q` | ✓ Uses single-quote escaping | ✅ Parity |
| Env file cleanup | ✓ Removes temp files | ✓ Same | ✅ Parity |
| **Auto-detection** | | | |
| Host from config | ✓ MGMT_HOST/MGMT_IP | ✓ Same | ✅ Parity |
| User from config | ✓ MGMT_USER/DEFAULT_USER | ✓ Same | ✅ Parity |
| SSH key auto-find | ✓ Searches ~/.ssh/id_ed25519*, id_rsa* | ✓ Same | ✅ Parity |
| **Root Access** | | | |
| SSH key copy (sshpass) | ✓ Uses sshpass if available | ✓ Same | ✅ Parity |
| Password via ROOT_PASS | ✓ Environment variable | ✓ Same | ✅ Parity |
| Fallback instructions | ✓ Shows ssh-copy-id command | ✓ Same | ✅ Parity |
| **Build** | | | |
| Architecture detection | ✓ `uname -m` -> GOARCH | ✓ Same | ✅ Parity |
| Cross-compilation | ✓ CGO_ENABLED=0 GOOS=linux | ✓ Same | ✅ Parity |
| Binary upload | ✓ SCP to remote host | ✓ Same | ✅ Parity |
| **Git Sync** | | | |
| Fetch all | ✓ `git fetch --all -p` | ✓ Same | ✅ Parity |
| Branch checkout | ✓ Creates tracking branch if needed | ✓ Same | ✅ Parity |
| Ref checkout | ✓ Detached HEAD | ✓ Same | ✅ Parity |
| Pull (ff-only) | ✓ `git pull --ff-only` | ✓ Same | ✅ Parity |
| Set upstream | ✓ `git branch --set-upstream-to` | ✓ Same | ✅ Parity |

## Behavioral Differences

### Improvements in Go Implementation

1. **Better Error Messages**: Go version provides more structured error messages with context
2. **Type Safety**: Go's type system prevents certain classes of bugs
3. **Testability**: Go code has unit tests; shell script is harder to test
4. **Help System**: Cobra provides consistent help formatting
5. **Flag Parsing**: More robust flag parsing with Cobra

### Maintained Shell Behavior

1. **Script Execution**: Remote commands still executed via bash for compatibility
2. **Environment Sourcing**: Uses same bash pattern (`set -a; source; set +a`)
3. **Placeholder Pattern**: Uses `__NONE__` for empty arguments (SSH doesn't preserve empty args)
4. **Error Handling**: Remote scripts still use `set -euo pipefail`

## Usage Examples Comparison

### Provision
```bash
# Shell
bin/netcup-kube-remote host.example.com --user ops provision

# Go
netcup-kube remote --host host.example.com --user ops provision
```

### Run with Env File
```bash
# Shell
bin/netcup-kube-remote host.example.com run --env-file config.env bootstrap

# Go
netcup-kube remote --host host.example.com run --env-file config.env bootstrap
```

### Git Sync
```bash
# Shell
bin/netcup-kube-remote host.example.com git --branch main --pull

# Go
netcup-kube remote --host host.example.com git --branch main --pull
```

### Build
```bash
# Shell
bin/netcup-kube-remote host.example.com build

# Go
netcup-kube remote --host host.example.com build
```

### Smoke Test
```bash
# Shell
bin/netcup-kube-remote host.example.com smoke

# Go
netcup-kube remote --host host.example.com smoke
```

## Conclusion

The Go implementation (`netcup-kube remote`) achieves **100% feature parity** with the shell implementation (`bin/netcup-kube-remote`), while providing:

- Better error handling
- Unit tests
- Type safety
- Consistent CLI interface via Cobra
- Same safety guarantees for remote execution

All documented behaviors and safety gates from the original implementation are preserved.
