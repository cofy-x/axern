package natbench

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

func (bucket HistogramBucketSnapshot) MarshalJSON() ([]byte, error) {
	type histogramBucketJSON struct {
		UpperBound      any    `json:"upperBound"`
		CumulativeCount uint64 `json:"cumulativeCount"`
	}

	upperBound := any(bucket.UpperBound)
	if math.IsInf(bucket.UpperBound, 1) {
		upperBound = "inf"
	}

	return json.Marshal(histogramBucketJSON{
		UpperBound:      upperBound,
		CumulativeCount: bucket.CumulativeCount,
	})
}

func (bucket *HistogramBucketSnapshot) UnmarshalJSON(data []byte) error {
	type histogramBucketJSON struct {
		UpperBound      json.RawMessage `json:"upperBound"`
		CumulativeCount uint64          `json:"cumulativeCount"`
	}

	var decoded histogramBucketJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var upperBound float64
	if len(decoded.UpperBound) == 0 || string(decoded.UpperBound) == "null" {
		upperBound = math.Inf(1)
	} else {
		if err := json.Unmarshal(decoded.UpperBound, &upperBound); err != nil {
			var upperBoundString string
			if stringErr := json.Unmarshal(decoded.UpperBound, &upperBoundString); stringErr != nil {
				return err
			}
			switch upperBoundString {
			case "inf", "+inf", "Inf", "+Inf":
				upperBound = math.Inf(1)
			default:
				return fmt.Errorf("unsupported histogram upperBound %q", upperBoundString)
			}
		}
	}

	bucket.UpperBound = upperBound
	bucket.CumulativeCount = decoded.CumulativeCount
	return nil
}

func subtractStartupCounter(after, before uint64) uint64 {
	if after >= before {
		return after - before
	}
	return after
}

func subtractStartupFloat(after, before float64) float64 {
	if after >= before {
		return after - before
	}
	return after
}

func histogramFromSamples(samples []float64) *HistogramSnapshot {
	if len(samples) == 0 {
		return nil
	}
	sortedSamples := append([]float64(nil), samples...)
	sort.Float64s(sortedSamples)
	out := &HistogramSnapshot{
		Count:   uint64(len(sortedSamples)),
		Samples: sortedSamples,
	}
	bucketCounts := make(map[float64]uint64, len(sortedSamples)+1)
	for index, sample := range sortedSamples {
		out.SumSeconds += sample
		bucketCounts[sample] = uint64(index + 1)
	}
	bucketCounts[math.Inf(1)] = uint64(len(sortedSamples))
	out.Buckets = histogramBucketsFromMap(bucketCounts)
	return out
}

func histogramFromCountSum(count uint64, sum float64) *HistogramSnapshot {
	if count == 0 {
		return nil
	}
	return &HistogramSnapshot{
		Count:      count,
		SumSeconds: sum,
		Buckets: []HistogramBucketSnapshot{
			{UpperBound: math.Inf(1), CumulativeCount: count},
		},
	}
}

func histogramFromPoint(count uint64, sum float64, sampleStart uint64, samples []float64) *HistogramSnapshot {
	if len(samples) > 0 {
		histogram := histogramFromSamples(samples)
		if histogram != nil {
			histogram.Count = count
			histogram.SumSeconds = sum
			histogram.SampleStart = sampleStart
			histogram.SamplesCumulative = true
		}
		return histogram
	}
	return histogramFromCountSum(count, sum)
}

func histogramFromSamplesAndBuckets(samples []float64, buckets []HistogramBucketSnapshot) *HistogramSnapshot {
	out := histogramFromSamples(samples)
	if out == nil {
		return nil
	}
	if len(buckets) > 0 {
		out.Buckets = append([]HistogramBucketSnapshot(nil), buckets...)
		sortHistogramBuckets(out)
	}
	return out
}

func bucketsFromSamples(samples []float64) []HistogramBucketSnapshot {
	histogram := histogramFromSamples(samples)
	if histogram == nil {
		return nil
	}
	return histogram.Buckets
}

func cloneBuckets(buckets []HistogramBucketSnapshot) []HistogramBucketSnapshot {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]HistogramBucketSnapshot, len(buckets))
	copy(out, buckets)
	return out
}

