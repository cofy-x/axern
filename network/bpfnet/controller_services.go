package bpfnet

import (
	"fmt"
	"strings"
)

func (c *Controller) UpsertService(protocol string, hostPort uint16, targetIP string, targetPort uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	services, err := c.loadServicesLocked()
	if err != nil {
		return err
	}

	key := serviceKey(protocol, hostPort)
	next := Service{
		Protocol:   strings.ToLower(protocol),
		HostPort:   hostPort,
		TargetIP:   targetIP,
		TargetPort: targetPort,
	}
	if current, ok := services[key]; ok {
		if current != next {
			c.bumpStats(func(s *Stats) {
				s.Conflicts++
			})
			return fmt.Errorf("service conflict for %s:%d", protocol, hostPort)
		}
		return nil
	}

	state := c.currentStateLocked()
	programmed := false
	if isDataplaneManagedProtocol(next.Protocol) && state.TCReady {
		if err := c.dataplaneLocked().UpsertService(next); err != nil {
			return err
		}
		programmed = true
	}
	services[key] = next
	if err := writeJSONFile(c.svcFile, flattenServices(services)); err != nil {
		if programmed {
			_ = c.dataplaneLocked().DeleteService(next)
		}
		return err
	}
	c.bumpStats(func(s *Stats) {
		s.Upserts++
		if !isDataplaneManagedProtocol(next.Protocol) || !state.TCReady {
			s.Fallbacks++
		}
	})
	return nil
}

func (c *Controller) DeleteService(protocol string, hostPort uint16, _ string, _ uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	services, err := c.loadServicesLocked()
	if err != nil {
		return err
	}

	key := serviceKey(protocol, hostPort)
	current, ok := services[key]
	if !ok {
		return nil
	}

	state := c.currentStateLocked()
	programmed := false
	if isDataplaneManagedProtocol(current.Protocol) && state.TCReady {
		if err := c.dataplaneLocked().DeleteService(current); err != nil {
			return err
		}
		programmed = true
	}
	delete(services, key)
	if err := writeJSONFile(c.svcFile, flattenServices(services)); err != nil {
		if programmed {
			_ = c.dataplaneLocked().UpsertService(current)
		}
		return err
	}
	c.bumpStats(func(s *Stats) {
		s.Deletes++
		if !isDataplaneManagedProtocol(current.Protocol) || !state.TCReady {
			s.Fallbacks++
		}
	})
	return nil
}

func isDataplaneManagedProtocol(protocol string) bool {
	switch strings.ToLower(protocol) {
	case "tcp", "udp":
		return true
	default:
		return false
	}
}

func flattenServices(in map[string]Service) []Service {
	out := make([]Service, 0, len(in))
	for _, svc := range in {
		out = append(out, svc)
	}
	return out
}

func serviceKey(protocol string, hostPort uint16) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(protocol), hostPort)
}
