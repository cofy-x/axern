package diskusage

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// UsedRatioByAvailable returns used disk ratio in [0,1] using available blocks.
func UsedRatioByAvailable(path string) (float64, error) {
	usage, err := statfsUsage(path)
	if err != nil {
		return 0, err
	}
	return usage.usedRatioByAvailable(), nil
}

// UsedPercentByFree returns used disk percentage in [0,100] using free blocks.
func UsedPercentByFree(path string) (float64, error) {
	usage, err := statfsUsage(path)
	if err != nil {
		return 0, err
	}
	return usage.usedRatioByFree() * 100.0, nil
}

type usageStat struct {
	blocks uint64
	bfree  uint64
	bavail uint64
}

func statfsUsage(path string) (usageStat, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return usageStat{}, fmt.Errorf("failed to statfs %s: %w", path, err)
	}
	return usageStat{
		blocks: fs.Blocks,
		bfree:  fs.Bfree,
		bavail: fs.Bavail,
	}, nil
}

func (u usageStat) usedRatioByAvailable() float64 {
	if u.blocks == 0 {
		return 0
	}
	used := float64(u.blocks-u.bavail) / float64(u.blocks)
	return clamp01(used)
}

func (u usageStat) usedRatioByFree() float64 {
	if u.blocks == 0 {
		return 0
	}
	used := float64(u.blocks-u.bfree) / float64(u.blocks)
	return clamp01(used)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
