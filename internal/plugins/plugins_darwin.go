//go:build darwin

package plugins

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func init() {
	Registry["system.cpu.load"] = &CpuLoadPlugin{}
	Registry["vm.memory.size.total"] = &MemInfoPlugin{key: "vm.memory.size.total"}
	Registry["vm.memory.size.available"] = &MemInfoPlugin{key: "vm.memory.size.available"}
}

type CpuLoadPlugin struct{}

func (p *CpuLoadPlugin) Key() string { return "system.cpu.load" }

func (p *CpuLoadPlugin) Collect() (string, error) {
	out, err := exec.Command("top", "-l", "1", "-n", "0").Output()
	if err != nil {
		return "0.00", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CPU usage:") {
			parts := strings.Split(line, "idle")
			if len(parts) > 0 {
				lastComma := strings.LastIndex(parts[0], ",")
				if lastComma != -1 {
					idleStr := strings.TrimSpace(strings.ReplaceAll(parts[0][lastComma+1:], "%", ""))
					if idleF, err := strconv.ParseFloat(idleStr, 64); err == nil {
						return fmt.Sprintf("%.2f", 100.0-idleF), nil
					}
				}
			}
		}
	}
	return "0.00", fmt.Errorf("cpu info not found")
}

type MemInfoPlugin struct {
	key string
}

func (p *MemInfoPlugin) Key() string { return p.key }

func (p *MemInfoPlugin) Collect() (string, error) {
	if p.key == "vm.memory.size.total" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return "0", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	if p.key == "vm.memory.size.available" {
		out, err := exec.Command("vm_stat").Output()
		if err != nil {
			return "0", err
		}
		var pageSize uint64 = 4096 // default
		var free, inactive, speculative uint64
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "page size of") {
				parts := strings.Split(line, "page size of ")
				if len(parts) == 2 {
					pageSizeStr := strings.Split(parts[1], " ")[0]
					pageSize, _ = strconv.ParseUint(pageSizeStr, 10, 64)
				}
			} else if strings.HasPrefix(line, "Pages free:") {
				free = parseVmStatLine(line)
			} else if strings.HasPrefix(line, "Pages inactive:") {
				inactive = parseVmStatLine(line)
			} else if strings.HasPrefix(line, "Pages speculative:") {
				speculative = parseVmStatLine(line)
			}
		}
		available := (free + inactive + speculative) * pageSize
		return fmt.Sprintf("%d", available), nil
	}
	return "0", fmt.Errorf("unknown key")
}

func parseVmStatLine(line string) uint64 {
	parts := strings.Split(line, ":")
	if len(parts) == 2 {
		valStr := strings.TrimSpace(strings.TrimRight(parts[1], "."))
		val, _ := strconv.ParseUint(valStr, 10, 64)
		return val
	}
	return 0
}
