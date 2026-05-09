//go:build windows

package plugins

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modkernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

func init() {
	Registry["system.cpu.load"] = &CpuLoadPlugin{firstRun: true}
	Registry["vm.memory.size.total"] = &MemInfoPlugin{key: "vm.memory.size.total"}
	Registry["vm.memory.size.available"] = &MemInfoPlugin{key: "vm.memory.size.available"}
}

type CpuLoadPlugin struct {
	mu        sync.Mutex
	lastTotal uint64
	lastIdle  uint64
	firstRun  bool
}

func (p *CpuLoadPlugin) Key() string { return "system.cpu.load" }

func (p *CpuLoadPlugin) Collect() (string, error) {
	var idle, kernel, user syscall.Filetime
	ret, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return "0.00", err
	}

	idleTicks := uint64(idle.HighDateTime)<<32 + uint64(idle.LowDateTime)
	kernelTicks := uint64(kernel.HighDateTime)<<32 + uint64(kernel.LowDateTime)
	userTicks := uint64(user.HighDateTime)<<32 + uint64(user.LowDateTime)

	t2 := kernelTicks + userTicks
	i2 := idleTicks

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

type MemInfoPlugin struct {
	key string
}

func (p *MemInfoPlugin) Key() string { return p.key }

type memorystatusex struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

func (p *MemInfoPlugin) Collect() (string, error) {
	var memInfo memorystatusex
	memInfo.dwLength = uint32(unsafe.Sizeof(memInfo))

	ret, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memInfo)))
	if ret == 0 {
		return "0", err
	}

	if p.key == "vm.memory.size.total" {
		return fmt.Sprintf("%d", memInfo.ullTotalPhys), nil
	} else if p.key == "vm.memory.size.available" {
		return fmt.Sprintf("%d", memInfo.ullAvailPhys), nil
	}
	return "0", fmt.Errorf("unknown key")
}
