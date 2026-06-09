//go:build linux

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// startStatsSampler — har 2 soniyada CPU/RAM/Disk holatini yangilab turadi
func startStatsSampler() {
	var prevIdle, prevTotal uint64
	for {
		idle, total := readCPUSample()
		var cpuPct float64
		if prevTotal != 0 && total > prevTotal {
			dTotal := total - prevTotal
			dIdle := idle - prevIdle
			if dTotal > 0 {
				cpuPct = round1((1 - float64(dIdle)/float64(dTotal)) * 100)
			}
		}
		prevIdle, prevTotal = idle, total

		memUsed, memTotal := readMem()
		diskUsed, diskTotal := readDisk("/")

		setStats(SystemStats{
			CPUPercent:      cpuPct,
			MemUsed:         memUsed,
			MemTotal:        memTotal,
			MemUsedPercent:  pct(memUsed, memTotal),
			DiskUsed:        diskUsed,
			DiskTotal:       diskTotal,
			DiskUsedPercent: pct(diskUsed, diskTotal),
			Supported:       true,
		})
		time.Sleep(2 * time.Second)
	}
}

// /proc/stat birinchi qatori: cpu user nice system idle iowait irq softirq steal ...
func readCPUSample() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	return idle, total
}

// /proc/meminfo: MemTotal, MemAvailable (kB)
func readMem() (used, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var memTotal, memAvail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64) // kB
		switch fields[0] {
		case "MemTotal:":
			memTotal = val * 1024
		case "MemAvailable:":
			memAvail = val * 1024
		}
	}
	if memTotal == 0 {
		return 0, 0
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	return memTotal - memAvail, memTotal
}

func readDisk(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free := st.Bavail * bsize
	if free > total {
		free = total
	}
	return total - free, total
}
