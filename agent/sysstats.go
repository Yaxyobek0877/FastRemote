package main

import "sync"

// DiskInfo — bitta disk/bo'lim holati
type DiskInfo struct {
	Mount   string  `json:"mount"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// GPUInfo — bitta grafik protsessor holati
type GPUInfo struct {
	Name        string  `json:"name"`
	UtilPercent float64 `json:"utilPercent"`
	MemUsed     uint64  `json:"memUsed"`
	MemTotal    uint64  `json:"memTotal"`
	MemPercent  float64 `json:"memPercent"`
	HasUtil     bool    `json:"hasUtil"` // util/mem mavjudmi (NVIDIA) yoki faqat nom
}

// SystemStats — qurilmaning real resurs ishlatilishi
type SystemStats struct {
	CPUPercent     float64    `json:"cpuPercent"`
	MemUsed        uint64     `json:"memUsed"`
	MemTotal       uint64     `json:"memTotal"`
	MemUsedPercent float64    `json:"memUsedPercent"`
	Disks          []DiskInfo `json:"disks"`
	GPUs           []GPUInfo  `json:"gpus"`
	Supported      bool       `json:"statsSupported"`
}

var (
	statsMu      sync.RWMutex
	currentStats = SystemStats{Disks: []DiskInfo{}, GPUs: []GPUInfo{}}
)

func getStats() SystemStats {
	statsMu.RLock()
	defer statsMu.RUnlock()
	return currentStats
}

func setStats(s SystemStats) {
	if s.Disks == nil {
		s.Disks = []DiskInfo{}
	}
	if s.GPUs == nil {
		s.GPUs = []GPUInfo{}
	}
	statsMu.Lock()
	currentStats = s
	statsMu.Unlock()
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round1(float64(used) / float64(total) * 100)
}

func round1(v float64) float64 {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return float64(int(v*10+0.5)) / 10
}
