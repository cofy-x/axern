package natbench

func DiffStartupSummary(before, after *StartupSnapshot) *StartupSummary {
	if after == nil {
		return nil
	}

	summary := &StartupSummary{
		Runtime:    after.Runtime,
		RootfsType: after.RootfsType,
		Classes:    map[string]StartupClassSummary{},
		Phases:     map[string]StartupPhaseSummary{},
	}
	for startClass, afterClass := range after.Classes {
		beforeClass := StartupClassSnapshot{}
		if before != nil && before.Classes != nil {
			beforeClass = before.Classes[startClass]
		}

		okCount := subtractStartupCounter(afterClass.OKCount, beforeClass.OKCount)
		errorCount := subtractStartupCounter(afterClass.ErrorCount, beforeClass.ErrorCount)
		okDurationCount := subtractStartupCounter(afterClass.OKDurationCount, beforeClass.OKDurationCount)
		okDurationSum := subtractStartupFloat(afterClass.OKDurationSumSeconds, beforeClass.OKDurationSumSeconds)
		histogram := subtractHistogram(afterClass.Histogram, beforeClass.Histogram)
		if okCount == 0 && errorCount == 0 && okDurationCount == 0 && histogram == nil {
			continue
		}

		classSummary := StartupClassSummary{
			OKCount:    okCount,
			ErrorCount: errorCount,
			Histogram:  histogram,
		}
		if histogram != nil && histogram.Count > 0 {
			classSummary.AverageDurationSeconds = histogram.SumSeconds / float64(histogram.Count)
			classSummary.Quantiles = histogramQuantiles(histogram)
		} else if okDurationCount > 0 {
			classSummary.AverageDurationSeconds = okDurationSum / float64(okDurationCount)
		}
		summary.Classes[startClass] = classSummary
	}
	if len(summary.Classes) == 0 {
		summary.Classes = nil
	}

	for phase, afterPhase := range after.Phases {
		phaseSummary := StartupPhaseSummary{Classes: map[string]StartupPhaseClassSummary{}}
		var beforePhase StartupPhaseSnapshot
		if before != nil && before.Phases != nil {
			beforePhase = before.Phases[phase]
		}
		for startClass, afterClass := range afterPhase.Classes {
			beforeClass := StartupPhaseClassSnapshot{}
			if beforePhase.Classes != nil {
				beforeClass = beforePhase.Classes[startClass]
			}

			count := subtractStartupCounter(afterClass.Count, beforeClass.Count)
			errorCount := subtractStartupCounter(afterClass.ErrorCount, beforeClass.ErrorCount)
			sumSeconds := subtractStartupFloat(afterClass.SumSeconds, beforeClass.SumSeconds)
			histogram := subtractHistogram(afterClass.Histogram, beforeClass.Histogram)
			if count == 0 && histogram == nil {
				continue
			}

			classSummary := StartupPhaseClassSummary{
				Count:      count,
				ErrorCount: errorCount,
				Histogram:  histogram,
			}
			if histogram != nil && histogram.Count > 0 {
				classSummary.AverageDurationSeconds = histogram.SumSeconds / float64(histogram.Count)
				classSummary.Quantiles = histogramQuantiles(histogram)
			} else if count > 0 {
				classSummary.AverageDurationSeconds = sumSeconds / float64(count)
			}
			phaseSummary.Classes[startClass] = classSummary
		}
		if len(phaseSummary.Classes) > 0 {
			summary.Phases[phase] = phaseSummary
		}
	}
	if len(summary.Phases) == 0 {
		summary.Phases = nil
	}

	if after.Bundle != nil {
		beforeBundle := BundleTemplateSnapshot{}
		if before != nil && before.Bundle != nil {
			beforeBundle = *before.Bundle
		}
		materializeHistogram := subtractHistogram(after.Bundle.MaterializeHistogram, beforeBundle.MaterializeHistogram)
		bundleSummary := &BundleTemplateSummary{
			HitCount:             subtractStartupCounter(after.Bundle.HitCount, beforeBundle.HitCount),
			MissCount:            subtractStartupCounter(after.Bundle.MissCount, beforeBundle.MissCount),
			ErrorCount:           subtractStartupCounter(after.Bundle.ErrorCount, beforeBundle.ErrorCount),
			MaterializeHistogram: materializeHistogram,
		}
		materializeCount := subtractStartupCounter(after.Bundle.MaterializeCount, beforeBundle.MaterializeCount)
		materializeSum := subtractStartupFloat(after.Bundle.MaterializeSumSeconds, beforeBundle.MaterializeSumSeconds)
		if materializeHistogram != nil && materializeHistogram.Count > 0 {
			bundleSummary.AverageMaterializeDurationSec = materializeHistogram.SumSeconds / float64(materializeHistogram.Count)
			bundleSummary.MaterializeQuantiles = histogramQuantiles(materializeHistogram)
		} else if materializeCount > 0 {
			bundleSummary.AverageMaterializeDurationSec = materializeSum / float64(materializeCount)
		}
		if bundleSummary.HitCount > 0 || bundleSummary.MissCount > 0 || bundleSummary.ErrorCount > 0 || materializeCount > 0 || materializeHistogram != nil {
			summary.Bundle = bundleSummary
		}
	}

	if after.Envelope != nil {
		beforeEnvelope := ExecutionEnvelopeSnapshot{}
		if before != nil && before.Envelope != nil {
			beforeEnvelope = *before.Envelope
		}
		prepareHistogram := subtractHistogram(after.Envelope.PrepareHistogram, beforeEnvelope.PrepareHistogram)
		activateHistogram := subtractHistogram(after.Envelope.ActivateHistogram, beforeEnvelope.ActivateHistogram)
		envelopeSummary := &ExecutionEnvelopeSummary{
			PreparedCount:     subtractStartupCounter(after.Envelope.PreparedCount, beforeEnvelope.PreparedCount),
			HitCount:          subtractStartupCounter(after.Envelope.HitCount, beforeEnvelope.HitCount),
			MissCount:         subtractStartupCounter(after.Envelope.MissCount, beforeEnvelope.MissCount),
			ErrorCount:        subtractStartupCounter(after.Envelope.ErrorCount, beforeEnvelope.ErrorCount),
			FallbackCount:     subtractStartupCounter(after.Envelope.FallbackCount, beforeEnvelope.FallbackCount),
			PrepareHistogram:  prepareHistogram,
			ActivateHistogram: activateHistogram,
		}
		if prepareHistogram != nil && prepareHistogram.Count > 0 {
			envelopeSummary.AveragePrepareDurationSec = prepareHistogram.SumSeconds / float64(prepareHistogram.Count)
			envelopeSummary.PrepareQuantiles = histogramQuantiles(prepareHistogram)
		}
		if activateHistogram != nil && activateHistogram.Count > 0 {
			envelopeSummary.AverageActivateDurationSec = activateHistogram.SumSeconds / float64(activateHistogram.Count)
			envelopeSummary.ActivateQuantiles = histogramQuantiles(activateHistogram)
		}
		if envelopeSummary.PreparedCount > 0 || envelopeSummary.HitCount > 0 || envelopeSummary.MissCount > 0 || envelopeSummary.ErrorCount > 0 || envelopeSummary.FallbackCount > 0 || prepareHistogram != nil || activateHistogram != nil {
			summary.Envelope = envelopeSummary
		}
	}

	if after.WaitGrace != nil {
		beforeWaitGrace := RuntimeWaitGraceSnapshot{}
		if before != nil && before.WaitGrace != nil {
			beforeWaitGrace = *before.WaitGrace
		}
		waitGraceSummary := &RuntimeWaitGraceSummary{
			RecoveredCount:   subtractStartupCounter(after.WaitGrace.RecoveredCount, beforeWaitGrace.RecoveredCount),
			UnavailableCount: subtractStartupCounter(after.WaitGrace.UnavailableCount, beforeWaitGrace.UnavailableCount),
		}
		if waitGraceSummary.RecoveredCount > 0 || waitGraceSummary.UnavailableCount > 0 {
			summary.WaitGrace = waitGraceSummary
		}
	}

	finalizeStartupSummary(summary)
	if len(summary.Classes) == 0 && len(summary.Phases) == 0 && summary.Bundle == nil && summary.Envelope == nil && summary.WaitGrace == nil {
		return nil
	}
	return summary
}

