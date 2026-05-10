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

func (p *StaticPlugin) Key() string { return p.key }
func (p *StaticPlugin) Collect() (string, error) { return p.val, nil }

// FuncPlugin uses a callback function
type FuncPlugin struct {
	key string
	fn  func() (string, error)
}

func (p *FuncPlugin) Key() string { return p.key }
func (p *FuncPlugin) Collect() (string, error) { return p.fn() }
