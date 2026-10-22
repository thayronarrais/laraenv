//go:build !windows

package agents

import (
	"os"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/sys/unix"
)

func activityProcessCreation(pid int) (time.Time, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return time.Time{}, err
	}
	created, err := p.CreateTime()
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(created).UTC(), nil
}
func activityProcessAlive(id ActivityOwnerIdentity) bool {
	p, err := process.NewProcess(int32(id.PID))
	if err != nil {
		return false
	}
	created, err := p.CreateTime()
	if err != nil || !time.UnixMilli(created).Equal(id.CreatedAt) {
		return false
	}
	states, err := p.Status()
	if err != nil || len(states) == 0 {
		return false
	}
	for _, state := range states {
		// gopsutil maps Linux X/x (dead) to UnknownState. Reject unknown
		// and zombie states instead of treating an extant /proc PID as live.
		switch state {
		case process.Running, process.Blocked, process.Idle, process.Lock,
			process.Sleep, process.Stop, process.Wait, process.Daemon,
			process.Detached, process.System, process.Orphan:
		default:
			return false
		}
	}
	return true
}
func lockActivityFile(f *os.File) error          { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }
func unlockActivityFile(f *os.File)              { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
func protectActivityDirectory(path string) error { return os.Chmod(path, 0700) }