func cloneSamples(samples []float64) []float64 {
	if len(samples) == 0 {
		return nil
	}
	out := make([]float64, len(samples))
	copy(out, samples)
	return out
}

func appendBucketsFromSamples(histogram *HistogramSnapshot) {
	if histogram == nil || len(histogram.Buckets) > 0 || len(histogram.Samples) == 0 {
		return
	}
	histogram.Buckets = bucketsFromSamples(histogram.Samples)
}

func rebaseHistogramSamples(after, before *HistogramSnapshot) []float64 {
	if after == nil || len(after.Samples) == 0 {
		return nil
	}
	if before == nil || len(before.Samples) == 0 {
		return cloneSamples(after.Samples)
	}
	if after.SamplesCumulative && before.SamplesCumulative {
		if after.Count < before.Count {
			return cloneSamples(after.Samples)
		}
		start := before.Count
		if start < after.SampleStart {
			start = after.SampleStart
		}
		end := after.SampleStart + uint64(len(after.Samples))
		if start >= end {
			return nil
		}
		return cloneSamples(after.Samples[start-after.SampleStart:])
	}
	if len(before.Samples) <= len(after.Samples) {
		match := true
		for index := range before.Samples {
			if before.Samples[index] != after.Samples[index] {
				match = false
				break
			}
		}
		if match {
			return cloneSamples(after.Samples[len(before.Samples):])
		}
	}
	beforeCounts := make(map[float64]int, len(before.Samples))
	for _, sample := range before.Samples {
		beforeCounts[sample]++
	}
	out := make([]float64, 0, len(after.Samples))
	for _, sample := range after.Samples {
		if beforeCounts[sample] > 0 {
			beforeCounts[sample]--
			continue
		}
		out = append(out, sample)
	}
	return out
}

func histogramBucketsFromSamples(samples []float64) []HistogramBucketSnapshot {
	if len(samples) == 0 {
		return nil
	}
	sortedSamples := append([]float64(nil), samples...)
	sort.Float64s(sortedSamples)
	buckets := make([]HistogramBucketSnapshot, 0, len(sortedSamples)+1)
	for index, sample := range sortedSamples {
		buckets = append(buckets, HistogramBucketSnapshot{
			UpperBound:      sample,
			CumulativeCount: uint64(index + 1),
		})
	}
	buckets = append(buckets, HistogramBucketSnapshot{
		UpperBound:      math.Inf(1),
		CumulativeCount: uint64(len(sortedSamples)),
	})
	return buckets
}

func cloneHistogram(histogram *HistogramSnapshot) *HistogramSnapshot {
	if histogram == nil {
		return nil
	}
	cloned := &HistogramSnapshot{
		Count:             histogram.Count,
		SumSeconds:        histogram.SumSeconds,
		Buckets:           cloneBuckets(histogram.Buckets),
		SampleStart:       histogram.SampleStart,
		SamplesCumulative: histogram.SamplesCumulative,
		Samples:           cloneSamples(histogram.Samples),
	}
	return cloned
}

func mergeHistogram(base, add *HistogramSnapshot) *HistogramSnapshot {
	if add == nil {
		return cloneHistogram(base)
	}
	if base == nil {
		return cloneHistogram(add)
	}
	merged := &HistogramSnapshot{
		Count:             base.Count + add.Count,
		SumSeconds:        base.SumSeconds + add.SumSeconds,
		SamplesCumulative: false,
		Samples:           append(cloneSamples(base.Samples), add.Samples...),
	}
	if len(merged.Samples) > 0 {
		sort.Float64s(merged.Samples)
		merged.Buckets = histogramBucketsFromSamples(merged.Samples)
		return merged
	}
	bucketCounts := make(map[float64]uint64, len(base.Buckets)+len(add.Buckets))
	for _, bucket := range base.Buckets {
		bucketCounts[bucket.UpperBound] += bucket.CumulativeCount
	}
	for _, bucket := range add.Buckets {
		bucketCounts[bucket.UpperBound] += bucket.CumulativeCount
	}
	merged.Buckets = histogramBucketsFromMap(bucketCounts)
	return merged
}

