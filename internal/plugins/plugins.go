package plugins

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Plugin represents a lightweight metric collector
type Plugin interface {
	Key() string
	Collect() (string, error)
}

// Registry holds our active metric gatherers without CGO or big plugins
var Registry = make(map[string]Plugin)

// Lookup resolves a requested item key against the Registry. It first tries
// an exact match (covers every built-in plugin and non-parameterized
// UserParameters). If that fails and the key has the form "base[p1,p2,...]",
// it looks for a UserParameter registered as "base[*]" — the standard
// Zabbix pattern for parameterized UserParameters, e.g.
// "UserParameter=vfs.file.size[*],stat -c%s $1" matching a requested key of
// "vfs.file.size[/var/log/syslog]" — and returns a plugin instance with
// $1.."$9" substituted into its command.
func Lookup(key string) (Plugin, bool) {
	if p, ok := Registry[key]; ok {
		return p, true
	}

	base, params, ok := splitKeyParams(key)
	if !ok {
		return nil, false
	}

	if p, ok := Registry[base+"[*]"]; ok {
		if up, ok := p.(*UserParamPlugin); ok {
			return up.WithParams(params), true
		}
	}
	return nil, false
}

// splitKeyParams parses a Zabbix item key of the form "base[p1,p2,...]" into
// its base key and parameter list. Parameters may be double-quoted to
// contain commas, spaces, or brackets; a backslash escapes a quote inside a
// quoted parameter. Unquoted parameters are taken verbatim (including
// surrounding whitespace, matching Zabbix's own key parsing).
func splitKeyParams(key string) (base string, params []string, ok bool) {
	i := strings.IndexByte(key, '[')
	if i < 0 || !strings.HasSuffix(key, "]") {
		return "", nil, false
	}
	base = key[:i]
	inner := key[i+1 : len(key)-1]
	return base, parseParams(inner), true
}

func parseParams(inner string) []string {
	if inner == "" {
		return nil
	}
	var params []string
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '"' && cur.Len() == 0 && !quoted:
			quoted = true
		case c == '"' && quoted:
			quoted = false
		case c == '\\' && quoted && i+1 < len(inner) && inner[i+1] == '"':
			cur.WriteByte('"')
			i++
		case c == ',' && !quoted:
			params = append(params, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	params = append(params, cur.String())
	return params
}

func init() {
	// Register base cross-platform plugins
	Registry["agent.ping"] = &StaticPlugin{key: "agent.ping", val: "1"}
	Registry["golang.goroutines"] = &FuncPlugin{
		key: "golang.goroutines",
		fn: func() (string, error) {
			return fmt.Sprint(runtime.NumGoroutine()), nil
		},
	}
	Registry["golang.mem.alloc"] = &FuncPlugin{
		key: "golang.mem.alloc",
		fn: func() (string, error) {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return fmt.Sprint(m.Alloc), nil
		},
	}
	Registry["system.cpu.load"] = &FuncPlugin{
		key: "system.cpu.load",
		fn: func() (string, error) {
			data, err := os.ReadFile("/proc/loadavg")
			if err != nil {
				return "0", err
			}
			parts := strings.Split(string(data), " ")
			if len(parts) > 0 {
				return parts[0], nil
			}
			return "0", nil
		},
	}
	Registry["vm.memory.size"] = &FuncPlugin{
		key: "vm.memory.size",
		fn: func() (string, error) {
			data, err := os.ReadFile("/proc/meminfo")
			if err != nil {
				return "0", err
			}
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "MemTotal:") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						val, _ := strconv.ParseInt(parts[1], 10, 64)
						return fmt.Sprint(val * 1024), nil // Return in bytes
					}
				}
			}
			return "0", nil
		},
	}
}

// StaticPlugin always returns a fixed value (0 memory allocation after init)
type StaticPlugin struct {
	key string
	val string
}

func (p *StaticPlugin) Key() string              { return p.key }
func (p *StaticPlugin) Collect() (string, error) { return p.val, nil }

// FuncPlugin uses a callback function
type FuncPlugin struct {
	key string
	fn  func() (string, error)
}

func (p *FuncPlugin) Key() string              { return p.key }
func (p *FuncPlugin) Collect() (string, error) { return p.fn() }
