package networking

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type DnatRule struct {
	Protocol    string
	DstPort     uint16
	TargetIP    string
	TargetPort  uint16
	ContainerID string
}

func (c *Coordinator) SetupDnatRules(containerID string, ports []string, targetIP string) error {
	if len(ports) == 0 {
		return nil
	}
	m, ok := c.networkManager(c.natBackend)
	if !ok {
		return fmt.Errorf("network manager not found for type: %s", c.natBackend)
	}
	rules, err := ParseDnatRules(containerID, ports, targetIP)
	if err != nil {
		return err
	}
	installed := make([]*DnatRule, 0, len(rules))
	for _, rule := range rules {
		if err := m.SetupDNATRule(rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort); err != nil {
			setupErr := fmt.Errorf("failed to add DNAT rule for %s:%d->%s:%d: %v", rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort, err)
			return errors.Join(setupErr, c.rollbackDnatRules(containerID, m, installed))
		}
		installed = append(installed, rule)
		c.logger.Infof("Added DNAT rule: %s:%d -> %s:%d for container %s", rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort, containerID)
	}
	c.dnatMu.Lock()
	c.dnatRules[containerID] = rules
	c.dnatMu.Unlock()
	c.StoreDnatRules()
	return nil
}

func (c *Coordinator) rollbackDnatRules(containerID string, m networkmanager.NetworkManager, rules []*DnatRule) error {
	var errs []error
	remaining := make([]*DnatRule, 0, len(rules))
	for i := len(rules) - 1; i >= 0; i-- {
		rule := rules[i]
		if err := m.CleanupDNATRule(rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort); err != nil {
			errs = append(errs, fmt.Errorf("roll back DNAT rule %s:%d->%s:%d: %w", rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort, err))
			remaining = append(remaining, rule)
		}
	}
	if len(remaining) > 0 {
		c.dnatMu.Lock()
		c.dnatRules[containerID] = remaining
		c.dnatMu.Unlock()
		c.StoreDnatRules()
	}
	return errors.Join(errs...)
}

func ParseDnatRules(containerID string, ports []string, targetIP string) ([]*DnatRule, error) {
	rules := make([]*DnatRule, 0, len(ports))
	for _, port := range ports {
		if port == "" {
			continue
		}
		parts := strings.Split(port, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid port format: %s, expected format: protocol:dstPort:targetPort", port)
		}
		dstPort, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid dstPort: %s, err: %v", parts[1], err)
		}
		targetPort, err := strconv.ParseUint(parts[2], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid targetPort: %s, err: %v", parts[2], err)
		}
		rules = append(rules, &DnatRule{
			Protocol:    parts[0],
			DstPort:     uint16(dstPort),
			TargetIP:    targetIP,
			TargetPort:  uint16(targetPort),
			ContainerID: containerID,
		})
	}
	return rules, nil
}

func (c *Coordinator) CleanupDnatRules(containerID string) {
	c.dnatMu.Lock()
	rules, ok := c.dnatRules[containerID]
	if !ok {
		c.dnatMu.Unlock()
		return
	}
	c.dnatMu.Unlock()

	m, mOk := c.networkManager(c.natBackend)
	remaining := make([]*DnatRule, 0, len(rules))
	for _, rule := range rules {
		if !mOk {
			c.logger.Warnf("network manager not found for type %s, cannot delete DNAT rule %s:%d->%s:%d", c.natBackend, rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort)
			remaining = append(remaining, rule)
			continue
		}
		if err := m.CleanupDNATRule(rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort); err != nil {
			c.logger.Warnf("failed to delete DNAT rule for %s:%d->%s:%d: %v", rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort, err)
			remaining = append(remaining, rule)
			continue
		}
		c.logger.Infof("Deleted DNAT rule: %s:%d -> %s:%d for container %s", rule.Protocol, rule.DstPort, rule.TargetIP, rule.TargetPort, containerID)
	}
	c.dnatMu.Lock()
	if len(remaining) == 0 {
		delete(c.dnatRules, containerID)
	} else {
		c.dnatRules[containerID] = remaining
	}
	c.dnatMu.Unlock()
	c.StoreDnatRules()
}

