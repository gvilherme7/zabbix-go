//go:build linux

package plugins

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

func init() {
	// Register native fast Linux hardware plugins
	Registry["system.cpu.load"] = &CpuLoadPlugin{firstRun: true}
	Registry["vm.memory.size.total"] = &MemInfoPlugin{targetMatch: "MemTotal:", key: "vm.memory.size.total"}
	Registry["vm.memory.size.available"] = &MemInfoPlugin{targetMatch: "MemAvailable:", key: "vm.memory.size.available"}
}

// CpuLoadPlugin parses /proc/stat to calculate rough CPU % over an instant.
type CpuLoadPlugin struct {
	mu        sync.Mutex
	lastTotal uint64
	lastIdle  uint64
	firstRun  bool
}

func (p *CpuLoadPlugin) Key() string { return "system.cpu.load" }
func (p *CpuLoadPlugin) Collect() (string, error) {
	t2, i2 := p.getStats()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.firstRun {
		p.lastTotal = t2
		p.lastIdle = i2
		p.firstRun = false
		return "0.00", nil
	}

	totalDelta := t2 - p.lastTotal
	idleDelta := i2 - p.lastIdle

	p.lastTotal = t2
	p.lastIdle = i2

	if totalDelta == 0 {
		return "0.00", nil
	}

	usage := 100.0 * (1.0 - (float64(idleDelta) / float64(totalDelta)))
	return fmt.Sprintf("%.2f", usage), nil
}

func (p *CpuLoadPlugin) getStats() (total, idle uint64) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 1, 1
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 4 && fields[0] == "cpu" {
			for i := 1; i < len(fields); i++ {
				val, _ := strconv.ParseUint(fields[i], 10, 64)
				total += val
				if i == 4 { // index 4 is the idle metric position in /proc/stat
					idle = val
				}
			}
		}
	}
	return
}

// MemInfoPlugin reads /proc/meminfo instantly using strict matching for extremely low allocation 
type MemInfoPlugin struct {
	key         string
	targetMatch string
}

func (p *MemInfoPlugin) Key() string { return p.key }
func (p *MemInfoPlugin) Collect() (string, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return "0", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, p.targetMatch) {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return fmt.Sprintf("%d", kb*1024), nil
				}
			}
		}
	}
	return "0", fmt.Errorf("metric not found")
}
