package remote

// Client is the minimal interface needed by the remote orchestration functions.
// It allows unit tests to provide fakes without shelling out to real ssh/scp.
type Client interface {
	TestConnection() error
	Execute(command string, args []string, forceTTY bool) error
	ExecuteScript(script string, args []string) error
	Upload(localPath, remotePath string) error

	// RunCommandString executes a raw remote shell command string via SSH.
	RunCommandString(cmdString string, forceTTY bool) error

	// OutputCommand runs a remote command and returns stdout (used for simple probes like uname -m).
	OutputCommand(command string, args []string) ([]byte, error)
}
