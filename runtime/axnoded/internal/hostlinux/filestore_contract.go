package hostlinux

import "math"

func StatfsBytes(blocks uint64, blockSize int64) int64 {
	if blocks == 0 || blockSize <= 0 {
		return 0
	}
	if blocks > uint64(math.MaxInt64)/uint64(blockSize) {
		return math.MaxInt64
	}
	return int64(blocks) * blockSize
}

func SaturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}

func RemainingCapacity(capacity int64, deductions ...int64) int64 {
	if capacity <= 0 {
		return 0
	}
	remaining := capacity
	for _, deduction := range deductions {
		if deduction <= 0 {
			continue
		}
		if deduction >= remaining {
			return 0
		}
		remaining -= deduction
	}
	return remaining
}