func finalizeStartupSummary(summary *StartupSummary) {
	if summary == nil {
		return
	}
	if summary.Bundle != nil {
		total := summary.Bundle.HitCount + summary.Bundle.MissCount
		if total > 0 {
			summary.Bundle.HitRate = float64(summary.Bundle.HitCount) / float64(total)
		}
	}
	if summary.Phases != nil {
		summary.PhaseBreakdown = buildPhaseBreakdown(summary.Phases)
		if len(summary.PhaseBreakdown) == 0 {
			summary.PhaseBreakdown = nil
		}
	}
	if summary.PhaseBreakdown != nil {
		summary.DominantPhaseP95 = buildDominantPhaseMap(summary.PhaseBreakdown, func(entry StartupPhaseBreakdown) float64 {
			return entry.P95Seconds
		})
		summary.DominantPhaseP99 = buildDominantPhaseMap(summary.PhaseBreakdown, func(entry StartupPhaseBreakdown) float64 {
			return entry.P99Seconds
		})
		if len(summary.DominantPhaseP95) == 0 {
			summary.DominantPhaseP95 = nil
		}
		if len(summary.DominantPhaseP99) == 0 {
			summary.DominantPhaseP99 = nil
		}
	}
}

func buildPhaseBreakdown(phases map[string]StartupPhaseSummary) map[string]map[string]StartupPhaseBreakdown {
	if len(phases) == 0 {
		return nil
	}
	breakdown := make(map[string]map[string]StartupPhaseBreakdown)
	for phase, phaseSummary := range phases {
		for startClass, classSummary := range phaseSummary.Classes {
			if breakdown[startClass] == nil {
				breakdown[startClass] = map[string]StartupPhaseBreakdown{}
			}
			entry := StartupPhaseBreakdown{
				Count:      classSummary.Count,
				ErrorCount: classSummary.ErrorCount,
			}
			if classSummary.Quantiles != nil {
				entry.P50Seconds = classSummary.Quantiles.P50Seconds
				entry.P95Seconds = classSummary.Quantiles.P95Seconds
				entry.P99Seconds = classSummary.Quantiles.P99Seconds
			}
			breakdown[startClass][phase] = entry
		}
	}
	return breakdown
}

