package plugins

import (
	"os/exec"
	"runtime"
	"strings"
)

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
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", p.command)
	} else {
		cmd = exec.Command("sh", "-c", p.command)
	}

	out, err := cmd.Output()
	result := strings.TrimSpace(string(out))
	if result == "" && err != nil {
		return "", err
	}
	return result, nil
}
