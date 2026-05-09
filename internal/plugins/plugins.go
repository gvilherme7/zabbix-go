package plugins

import (
	"fmt"
	"runtime"
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
