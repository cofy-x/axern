package wire

type ProbeRequest struct {
	Host      string     `json:"host,omitempty"`
	TimeoutMS int        `json:"timeoutMs,omitempty"`
	HTTP      *HTTPProbe `json:"http,omitempty"`
	TCP       *TCPProbe  `json:"tcp,omitempty"`
}

type HTTPProbe struct {
	Port   int    `json:"port,omitempty"`
	Path   string `json:"path,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

type TCPProbe struct {
	Port int `json:"port,omitempty"`
}

type ProbeResponse struct {
	OK         bool   `json:"ok"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type PortSnapshot struct {
	Ports []Port `json:"ports"`
}

type Port struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	State    string `json:"state,omitempty"`
}

type MountSnapshot struct {
	Mounts []Mount `json:"mounts"`
	Paths  []Path  `json:"paths,omitempty"`
}

type FileLimitSnapshot struct {
	MaxArchiveEntries    int   `json:"maxArchiveEntries"`
	MaxArchiveBytes      int64 `json:"maxArchiveBytes"`
	MaxArchiveEntryBytes int64 `json:"maxArchiveEntryBytes"`
	MaxArchivePathDepth  int   `json:"maxArchivePathDepth"`
}

type Mount struct {
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fsType"`
	Source     string `json:"source,omitempty"`
	Options    string `json:"options,omitempty"`
}

type Path struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Writable  bool   `json:"writable,omitempty"`
	Total     uint64 `json:"totalBytes,omitempty"`
	Available uint64 `json:"availableBytes,omitempty"`
	Error     string `json:"error,omitempty"`
}
