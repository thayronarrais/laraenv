package sysmetrics

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// Sample is one snapshot of system resource usage.
type Sample struct {
	CPUPercent  float64 `json:"cpuPercent"`
	MemPercent  float64 `json:"memPercent"`
	MemUsedMB   uint64  `json:"memUsedMB"`
	MemTotalMB  uint64  `json:"memTotalMB"`
	GPUPercent  float64 `json:"gpuPercent"` // -1 if unavailable
	GPUVendor   string  `json:"gpuVendor"`  // best-effort label, may be ""
	GPUAvailable bool   `json:"gpuAvailable"`
	// Disk usage of the system drive (C:\). DiskPath is the path that was
	// actually probed so the UI can show "C: 78%" or similar.
	DiskPercent float64 `json:"diskPercent"`
	DiskUsedGB  float64 `json:"diskUsedGB"`
	DiskTotalGB float64 `json:"diskTotalGB"`
	DiskPath    string  `json:"diskPath"`
}

// Sampler owns a persistent PDH query (CPU + GPU counters). CPU reads
// "% Processor Utility" so the number matches Task Manager; GPU sums the
// "GPU Engine" utilization counters. Memory/disk stay on gopsutil.
type Sampler struct {
	mu       sync.Mutex
	pdh      *pdhSampler
	pdhTried bool

	// Last good PDH readings, reused when Sample is called again before the
	// counters have accumulated a meaningful interval (<300ms).
	lastAt  time.Time
	lastCPU float64
	lastGPU float64
	gpuOK   bool
	cpuOK   bool
}

func NewSampler() *Sampler { return &Sampler{} }

// Sample returns the current Sample.
func (s *Sampler) Sample(ctx context.Context) Sample {
	out := Sample{GPUPercent: -1}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		out.MemPercent = vm.UsedPercent
		out.MemUsedMB = vm.Used / 1024 / 1024
		out.MemTotalMB = vm.Total / 1024 / 1024
	}

	// Disk: probe the system drive. On Windows that's almost always C:\
	// (LaraEnv installs binaries under C:\laraenv anyway). gopsutil handles
	// the GetDiskFreeSpaceExW call internally and reports values that match
	// Explorer's "Properties" dialog.
	const diskPath = `C:\`
	out.DiskPath = diskPath
	if du, err := disk.UsageWithContext(ctx, diskPath); err == nil {
		out.DiskPercent = du.UsedPercent
		out.DiskUsedGB = float64(du.Used) / 1024 / 1024 / 1024
		out.DiskTotalGB = float64(du.Total) / 1024 / 1024 / 1024
	}

	cpuPct, cpuOK, gpuPct, gpuOK := s.collectPdh()
	if cpuOK {
		out.CPUPercent = cpuPct
	} else {
		// PDH unavailable (or CPU counter missing): fall back to gopsutil's
		// busy-time percent over a short window.
		if p, err := cpu.PercentWithContext(ctx, 250*time.Millisecond, false); err == nil && len(p) > 0 {
			out.CPUPercent = p[0]
		}
	}
	if gpuOK {
		out.GPUPercent = gpuPct
		out.GPUAvailable = true
		out.GPUVendor = gpuVendor(ctx)
	}
	return out
}

// collectPdh lazily initializes the PDH query and returns the latest CPU/GPU
// readings. Calls closer than 300ms apart reuse the previous values — rate
// counters need a real interval between collections to be meaningful.
func (s *Sampler) collectPdh() (cpu float64, cpuOK bool, gpu float64, gpuOK bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.pdhTried {
		s.pdhTried = true
		s.pdh = newPdhSampler()
		if s.pdh != nil {
			// Prime interval: the query was collected once at init; give the
			// counters a short window so the first read isn't invalid.
			time.Sleep(300 * time.Millisecond)
		}
	}
	if s.pdh == nil {
		return 0, false, 0, false
	}
	if time.Since(s.lastAt) < 300*time.Millisecond {
		return s.lastCPU, s.cpuOK, s.lastGPU, s.gpuOK
	}
	cpu, cpuOK, gpu, gpuOK = s.pdh.collect()
	s.lastAt = time.Now()
	// Keep the previous good reading when one counter momentarily reports
	// invalid data instead of flickering the UI to 0 / N/A.
	if cpuOK {
		s.lastCPU, s.cpuOK = cpu, true
	}
	if gpuOK {
		s.lastGPU, s.gpuOK = gpu, true
	}
	return s.lastCPU, s.cpuOK, s.lastGPU, s.gpuOK
}

var (
	vendorOnce sync.Once
	vendorVal  string
)

func gpuVendor(ctx context.Context) string {
	vendorOnce.Do(func() {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, "powershell", "-NoProfile", "-Command",
			`(Get-CimInstance Win32_VideoController | Select-Object -First 1).Name`)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.Output()
		if err != nil {
			return
		}
		vendorVal = strings.TrimSpace(string(out))
	})
	return vendorVal
}