func subtractHistogram(after, before *HistogramSnapshot) *HistogramSnapshot {
	if after == nil {
		return nil
	}
	if before == nil {
		return cloneHistogram(after)
	}
	out := &HistogramSnapshot{
		Count:      subtractStartupCounter(after.Count, before.Count),
		SumSeconds: subtractStartupFloat(after.SumSeconds, before.SumSeconds),
	}
	out.Samples = rebaseHistogramSamples(after, before)
	if len(out.Samples) > 0 {
		sort.Float64s(out.Samples)
		out.Buckets = histogramBucketsFromSamples(out.Samples)
		return out
	}
	afterBuckets := make(map[float64]uint64, len(after.Buckets))
	for _, bucket := range after.Buckets {
		afterBuckets[bucket.UpperBound] = bucket.CumulativeCount
	}
	for _, bucket := range before.Buckets {
		current := afterBuckets[bucket.UpperBound]
		if current >= bucket.CumulativeCount {
			afterBuckets[bucket.UpperBound] = current - bucket.CumulativeCount
		}
	}
	out.Buckets = histogramBucketsFromMap(afterBuckets)
	if out.Count == 0 && out.SumSeconds == 0 && len(out.Buckets) == 0 {
		return nil
	}
	return out
}

func histogramBucketsFromMap(bucketCounts map[float64]uint64) []HistogramBucketSnapshot {
	if len(bucketCounts) == 0 {
		return nil
	}
	bounds := make([]float64, 0, len(bucketCounts))
	for upperBound := range bucketCounts {
		bounds = append(bounds, upperBound)
	}
	sort.Float64s(bounds)
	buckets := make([]HistogramBucketSnapshot, 0, len(bounds))
	for _, upperBound := range bounds {
		buckets = append(buckets, HistogramBucketSnapshot{
			UpperBound:      upperBound,
			CumulativeCount: bucketCounts[upperBound],
		})
	}
	return buckets
}

func sortHistogramBuckets(histogram *HistogramSnapshot) {
	if histogram == nil || len(histogram.Buckets) == 0 {
		return
	}
	sort.Slice(histogram.Buckets, func(i, j int) bool {
		return histogram.Buckets[i].UpperBound < histogram.Buckets[j].UpperBound
	})
}

func histogramQuantiles(histogram *HistogramSnapshot) *DurationQuantiles {
	if histogram == nil || histogram.Count == 0 {
		return nil
	}
	return &DurationQuantiles{
		P50Seconds: histogramQuantile(histogram, 0.50),
		P95Seconds: histogramQuantile(histogram, 0.95),
		P99Seconds: histogramQuantile(histogram, 0.99),
	}
}

func histogramQuantile(histogram *HistogramSnapshot, quantile float64) float64 {
	if histogram == nil || histogram.Count == 0 {
		return 0
	}
	if len(histogram.Samples) > 0 {
		samples := cloneSamples(histogram.Samples)
		sort.Float64s(samples)
		index := int(math.Ceil(quantile*float64(len(samples)))) - 1
		if index < 0 {
			index = 0
		}
		if index >= len(samples) {
			index = len(samples) - 1
		}
		return samples[index]
	}
	appendBucketsFromSamples(histogram)
	sortHistogramBuckets(histogram)
	target := quantile * float64(histogram.Count)
	prevCount := uint64(0)
	prevBound := 0.0
	lastFiniteBound := 0.0
	for _, bucket := range histogram.Buckets {
		upperBound := bucket.UpperBound
		if !math.IsInf(upperBound, 1) {
			lastFiniteBound = upperBound
		}
		if float64(bucket.CumulativeCount) >= target {
			if math.IsInf(upperBound, 1) {
				return lastFiniteBound
			}
			bucketCount := bucket.CumulativeCount - prevCount
			if bucketCount == 0 {
				return upperBound
			}
			position := target - float64(prevCount)
			if position < 0 {
				position = 0
			}
			fraction := position / float64(bucketCount)
			if fraction < 0 {
				fraction = 0
			}
			if fraction > 1 {
				fraction = 1
			}
			return prevBound + (upperBound-prevBound)*fraction
		}
		prevCount = bucket.CumulativeCount
		if !math.IsInf(upperBound, 1) {
			prevBound = upperBound
		}
	}
	return lastFiniteBound
}
