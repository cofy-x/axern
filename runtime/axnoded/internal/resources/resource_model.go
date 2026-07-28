package resources

type ResourceName string

type PoolStatus struct {
	Using       int
	Idle        int
	Capacity    int
	Unavailable int
}

const (
	CgroupResourceName    ResourceName = "cgroup"
	InterfaceResourceName ResourceName = "interface"

	ResourceAnnotationKeyPrefix = "io.axnoded.resource/"
	CgroupPrefix                = "/sandbox/"
)

const (
	defaultIpRange = "172.17.0.1/16"

	bridgeName        = "sandbox0"
	containerEthName  = "eth0"
	ContainerLoopName = "lo"
	bridgeMac         = "02:3f:e1:bd:13:b8"

	// maxVethNum is the max number of veth pair,
	// can not exceed 1024  which is  the limitation of netlink for one bridge device.
	maxVethNum = 1000
)

type StringResource string

var (
	emptyString                  = StringResource("")
	EmptyStringResource Resource = &emptyString
)

type Resource interface {
	ToString() string
	FromString(string) error
}

func (s *StringResource) ToString() string {
	return string(*s)
}

func (s *StringResource) FromString(str string) error {
	*s = StringResource(str)
	return nil
}

func NewStringResource(str string) Resource {
	s := StringResource(str)
	return &s
}
