//go:build linux

// Linux memory provider: /proc/meminfo parsing. MemAvailable (kernel
// >= 3.14) is the canonical "available for new allocations" number;
// it accounts for reclaimable cache, unlike MemFree alone.

package build

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type linuxMemProvider struct{}

func init() {
	defaultMemProvider = linuxMemProvider{}
}

func (linuxMemProvider) Read() (MemStat, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemStat{}, fmt.Errorf("open /proc/meminfo: %w", err)
	}
	defer f.Close()

	var totalKB, availKB uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = v
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB = v
		}
		if totalKB != 0 && availKB != 0 {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return MemStat{}, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	if totalKB == 0 {
		return MemStat{}, fmt.Errorf("/proc/meminfo: no MemTotal")
	}
	return MemStat{
		TotalMB:     totalKB / 1024,
		AvailableMB: availKB / 1024,
	}, nil
}
