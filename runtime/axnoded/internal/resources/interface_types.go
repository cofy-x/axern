package resources

import (
	"encoding/json"
	"math"
	"net"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

func netnsName(ip string) string {
	return "axctl-" + strings.ReplaceAll(strings.TrimPrefix(ip, "0x"), ".", "-")
}

func netnsPath(ip string) string {
	return filepath.Join("/var/run/netns", netnsName(ip))
}

type NetResource struct {
	Interface *net.Interface `json:"interface" protobuf:"bytes,0,opt,name=interface"`
	Ip        net.IP         `json:"ip" protobuf:"bytes,1,opt,name=ip"`
	Mask      net.IPMask     `json:"mask" protobuf:"bytes,2,opt,name=mask"`
	Gateway   net.IP         `json:"gateway" protobuf:"bytes,3,opt,name=gateway"`
	Type      string         `json:"type" protobuf:"bytes,4,opt,name=type"`
	NetNSPath string         `json:"netnsPath,omitempty" protobuf:"bytes,5,opt,name=netnsPath"`
}

func (n *NetResource) ToString() string {
	bytes, _ := json.Marshal(n)
	return string(bytes)
}

func (n *NetResource) FromString(s string) error {
	return json.Unmarshal([]byte(s), n)
}

func NewNetResource(str string) (*NetResource, error) {
	n := &NetResource{}
	err := n.FromString(str)
	return n, err
}

// Interface cache should not significantly exceed the effective task capacity
// of the local machine.
func calcluteCacheSize(cacheSize int) int {
	return calcluteCacheSizeWithCPUProbe(cacheSize, getLocalCpuNum)
}

func calcluteCacheSizeWithCPUProbe(cacheSize int, cpuProbe func() (int, error)) int {
	cellNum, err := cpuProbe()
	if err != nil {
		logrus.Errorf("get local cpu num failed: %v", err)
		return cacheSize
	}

	minTaskSize := 0.5
	maxTaskCount := int(math.Ceil(float64(cellNum) / minTaskSize))
	maxInterfaceCount := int(math.Ceil(float64(maxTaskCount) * float64(1.1)))

	logrus.Infof("max task count: %v, max calculative interface count: %v", maxTaskCount, maxInterfaceCount)

	if cacheSize > maxTaskCount {
		cacheSize = maxTaskCount
	}

	return cacheSize
}

var _ Resource = &NetResource{}