func (c *Coordinator) StoreDnatRules() {
	if c.store == nil {
		return
	}
	c.dnatMu.Lock()
	m := make(map[string]string, len(c.dnatRules))
	for cid, rules := range c.dnatRules {
		var ports []string
		for _, r := range rules {
			ports = append(ports, fmt.Sprintf("%s:%d:%d", r.Protocol, r.DstPort, r.TargetPort))
		}
		m[cid] = strings.Join(ports, ",")
	}
	c.dnatMu.Unlock()
	if err := c.store.SaveSnapshot(config.DNATRulesBucket, &runtime.Map{Items: m}); err != nil {
		c.logger.Warnf("store dnat rules failed: %v", err)
	}
}

func (c *Coordinator) LoadDnatRules() {
	if c.store == nil {
		c.reconcileDnatRules()
		return
	}
	var m runtime.Map
	err := c.store.LoadSnapshot(config.DNATRulesBucket, &m)
	if err != nil {
		if errord.IsNotFound(err) {
			c.reconcileDnatRules()
			return
		}
		c.logger.Warnf("load dnat rules failed: %v", err)
		return
	}
	restored := 0
	c.dnatMu.Lock()
	for cid, portsStr := range m.Items {
		if c.containerExists != nil && !c.containerExists(cid) {
			c.logger.Debugf("dnat: container %s no longer exists, skip", cid)
			continue
		}
		resource, err := c.resourceForContainer(cid)
		if err != nil {
			c.logger.Warnf("dnat: failed to get resource for container %s: %v", cid, err)
			continue
		}
		netDevice, err := netResourceFromOccupied(resource)
		if err != nil {
			c.logger.Warnf("dnat: failed to parse network device for container %s: %v", cid, err)
			continue
		}
		rules, err := ParseDnatRules(cid, strings.Split(portsStr, ","), netDevice.Ip.String())
		if err != nil {
			c.logger.Warnf("dnat: failed to parse stored rules for container %s: %v", cid, err)
			continue
		}
		c.dnatRules[cid] = rules
		restored++
	}
	c.dnatMu.Unlock()
	c.reconcileDnatRules()
	c.StoreDnatRules()
	if restored > 0 {
		c.logger.Infof("restored DNAT rules for %d containers", restored)
	}
}

func (c *Coordinator) reconcileDnatRules() {
	m, ok := c.networkManager(c.natBackend)
	if !ok {
		c.logger.Warnf("network manager not found for type %s, cannot reconcile DNAT rules", c.natBackend)
		return
	}
	reconciler, ok := m.(networkmanager.DNATReconciler)
	if !ok {
		return
	}

	c.dnatMu.Lock()
	desired := make([]networkmanager.DNATRule, 0)
	for _, rules := range c.dnatRules {
		for _, rule := range rules {
			desired = append(desired, networkmanager.DNATRule{
				Protocol:   rule.Protocol,
				HostPort:   rule.DstPort,
				TargetIP:   rule.TargetIP,
				TargetPort: rule.TargetPort,
			})
		}
	}
	c.dnatMu.Unlock()

	if err := reconciler.ReconcileDNATRules(desired); err != nil {
		c.logger.Warnf("reconcile DNAT rules: %v", err)
	}
}

func (c *Coordinator) DnatRules(containerID string) []*DnatRule {
	c.dnatMu.Lock()
	defer c.dnatMu.Unlock()
	return append([]*DnatRule(nil), c.dnatRules[containerID]...)
}

func (c *Coordinator) DnatRuleCount() int {
	c.dnatMu.Lock()
	defer c.dnatMu.Unlock()
	return len(c.dnatRules)
}
