package inspect

import "strings"

const (
	MapConfig        = "config_map"
	MapUplinkAddr    = "uplink_addr_map"
	MapNativeRoute   = "native_route_map"
	MapSNATFwd       = "snat_fwd_map"
	MapSNATRev       = "snat_rev_map"
	MapSNATRevMarker = "snat_rev_marker_map"
	MapStats         = "stats_map"
)

var knownMaps = []string{
	MapStats,
	MapConfig,
	MapUplinkAddr,
	MapNativeRoute,
	MapSNATFwd,
	MapSNATRev,
	MapSNATRevMarker,
}

var highChurnMaps = map[string]bool{
	MapSNATFwd:       true,
	MapSNATRev:       true,
	MapSNATRevMarker: true,
}

var programPins = []string{
	"ingress",
	"egress",
}

var linkPins = []string{}

type ObjectInfo struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Present    bool   `json:"present"`
	Openable   bool   `json:"openable"`
	Type       string `json:"type,omitempty"`
	KeySize    uint32 `json:"keySize,omitempty"`
	ValueSize  uint32 `json:"valueSize,omitempty"`
	MaxEntries uint32 `json:"maxEntries,omitempty"`
	Entries    int    `json:"entries,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Entry struct {
	Key   any `json:"key"`
	Value any `json:"value"`
}

type Dump struct {
	MapName   string  `json:"mapName"`
	Raw       bool    `json:"raw"`
	Limit     int     `json:"limit"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

func KnownMaps() []string {
	return append([]string(nil), knownMaps...)
}

func IsHighChurnMap(name string) bool {
	return highChurnMaps[name]
}

func NormalizeMapName(name string) string {
	name = strings.TrimSpace(name)
	for _, known := range knownMaps {
		if name == known {
			return name
		}
	}
	return name
}

func isKnownMap(name string) bool {
	for _, known := range knownMaps {
		if name == known {
			return true
		}
	}
	return false
}

var statNames = []string{
	"attach_success",
	"attach_error",
	"snat_hit",
	"snat_rev_hit",
	"snat_fwd_hit",
	"snat_udp_same_port_hit",
	"snat_udp_port_rewrite_hit",
	"snat_udp_checksum_present_hit",
	"snat_mapping_programmed",
	"snat_alloc_collision",
	"snat_fallback_hit",
	"snat_alloc_exhausted",
	"snat_tcp_non_syn_miss",
	"snat_tcp_non_syn_miss_fin",
	"snat_tcp_non_syn_miss_rst",
	"snat_tcp_non_syn_miss_ack",
	"snat_tcp_non_syn_miss_other",
	"snat_full_close_reclaim",
	"snat_full_close_mark",
	"snat_tcp_full_close_delete",
	"snat_tcp_full_close_delete_fwd",
	"snat_tcp_full_close_delete_rev",
	"snat_tcp_non_syn_miss_fwd_lookup",
	"snat_tcp_non_syn_miss_fwd_host_mismatch",
	"snat_tcp_rev_miss",
	"snat_tcp_rev_miss_syn_ack",
	"snat_tcp_rev_miss_fin",
	"snat_tcp_rev_miss_rst",
	"snat_tcp_rev_miss_ack",
	"snat_tcp_rev_miss_other",
	"native_route_skip",
}
