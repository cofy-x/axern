package service

import (
	"fmt"
	"time"

	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
)

const (
	endpointEWMANewWeight       = 0.2
	defaultEndpointErrorPenalty = time.Second
)

type endpointStats struct {
	outstanding int
	selections  uint64
	ewma        time.Duration
	observed    bool
}

func pickEndpointLocked(item *cacheItem, now time.Time) (*gatewayv1.ServiceRouteEndpoint, cacheDelta) {
	delta := cacheDelta{}
	for endpoint, expiresAt := range item.quarantined {
		if !now.Before(expiresAt) {
			delete(item.quarantined, endpoint)
			delta.quarantined--
		}
	}

	endpoints := item.route.GetEndpoints()
	start := item.next
	var selected *gatewayv1.ServiceRouteEndpoint
	selectedIndex := start % len(endpoints)
	var selectedStats *endpointStats
	for offset := 0; offset < len(endpoints); offset++ {
		index := (start + offset) % len(endpoints)
		ep := endpoints[index]
		endpoint := endpointKey(ep)
		if _, quarantined := item.quarantined[endpoint]; quarantined {
			continue
		}
		stats := item.endpointStats[endpoint]
		if stats == nil {
			stats = &endpointStats{}
			item.endpointStats[endpoint] = stats
			delta.endpoints++
		}
		if selected == nil || endpointStatsLess(stats, selectedStats) {
			selected = ep
			selectedIndex = index
			selectedStats = stats
		}
	}

	if selected == nil {
		selected = endpoints[start%len(endpoints)]
		selectedIndex = start % len(endpoints)
		endpoint := endpointKey(selected)
		selectedStats = item.endpointStats[endpoint]
		if selectedStats == nil {
			selectedStats = &endpointStats{}
			item.endpointStats[endpoint] = selectedStats
			delta.endpoints++
		}
	}
	item.next = selectedIndex + 1
	selectedStats.outstanding++
	selectedStats.selections++
	return selected, delta
}

func pruneEndpointStateLocked(item *cacheItem, route *gatewayv1.ResolveServiceRouteResponse) cacheDelta {
	live := make(map[string]struct{}, len(route.GetEndpoints()))
	for _, ep := range route.GetEndpoints() {
		if endpoint := endpointKey(ep); endpoint != "" {
			live[endpoint] = struct{}{}
		}
	}
	delta := cacheDelta{}
	for endpoint := range item.endpointStats {
		if _, ok := live[endpoint]; !ok {
			delete(item.endpointStats, endpoint)
			delta.endpoints--
		}
	}
	for endpoint := range item.quarantined {
		if _, ok := live[endpoint]; !ok {
			delete(item.quarantined, endpoint)
			delta.quarantined--
		}
	}
	return delta
}

func (s *endpointStats) observe(latency time.Duration) {
	if !s.observed {
		s.ewma = latency
		s.observed = true
		return
	}
	s.ewma = time.Duration(float64(s.ewma)*(1-endpointEWMANewWeight) + float64(latency)*endpointEWMANewWeight)
}

func endpointStatsLess(candidate, current *endpointStats) bool {
	if current == nil {
		return true
	}
	if candidate.outstanding != current.outstanding {
		return candidate.outstanding < current.outstanding
	}
	if candidate.selections != current.selections {
		return candidate.selections < current.selections
	}
	if candidate.observed != current.observed {
		return !candidate.observed
	}
	if candidate.observed && candidate.ewma != current.ewma {
		return candidate.ewma < current.ewma
	}
	return false
}

func cacheKey(ref RouteRef) string {
	return ref.Namespace + "\x00" + ref.ServiceID + "\x00" + ref.PortRef
}

func endpointKey(ep *gatewayv1.ServiceRouteEndpoint) string {
	if ep == nil {
		return ""
	}
	if ep.GetAllocationID() != "" {
		return ep.GetAllocationID() + "\x00" + fmt.Sprint(ep.GetAttempt())
	}
	return ep.GetNodeID() + "\x00" + ep.GetNodeTarget() + "\x00" + fmt.Sprint(ep.GetContainerPort())
}
