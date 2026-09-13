package networking

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type DnatRule struct {
	Protocol    string
	DstPort     uint16
	TargetIP    string
	TargetPort  uint16
	ContainerID string
}

func (c *Coordinator) SetupDnatRules(containerID string, ports []*commonv1.PortSpec, targetIP string) error {
	if len(ports) == 0 {
		return nil
	}
	m, ok := c.networkManager(c.natBackend)
	if !ok {
		return fmt.Errorf("network manager not found for type: %s", c.natBackend)
	}
	rules, err := DnatRulesFromPortSpecs(containerID, ports, targetIP)
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
	if err := c.storeDnatRulesLocked(); err != nil {
		delete(c.dnatRules, containerID)
		c.dnatMu.Unlock()
		return errors.Join(fmt.Errorf("persist DNAT rules: %w", err), c.rollbackDnatRules(containerID, m, installed))
	}
	c.dnatMu.Unlock()
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
		err := c.storeDnatRulesLocked()
		c.dnatMu.Unlock()
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func DnatRulesFromPortSpecs(containerID string, ports []*commonv1.PortSpec, targetIP string) ([]*DnatRule, error) {
	rules := make([]*DnatRule, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			return nil, errors.New("port specification is required")
		}
		protocol := "tcp"
		switch port.GetProtocol() {
		case commonv1.PortProtocol_PORT_PROTOCOL_UNSPECIFIED, commonv1.PortProtocol_PORT_PROTOCOL_TCP:
		case commonv1.PortProtocol_PORT_PROTOCOL_UDP:
			protocol = "udp"
		default:
			return nil, fmt.Errorf("unsupported port protocol: %s", port.GetProtocol())
		}
		containerPort := port.GetContainerPort()
		if containerPort < 1 || containerPort > 65535 {
			return nil, fmt.Errorf("container port %d is outside 1..65535", containerPort)
		}
		hostPort := port.GetHostPort()
		if hostPort == 0 {
			hostPort = containerPort
		}
		if hostPort < 1 || hostPort > 65535 {
			return nil, fmt.Errorf("host port %d is outside 1..65535", hostPort)
		}
		rules = append(rules, &DnatRule{
			Protocol:    protocol,
			DstPort:     uint16(hostPort),
			TargetIP:    targetIP,
			TargetPort:  uint16(containerPort),
			ContainerID: containerID,
		})
	}
	return rules, nil
}

func (c *Coordinator) CleanupDnatRules(containerID string) error {
	c.dnatMu.Lock()
	rules, ok := c.dnatRules[containerID]
	if !ok {
		c.dnatMu.Unlock()
		return nil
	}
	rules = append([]*DnatRule(nil), rules...)
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
	if err := c.storeDnatRulesLocked(); err != nil {
		// Keep memory aligned with the durable desired state so a retry can
		// complete the cleanup after a transient store failure.
		c.dnatRules[containerID] = rules
		c.dnatMu.Unlock()
		return fmt.Errorf("persist DNAT cleanup: %w", err)
	}
	c.dnatMu.Unlock()
	return nil
}

func (c *Coordinator) StoreDnatRules() error {
	c.dnatMu.Lock()
	defer c.dnatMu.Unlock()
	return c.storeDnatRulesLocked()
}

func (c *Coordinator) storeDnatRulesLocked() error {
	if c.store == nil {
		return nil
	}
	snapshot := &runtime.DnatRuleSnapshot{Bindings: make([]*runtime.DnatRuleBinding, 0, len(c.dnatRules))}
	allocationIDs := make([]string, 0, len(c.dnatRules))
	for allocationID := range c.dnatRules {
		allocationIDs = append(allocationIDs, allocationID)
	}
	sort.Strings(allocationIDs)
	for _, allocationID := range allocationIDs {
		rules := c.dnatRules[allocationID]
		binding := &runtime.DnatRuleBinding{AllocationID: allocationID, Ports: make([]*commonv1.PortSpec, 0, len(rules))}
		for _, r := range rules {
			protocol := commonv1.PortProtocol_PORT_PROTOCOL_TCP
			if r.Protocol == "udp" {
				protocol = commonv1.PortProtocol_PORT_PROTOCOL_UDP
			}
			binding.Ports = append(binding.Ports, &commonv1.PortSpec{Protocol: protocol, HostPort: int32(r.DstPort), ContainerPort: int32(r.TargetPort)})
		}
		snapshot.Bindings = append(snapshot.Bindings, binding)
	}
	return c.store.SaveSnapshot(config.DNATRulesBucket, snapshot)
}

func (c *Coordinator) LoadDnatRules() {
	if c.store == nil {
		c.reconcileDnatRules()
		return
	}
	var snapshot runtime.DnatRuleSnapshot
	err := c.store.LoadSnapshot(config.DNATRulesBucket, &snapshot)
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
	for _, binding := range snapshot.GetBindings() {
		cid := binding.GetAllocationID()
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
		rules, err := DnatRulesFromPortSpecs(cid, binding.GetPorts(), netDevice.Ip.String())
		if err != nil {
			c.logger.Warnf("dnat: failed to parse stored rules for container %s: %v", cid, err)
			continue
		}
		c.dnatRules[cid] = rules
		restored++
	}
	c.dnatMu.Unlock()
	c.reconcileDnatRules()
	if err := c.StoreDnatRules(); err != nil {
		c.logger.Warnf("store reconciled dnat rules failed: %v", err)
	}
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
