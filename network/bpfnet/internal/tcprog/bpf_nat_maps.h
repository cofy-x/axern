#ifndef BPF_NAT_MAPS_H
#define BPF_NAT_MAPS_H

#define SNAT_PORT_MIN 10000
#define SNAT_PORT_MAX 65535
#define SNAT_PORT_ATTEMPTS 256
#define SNAT_FLOW_ACTIVE 0
#define SNAT_FLOW_ORIG_CLOSING 1
#define SNAT_FLOW_REPLY_CLOSING 2
#define SNAT_FLOW_CLOSING 3

struct config_value {
	__u32 sandbox_addr;
	__u32 sandbox_mask;
	__u32 native_routes_enabled;
};

struct uplink_addr_key {
	__u32 ifindex;
};

struct uplink_addr_value {
	__u32 addr;
};

struct native_route_key {
	__u32 prefixlen;
	__u32 addr;
};

struct native_route_value {
	__u8 present;
	__u8 pad[3];
};

struct snat_fwd_key {
	__u32 src_ip;
	__u32 dst_ip;
	__u16 src_port;
	__u16 dst_port;
	__u8 proto;
	__u8 pad[3];
};

struct snat_fwd_value {
	__u32 host_ip;
	__u16 translated_src;
	__u8 state;
	__u8 pad0;
	__u64 last_seen_ns;
};

struct snat_rev_key {
	__u32 src_ip;
	__u32 dst_ip;
	__u16 src_port;
	__u16 dst_port;
	__u8 proto;
	__u8 flags;
	__u8 pad[2];
};

struct snat_rev_value {
	__u32 target_ip;
	__u32 host_ip;
	__u16 target_port;
	__u16 translated_src;
	__u8 state;
	__u8 pad[3];
	__u64 last_seen_ns;
};

struct snat_rev_marker_value {
	__u64 last_seen_ns;
};

enum stat_index {
	STAT_ATTACH_SUCCESS = 0,
	STAT_ATTACH_ERROR = 1,
	STAT_SNAT_HIT = 2,
	STAT_SNAT_REV_HIT = 3,
	STAT_SNAT_FWD_HIT = 4,
	STAT_SNAT_UDP_SAME_PORT_HIT = 5,
	STAT_SNAT_UDP_PORT_REWRITE_HIT = 6,
	STAT_SNAT_UDP_CHECKSUM_PRESENT_HIT = 7,
	STAT_SNAT_MAPPING_PROGRAMMED = 8,
	STAT_SNAT_ALLOC_COLLISION = 9,
	STAT_SNAT_FALLBACK_HIT = 10,
	STAT_SNAT_ALLOC_EXHAUSTED = 11,
	STAT_SNAT_TCP_NON_SYN_MISS = 12,
	STAT_SNAT_TCP_NON_SYN_MISS_FIN = 13,
	STAT_SNAT_TCP_NON_SYN_MISS_RST = 14,
	STAT_SNAT_TCP_NON_SYN_MISS_ACK = 15,
	STAT_SNAT_TCP_NON_SYN_MISS_OTHER = 16,
	STAT_SNAT_FULL_CLOSE_RECLAIM = 17,
	STAT_SNAT_FULL_CLOSE_MARK = 18,
	STAT_SNAT_TCP_FULL_CLOSE_DELETE = 19,
	STAT_SNAT_TCP_FULL_CLOSE_DELETE_FWD = 20,
	STAT_SNAT_TCP_FULL_CLOSE_DELETE_REV = 21,
	STAT_SNAT_TCP_NON_SYN_MISS_FWD_LOOKUP = 22,
	STAT_SNAT_TCP_NON_SYN_MISS_FWD_HOST_MISMATCH = 23,
	STAT_SNAT_TCP_REV_MISS = 24,
	STAT_SNAT_TCP_REV_MISS_SYN_ACK = 25,
	STAT_SNAT_TCP_REV_MISS_FIN = 26,
	STAT_SNAT_TCP_REV_MISS_RST = 27,
	STAT_SNAT_TCP_REV_MISS_ACK = 28,
	STAT_SNAT_TCP_REV_MISS_OTHER = 29,
	STAT_NATIVE_ROUTE_SKIP = 30,
	STAT_MAX = 31,
};

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct config_value);
} config_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 64);
	__type(key, struct uplink_addr_key);
	__type(value, struct uplink_addr_value);
} uplink_addr_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LPM_TRIE);
	__uint(map_flags, BPF_F_NO_PREALLOC);
	__uint(max_entries, 256);
	__type(key, struct native_route_key);
	__type(value, struct native_route_value);
} native_route_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 16384);
	__type(key, struct snat_fwd_key);
	__type(value, struct snat_fwd_value);
} snat_fwd_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 16384);
	__type(key, struct snat_rev_key);
	__type(value, struct snat_rev_value);
} snat_rev_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 16384);
	__type(key, struct snat_rev_key);
	__type(value, struct snat_rev_marker_value);
} snat_rev_marker_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, STAT_MAX);
	__type(key, __u32);
	__type(value, __u64);
} stats_map SEC(".maps");

static __always_inline void bump_stat(__u32 index)
{
	__u64 *value;

	value = bpf_map_lookup_elem(&stats_map, &index);
	if (value)
		(*value)++;
}

#endif
