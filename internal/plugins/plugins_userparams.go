package plugins

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// commandTimeout bounds how long a UserParameter shell command may run.
// Without it, a hanging command (blocked I/O, unresponsive network call)
// leaks its OS process and the goroutine collecting it forever, since the
// caller's own timeout only stops waiting for the result — it doesn't kill
// the child process.
const commandTimeout = 10 * time.Second

// UserParamPlugin executes a custom shell command
type UserParamPlugin struct {
	key     string
	command string
}

func NewUserParamPlugin(key, command string) *UserParamPlugin {
	return &UserParamPlugin{
		key:     key,
		command: command,
	}
}

func (p *UserParamPlugin) Key() string {
	return p.key
}

func (p *UserParamPlugin) Collect() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", p.command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.command)
	}

	out, err := cmd.Output()
	result := strings.TrimSpace(string(out))
	if result == "" && err != nil {
		return "", err
	}
	return result, nil
}
