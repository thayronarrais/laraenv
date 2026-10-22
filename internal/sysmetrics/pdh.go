package sysmetrics

import (
	"syscall"
	"unsafe"
)

// Native PDH (Performance Data Helper) sampler. Replaces the old
// PowerShell-based GPU probe, which spawned powershell.exe on every tick and
// took ~3s per run — far beyond the 1.5s timeout, so GPU permanently reported
// "N/A". PDH keeps one persistent query open and each collection is a cheap
// in-process call. PdhAddEnglishCounter* is used so counter paths work on
// localized Windows installs (pt-BR etc.) where "\GPU Engine" is translated.
var (
	pdhDLL              = syscall.NewLazyDLL("pdh.dll")
	procPdhOpenQuery    = pdhDLL.NewProc("PdhOpenQueryW")
	procPdhAddEnCounter = pdhDLL.NewProc("PdhAddEnglishCounterW")
	procPdhCollect      = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhGetValue     = pdhDLL.NewProc("PdhGetFormattedCounterValue")
	procPdhGetArray     = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
)

const (
	pdhFmtDouble = 0x00000200
	pdhMoreData  = 0x800007D2
)

// pdhFmtCounterValueDouble mirrors PDH_FMT_COUNTERVALUE with the double union
// arm: 4-byte CStatus, 4 bytes padding, 8-byte double.
type pdhFmtCounterValueDouble struct {
	CStatus uint32
	_       uint32
	Value   float64
}

// pdhFmtCounterValueItemDouble mirrors PDH_FMT_COUNTERVALUE_ITEM_W.
type pdhFmtCounterValueItemDouble struct {
	Name  *uint16
	Value pdhFmtCounterValueDouble
}

type pdhSampler struct {
	query      uintptr
	cpuCounter uintptr // 0 when unavailable
	gpuCounter uintptr // 0 when unavailable
}

// newPdhSampler opens the query and registers the CPU + GPU counters, then
// primes it with one collection (rate counters need two collections before
// they yield a value). Returns nil when PDH itself is unusable.
func newPdhSampler() *pdhSampler {
	var q uintptr
	if ret, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); ret != 0 {
		return nil
	}
	p := &pdhSampler{query: q}

	// "% Processor Utility" is what Task Manager shows on Win8+ (accounts for
	// turbo/parked cores). Fall back to the classic "% Processor Time" when the
	// counter doesn't exist (older builds / stripped-down SKUs).
	p.cpuCounter = p.addCounter(`\Processor Information(_Total)\% Processor Utility`)
	if p.cpuCounter == 0 {
		p.cpuCounter = p.addCounter(`\Processor(_Total)\% Processor Time`)
	}
	p.gpuCounter = p.addCounter(`\GPU Engine(*)\Utilization Percentage`)

	if p.cpuCounter == 0 && p.gpuCounter == 0 {
		return nil
	}
	procPdhCollect.Call(q)
	return p
}

func (p *pdhSampler) addCounter(path string) uintptr {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var c uintptr
	if ret, _, _ := procPdhAddEnCounter.Call(p.query, uintptr(unsafe.Pointer(ptr)), 0, uintptr(unsafe.Pointer(&c))); ret != 0 {
		return 0
	}
	return c
}

// collect performs one PdhCollectQueryData and reads both counters. ok flags
// are false when the respective counter is missing or returned invalid data
// (e.g. the very first collection after priming was too recent).
func (p *pdhSampler) collect() (cpu float64, cpuOK bool, gpu float64, gpuOK bool) {
	if ret, _, _ := procPdhCollect.Call(p.query); ret != 0 {
		return 0, false, 0, false
	}
	if p.cpuCounter != 0 {
		var v pdhFmtCounterValueDouble
		ret, _, _ := procPdhGetValue.Call(p.cpuCounter, pdhFmtDouble, 0, uintptr(unsafe.Pointer(&v)))
		if ret == 0 && v.CStatus <= 1 { // PDH_CSTATUS_VALID_DATA or NEW_DATA
			cpu = clampPct(v.Value)
			cpuOK = true
		}
	}
	if p.gpuCounter != 0 {
		if sum, ok := p.sumArray(p.gpuCounter); ok {
			gpu = clampPct(sum)
			gpuOK = true
		}
	}
	return cpu, cpuOK, gpu, gpuOK
}

// sumArray reads a wildcard counter ("\GPU Engine(*)\...") and sums every
// instance's value — the same aggregation Task Manager's overall GPU% uses
// (well approximated; per-engine utilization summed and clamped).
func (p *pdhSampler) sumArray(counter uintptr) (float64, bool) {
	var bufSize, itemCount uint32
	ret, _, _ := procPdhGetArray.Call(counter, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), 0)
	if uint32(ret) != uint32(pdhMoreData) || bufSize == 0 {
		return 0, false
	}
	buf := make([]byte, bufSize)
	ret, _, _ = procPdhGetArray.Call(counter, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)),
		uintptr(unsafe.Pointer(&buf[0])))
	if ret != 0 || itemCount == 0 {
		return 0, false
	}
	itemSize := unsafe.Sizeof(pdhFmtCounterValueItemDouble{})
	if uintptr(bufSize) < uintptr(itemCount)*itemSize {
		return 0, false
	}
	var sum float64
	for i := uintptr(0); i < uintptr(itemCount); i++ {
		item := (*pdhFmtCounterValueItemDouble)(unsafe.Pointer(&buf[i*itemSize]))
		if item.Value.CStatus <= 1 {
			sum += item.Value.Value
		}
	}
	return sum, true
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
