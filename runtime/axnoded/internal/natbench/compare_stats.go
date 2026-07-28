package natbench

import "sort"

func percentDelta(base, current float64) *float64 {
	if base == 0 {
		return nil
	}
	value := ((current - base) / base) * 100
	return &value
}

func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

func medianUint64(values []uint64) uint64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]uint64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	return sorted[len(sorted)/2]
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	return sorted[len(sorted)/2]
}

func mostCommonString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	counts := make(map[string]int, len(values))
	best := values[0]
	for _, value := range values {
		counts[value]++
		if counts[value] > counts[best] {
			best = value
		}
	}
	return best
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func allTrue(values []bool) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	for _, value := range append(append([]string(nil), base...), values...) {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
