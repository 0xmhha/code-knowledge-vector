//go:build darwin

// Darwin memory provider: sysctl for total, vm_stat for available.
//
// Why not mach_host_self() + host_statistics64()? That's the precise
// way to read VM stats, but it requires cgo. vm_stat exec adds ~10 ms
// per call, well under the 5 s poll interval. The trade-off favors no
// cgo dependency.

package build

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"golang.org/x/sys/unix"
)

type darwinMemProvider struct {
	pageSize uint64
}

func init() {
	ps, err := unix.SysctlUint32("hw.pagesize")
	if err != nil || ps == 0 {
		// Apple Silicon is 16384, older Intel was 4096.
		// 4096 underestimates available memory on M-series, which is
		// safer (more conservative pre-check). Used only as fallback.
		ps = 4096
	}
	defaultMemProvider = &darwinMemProvider{pageSize: uint64(ps)}
}

// vm_stat lines we treat as "available for new allocations":
//   - free        : truly unused
//   - inactive    : will be reclaimed before paging
//   - speculative : prefetch buffer, cheap to drop
//   - purgeable   : explicitly purgeable allocations
// "wired" and "active" are NOT counted — touching them costs disk I/O.
var vmStatPagesRe = regexp.MustCompile(`(?m)^Pages (free|inactive|speculative|purgeable):\s+(\d+)\.`)

func (p *darwinMemProvider) Read() (MemStat, error) {
	totalBytes, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return MemStat{}, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return MemStat{}, fmt.Errorf("vm_stat exec: %w", err)
	}
	var pages uint64
	for _, m := range vmStatPagesRe.FindAllStringSubmatch(string(out), -1) {
		n, perr := strconv.ParseUint(m[2], 10, 64)
		if perr != nil {
			continue
		}
		pages += n
	}
	if pages == 0 {
		return MemStat{}, fmt.Errorf("vm_stat: no recognized page counts in output")
	}
	availBytes := pages * p.pageSize
	return MemStat{
		TotalMB:     totalBytes / (1024 * 1024),
		AvailableMB: availBytes / (1024 * 1024),
	}, nil
}
