package main

import "sync"

// SystemStats — qurilmaning real resurs ishlatilishi
type SystemStats struct {
	CPUPercent      float64 `json:"cpuPercent"`
	MemUsed         uint64  `json:"memUsed"`
	MemTotal        uint64  `json:"memTotal"`
	MemUsedPercent  float64 `json:"memUsedPercent"`
	DiskUsed        uint64  `json:"diskUsed"`
	DiskTotal       uint64  `json:"diskTotal"`
	DiskUsedPercent float64 `json:"diskUsedPercent"`
	Supported       bool    `json:"statsSupported"`
}

var (
	statsMu      sync.RWMutex
	currentStats SystemStats
)

func getStats() SystemStats {
	statsMu.RLock()
	defer statsMu.RUnlock()
	return currentStats
}

func setStats(s SystemStats) {
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