func buildDominantPhaseMap(
	breakdown map[string]map[string]StartupPhaseBreakdown,
	value func(StartupPhaseBreakdown) float64,
) map[string]string {
	if len(breakdown) == 0 {
		return nil
	}
	dominant := make(map[string]string, len(breakdown))
	for startClass, phaseEntries := range breakdown {
		bestPhase := dominantPhase(phaseEntries, value)
		if bestPhase != "" {
			dominant[startClass] = bestPhase
		}
	}
	return dominant
}

func dominantPhase(phaseEntries map[string]StartupPhaseBreakdown, value func(StartupPhaseBreakdown) float64) string {
	bestPhase := ""
	bestValue := 0.0
	bestCount := uint64(0)
	for phase, entry := range phaseEntries {
		currentValue := value(entry)
		switch {
		case currentValue > bestValue:
			bestPhase, bestValue, bestCount = phase, currentValue, entry.Count
		case currentValue == bestValue && entry.Count > bestCount:
			bestPhase, bestValue, bestCount = phase, currentValue, entry.Count
		case currentValue == bestValue && entry.Count == bestCount && comparePhaseOrder(phase, bestPhase):
			bestPhase = phase
		}
	}
	return bestPhase
}

var startupPhaseOrder = map[string]int{
	"langruntime_lookup":     0,
	"rootfs_prepare":         1,
	"resource_allocate":      2,
	"runtime_bundle_prepare": 3,
	"runtime_launch":         4,
	"network_activate":       5,
}

func comparePhaseOrder(left, right string) bool {
	leftOrder, leftOK := startupPhaseOrder[left]
	rightOrder, rightOK := startupPhaseOrder[right]
	switch {
	case leftOK && rightOK:
		return leftOrder < rightOrder
	case leftOK:
		return true
	case rightOK:
		return false
	default:
		return left < right
	}
}
