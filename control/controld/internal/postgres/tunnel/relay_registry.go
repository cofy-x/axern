package pgtunnel

import (
	"fmt"
	"strconv"
	"strings"
)

type Relay struct {
	ID           string
	ClientTarget string
	NodeTarget   string
	Weight       int
	Drain        bool
}

func ParseRelays(spec string) ([]Relay, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("tunnel relay registry is required")
	}
	parts := strings.Split(spec, ";")
	relays := make([]Relay, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ",")
		if len(fields) != 5 {
			return nil, fmt.Errorf("relay %q must be id,client_target,node_target,weight,drain", part)
		}
		relay := Relay{
			ID:           strings.TrimSpace(fields[0]),
			ClientTarget: strings.TrimSpace(fields[1]),
			NodeTarget:   strings.TrimSpace(fields[2]),
		}
		if relay.ID == "" || relay.ClientTarget == "" || relay.NodeTarget == "" {
			return nil, fmt.Errorf("relay id, client_target, and node_target are required")
		}
		if _, ok := seen[relay.ID]; ok {
			return nil, fmt.Errorf("duplicate tunnel relay id %q", relay.ID)
		}
		weight, err := strconv.Atoi(strings.TrimSpace(fields[3]))
		if err != nil || weight <= 0 {
			return nil, fmt.Errorf("relay %q weight must be > 0", relay.ID)
		}
		drain, err := strconv.ParseBool(strings.TrimSpace(fields[4]))
		if err != nil {
			return nil, fmt.Errorf("relay %q drain must be true or false", relay.ID)
		}
		relay.Weight = weight
		relay.Drain = drain
		relays = append(relays, relay)
		seen[relay.ID] = struct{}{}
	}
	return normalizeRelays(relays), nil
}

func normalizeRelays(relays []Relay) []Relay {
	out := make([]Relay, 0, len(relays))
	for _, relay := range relays {
		relay.ID = strings.TrimSpace(relay.ID)
		relay.ClientTarget = strings.TrimSpace(relay.ClientTarget)
		relay.NodeTarget = strings.TrimSpace(relay.NodeTarget)
		if relay.ID == "" || relay.ClientTarget == "" || relay.NodeTarget == "" || relay.Weight <= 0 {
			continue
		}
		out = append(out, relay)
	}
	return out
}

func (s *Store) selectRelay(seed string) (Relay, error) {
	if len(s.relays) == 0 {
		return Relay{}, fmt.Errorf("tunnel relay registry is empty")
	}
	total := 0
	for _, relay := range s.relays {
		if !relay.Drain {
			total += relay.Weight
		}
	}
	if total <= 0 {
		return Relay{}, fmt.Errorf("no non-draining tunnel relays are available")
	}
	slot := stableSlot(seed, total)
	for _, relay := range s.relays {
		if relay.Drain {
			continue
		}
		if slot < relay.Weight {
			return relay, nil
		}
		slot -= relay.Weight
	}
	return Relay{}, fmt.Errorf("select tunnel relay")
}

func stableSlot(seed string, total int) int {
	var h uint32 = 2166136261
	for _, b := range []byte(seed) {
		h ^= uint32(b)
		h *= 16777619
	}
	return int(h % uint32(total))
}
