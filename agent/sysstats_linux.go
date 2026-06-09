//go:build linux

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// startStatsSampler — har 2 soniyada CPU/RAM/Disk/GPU holatini yangilab turadi
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

		setStats(SystemStats{
			CPUPercent:     cpuPct,
			MemUsed:        memUsed,
			MemTotal:       memTotal,
			MemUsedPercent: pct(memUsed, memTotal),
			Disks:          readDisks(),
			GPUs:           readGPUs(),
			Supported:      true,
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

func statfsUsage(path string) (used, total uint64) {
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

// realFS — haqiqiy disk fayl tizimlari (pseudo-fs lar chiqarib tashlanadi)
var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "f2fs": true,
	"zfs": true, "jfs": true, "reiserfs": true, "fuseblk": true,
}

// /proc/mounts dan barcha real disklarni o'qiydi
func readDisks() []DiskInfo {
	out := []DiskInfo{}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return out
	}
	defer f.Close()
	seen := map[string]bool{} // qurilma bo'yicha takrorlanmaslik
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, mount, fstype := fields[0], fields[1], fields[2]
		if !realFS[fstype] || !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		if seen[dev] {
			continue
		}
		seen[dev] = true
		mount = strings.ReplaceAll(mount, `\040`, " ")
		used, total := statfsUsage(mount)
		if total == 0 {
			continue
		}
		out = append(out, DiskInfo{Mount: mount, Used: used, Total: total, Percent: pct(used, total)})
	}
	return out
}

var (
	gpuOnce        sync.Once
	gpuCachedNames []string
)

// readGPUs — avval NVIDIA (util+xotira), bo'lmasa lspci orqali nom
func readGPUs() []GPUInfo {
	if gs := nvidiaGPUs(); len(gs) > 0 {
		return gs
	}
	gpuOnce.Do(func() { gpuCachedNames = lspciGPUs() })
	out := []GPUInfo{}
	for _, n := range gpuCachedNames {
		out = append(out, GPUInfo{Name: n})
	}
	return out
}

func nvidiaGPUs() []GPUInfo {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits")
	outBytes, err := cmd.Output()
	if err != nil {
		return nil
	}
	out := []GPUInfo{}
	for _, line := range strings.Split(strings.TrimSpace(string(outBytes)), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		util, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		memUsedMiB, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
		memTotalMiB, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
		memUsed := memUsedMiB * 1024 * 1024
		memTotal := memTotalMiB * 1024 * 1024
		out = append(out, GPUInfo{
			Name:        name,
			UtilPercent: round1(util),
			MemUsed:     memUsed,
			MemTotal:    memTotal,
			MemPercent:  pct(memUsed, memTotal),
			HasUtil:     true,
		})
	}
	return out
}

// lspci orqali GPU nomlari (util yo'q)
func lspciGPUs() []string {
	if _, err := exec.LookPath("lspci"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outBytes, err := exec.CommandContext(ctx, "lspci").Output()
	if err != nil {
		return nil
	}
	names := []string{}
	for _, line := range strings.Split(string(outBytes), "\n") {
		low := strings.ToLower(line)
		if !strings.Contains(low, "vga compatible controller") && !strings.Contains(low, "3d controller") {
			continue
		}
		if idx := strings.LastIndex(line, ": "); idx != -1 {
			names = append(names, strings.TrimSpace(line[idx+2:]))
		}
	}
	return names
}
