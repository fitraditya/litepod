package system

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Metrics implements domain.SystemMetrics using gopsutil.
type Metrics struct{}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) CPUPercent() (float64, error) {
	pcts, err := cpu.Percent(0, false)
	if err != nil {
		return 0, fmt.Errorf("cpu percent: %w", err)
	}
	if len(pcts) == 0 {
		return 0, fmt.Errorf("no cpu data returned")
	}
	return pcts[0], nil
}

func (m *Metrics) MemAvailMB() (uint64, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, fmt.Errorf("virtual memory: %w", err)
	}
	return vm.Available / 1024 / 1024, nil
}
