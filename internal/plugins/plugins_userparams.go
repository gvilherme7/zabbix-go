package plugins

import (
	"context"
	"fmt"
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

// WithParams returns a copy of the plugin with $1.."$9" in its command
// replaced by the given values, matching the standard Zabbix
// "UserParameter=key[*],cmd $1" convention. Substitutions are shell-quoted:
// the parameter values arrive over the network in the active-checks/config
// response, and naive string substitution into a shell command would let a
// value containing shell metacharacters escape its argument.
func (p *UserParamPlugin) WithParams(params []string) *UserParamPlugin {
	cmd := p.command
	for i := len(params); i >= 1; i-- {
		placeholder := fmt.Sprintf("$%d", i)
		cmd = strings.ReplaceAll(cmd, placeholder, quoteArg(params[i-1]))
	}
	return &UserParamPlugin{key: p.key, command: cmd}
}

// quoteArg shell-quotes a single value for safe substitution into a command
// string. POSIX shells: wrap in single quotes, escaping embedded single
// quotes. Windows cmd.exe has no fully safe quoting for arbitrary strings;
// wrapping in double quotes and doubling embedded ones covers the common
// case, same practical limitation the official Windows agent has.
func quoteArg(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
