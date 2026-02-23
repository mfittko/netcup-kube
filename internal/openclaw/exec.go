package openclaw

import "os/exec"

// defaultExec runs an external command and returns its combined output
func defaultExec(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}
