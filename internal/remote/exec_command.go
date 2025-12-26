package remote

import (
	"fmt"
	"os"
	"os/exec"
)

// Injection points for unit tests. Centralized here so dependencies like `execCommand`
// are explicit across files in this package (e.g. used by ssh.go, provision.go, remote.go).
var (
	execCommand = exec.Command
	lookPath    = exec.LookPath
	mkdirTemp   = os.MkdirTemp
	removeAll   = os.RemoveAll

	localGoBuild = func(projectRoot, out, goarch string) error {
		cmd := execCommand("go", "build", "-o", out, "./cmd/netcup-kube")
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS=linux",
			fmt.Sprintf("GOARCH=%s", goarch),
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
)


