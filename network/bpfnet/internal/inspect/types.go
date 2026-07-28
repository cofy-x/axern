package inspect

import "strings"

const (
	MapService       = "service_map"
	MapLocalAddr     = "local_addr_map"
	MapRevNAT        = "rev_nat_map"
	MapConfig        = "config_map"
	MapHostNetNS     = "host_netns_cookie_map"
	MapUplinkAddr    = "uplink_addr_map"
	MapNativeRoute   = "native_route_map"
	MapSNATFwd       = "snat_fwd_map"
	MapSNATRev       = "snat_rev_map"
	MapSNATRevMarker = "snat_rev_marker_map"
	MapLocalhostSock = "localhost_sock_map"
	MapStats         = "stats_map"
)

var knownMaps = []string{
	MapService,
	MapStats,
	MapLocalAddr,
	MapRevNAT,
	MapConfig,
	MapHostNetNS,
	MapUplinkAddr,
	MapNativeRoute,
	MapSNATFwd,
	MapSNATRev,
	MapSNATRevMarker,
	MapLocalhostSock,
}

var highChurnMaps = map[string]bool{
	MapRevNAT:        true,
	MapSNATFwd:       true,
	MapSNATRev:       true,
	MapSNATRevMarker: true,
	MapLocalhostSock: true,
}

var programPins = []string{
	"ingress",
	"egress",
	"localhost-connect4",
	"localhost-getpeer4",
	"localhost-release",
}

var linkPins = []string{
	"localhost-connect4",
	"localhost-getpeer4",
	"localhost-release",
}

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
	"service_hit",
	"rev_nat_hit",
	"fallback_hit",
	"map_conflict",
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
	"localhost_connect_hit",
	"localhost_getpeer_hit",
	"localhost_fallback_hit",
}
