#ifndef BPF_NAT_SNAT_H
#define BPF_NAT_SNAT_H

#define SNAT_RESERVE_CONFLICT 0
#define SNAT_RESERVE_REUSED 1
#define SNAT_RESERVE_INSERTED 2
#define SNAT_ENTRY_REVERSE 1
#define SNAT_ENTRY_ALIAS 2
#define TCP_FLAG_FIN 0x01
#define TCP_FLAG_SYN 0x02
#define TCP_FLAG_RST 0x04
#define TCP_FLAG_ACK 0x10
#define SNAT_NON_SYN_MISS_FWD_LOOKUP 1
#define SNAT_NON_SYN_MISS_FWD_HOST_MISMATCH 2

static __always_inline struct config_value *lookup_config(void)
{
	__u32 key = 0;

	return bpf_map_lookup_elem(&config_map, &key);
}

static __always_inline bool in_sandbox_cidr_with_config(const struct config_value *cfg, __u32 addr)
{
	if (!cfg)
		return false;
	return (addr & cfg->sandbox_mask) == cfg->sandbox_addr;
}

static __always_inline bool is_native_route_with_config(const struct config_value *cfg, __u32 addr)
{
	struct native_route_key key = {
		.prefixlen = 32,
		.addr = addr,
	};

	if (!cfg || cfg->native_routes_enabled == 0)
		return false;
	return bpf_map_lookup_elem(&native_route_map, &key) != 0;
}

static __always_inline __u8 snat_tcp_close_state(const struct tcphdr *tcph, __u8 close_state)
{
	__u8 flags = ((__u8 *)tcph)[13];

	if (flags & TCP_FLAG_RST)
		return SNAT_FLOW_CLOSING;
	if (flags & TCP_FLAG_FIN)
		return close_state;
	return SNAT_FLOW_ACTIVE;
}

static __always_inline bool snat_tcp_initial_syn(const struct tcphdr *tcph)
{
	__u8 flags = ((__u8 *)tcph)[13];

	return (flags & TCP_FLAG_SYN) != 0 && (flags & TCP_FLAG_ACK) == 0;
}

static __always_inline void record_snat_tcp_flags(__u8 flags,
						  __u32 rst_stat,
						  __u32 fin_stat,
						  __u32 ack_stat,
						  __u32 other_stat)
{
	if (flags & TCP_FLAG_RST) {
		bump_stat(rst_stat);
		return;
	}
	if (flags & TCP_FLAG_FIN) {
		bump_stat(fin_stat);
		return;
	}
	if (flags & TCP_FLAG_ACK) {
		bump_stat(ack_stat);
		return;
	}
	bump_stat(other_stat);
}

static __always_inline void record_snat_tcp_non_syn_miss(const struct tcphdr *tcph, __u8 reason)
{
	__u8 flags = ((__u8 *)tcph)[13];

	bump_stat(STAT_SNAT_TCP_NON_SYN_MISS);
	record_snat_tcp_flags(flags,
			      STAT_SNAT_TCP_NON_SYN_MISS_RST,
			      STAT_SNAT_TCP_NON_SYN_MISS_FIN,
			      STAT_SNAT_TCP_NON_SYN_MISS_ACK,
			      STAT_SNAT_TCP_NON_SYN_MISS_OTHER);
	if (reason == SNAT_NON_SYN_MISS_FWD_LOOKUP)
		bump_stat(STAT_SNAT_TCP_NON_SYN_MISS_FWD_LOOKUP);
	else if (reason == SNAT_NON_SYN_MISS_FWD_HOST_MISMATCH)
		bump_stat(STAT_SNAT_TCP_NON_SYN_MISS_FWD_HOST_MISMATCH);
}

static __always_inline void record_snat_tcp_reverse_miss(const struct tcphdr *tcph)
{
	__u8 flags = ((__u8 *)tcph)[13];

	bump_stat(STAT_SNAT_TCP_REV_MISS);
	if ((flags & TCP_FLAG_SYN) && (flags & TCP_FLAG_ACK)) {
		bump_stat(STAT_SNAT_TCP_REV_MISS_SYN_ACK);
		return;
	}
	record_snat_tcp_flags(flags,
			      STAT_SNAT_TCP_REV_MISS_RST,
			      STAT_SNAT_TCP_REV_MISS_FIN,
			      STAT_SNAT_TCP_REV_MISS_ACK,
			      STAT_SNAT_TCP_REV_MISS_OTHER);
}

static __always_inline void remember_snat_reverse_tuple(const struct snat_rev_key *key, __u64 now)
{
	struct snat_rev_marker_value value = {
		.last_seen_ns = now,
	};

	if (key->proto != IPPROTO_TCP)
		return;
	bpf_map_update_elem(&snat_rev_marker_map, key, &value, BPF_ANY);
}

static __always_inline bool known_snat_reverse_tuple(const struct snat_rev_key *key)
{
	if (key->proto != IPPROTO_TCP)
		return false;
	return bpf_map_lookup_elem(&snat_rev_marker_map, key) != 0;
}

static __always_inline void record_snat_tcp_full_close_delete(bool reverse_direction)
{
	bump_stat(STAT_SNAT_TCP_FULL_CLOSE_DELETE);
	if (reverse_direction)
		bump_stat(STAT_SNAT_TCP_FULL_CLOSE_DELETE_REV);
	else
		bump_stat(STAT_SNAT_TCP_FULL_CLOSE_DELETE_FWD);
}

static __always_inline __u8 merged_snat_state(__u8 current, __u8 next)
{
	return current | next;
}

static __always_inline bool snat_flow_is_closing(__u8 state)
{
	return state != SNAT_FLOW_ACTIVE;
}

static __always_inline void touch_snat_fwd_value(struct snat_fwd_value *value, __u64 now, __u8 state)
{
	__u8 next_state;

	if (!value)
		return;
	if (snat_flow_is_closing(value->state) && state == SNAT_FLOW_ACTIVE)
		return;
	next_state = merged_snat_state(value->state, state);
	if (next_state == SNAT_FLOW_CLOSING && value->state != SNAT_FLOW_CLOSING)
		bump_stat(STAT_SNAT_FULL_CLOSE_MARK);
	value->last_seen_ns = now;
	value->state = next_state;
}

static __always_inline void touch_snat_rev_value(struct snat_rev_value *value, __u64 now, __u8 state)
{
	__u8 next_state;

	if (!value)
		return;
	if (snat_flow_is_closing(value->state) && state == SNAT_FLOW_ACTIVE)
		return;
	next_state = merged_snat_state(value->state, state);
	if (next_state == SNAT_FLOW_CLOSING && value->state != SNAT_FLOW_CLOSING)
		bump_stat(STAT_SNAT_FULL_CLOSE_MARK);
	value->last_seen_ns = now;
	value->state = next_state;
}

static __always_inline void touch_snat_fwd_mapping(const struct snat_fwd_key *key,
						  __u32 host_ip,
						  __u16 translated_src,
						  __u64 now,
						  __u8 state)
{
	struct snat_fwd_value *value;

	value = bpf_map_lookup_elem(&snat_fwd_map, key);
	if (!value)
		return;
	if (value->host_ip != host_ip || value->translated_src != translated_src)
		return;
	touch_snat_fwd_value(value, now, state);
}

static __always_inline void touch_snat_rev_mapping(const struct snat_rev_key *key,
						  __u32 target_ip,
						  __u16 target_port,
						  __u32 host_ip,
						  __u64 now,
						  __u8 state)
{
	struct snat_rev_value *value;

	value = bpf_map_lookup_elem(&snat_rev_map, key);
	if (!value)
		return;
	if (value->target_ip != target_ip ||
	    value->target_port != target_port ||
	    value->host_ip != host_ip)
		return;
	remember_snat_reverse_tuple(key, now);
	touch_snat_rev_value(value, now, state);
}

static __always_inline void mark_snat_tcp_mapping_close(const struct snat_fwd_key *fwd_key,
							const struct snat_rev_key *rev_key,
							__u64 now,
							__u8 state)
{
	struct snat_fwd_value *fwd_value;
	struct snat_rev_value *rev_value;
	bool marked = false;
	__u8 next_state;

	if (state == SNAT_FLOW_ACTIVE)
		return;
	fwd_value = bpf_map_lookup_elem(&snat_fwd_map, fwd_key);
	if (fwd_value) {
		next_state = merged_snat_state(fwd_value->state, state);
		if (next_state == SNAT_FLOW_CLOSING && fwd_value->state != SNAT_FLOW_CLOSING)
			marked = true;
		fwd_value->state = next_state;
		fwd_value->last_seen_ns = now;
	}
	rev_value = bpf_map_lookup_elem(&snat_rev_map, rev_key);
	if (rev_value) {
		next_state = merged_snat_state(rev_value->state, state);
		if (next_state == SNAT_FLOW_CLOSING && rev_value->state != SNAT_FLOW_CLOSING)
			marked = true;
		rev_value->state = next_state;
		rev_value->last_seen_ns = now;
		remember_snat_reverse_tuple(rev_key, now);
	}
	if (marked)
		bump_stat(STAT_SNAT_FULL_CLOSE_MARK);
}

static __always_inline void delete_snat_tcp_mapping(const struct snat_fwd_key *fwd_key,
						    const struct snat_rev_key *rev_key)
{
	bpf_map_delete_elem(&snat_fwd_map, fwd_key);
	bpf_map_delete_elem(&snat_rev_map, rev_key);
}

static __always_inline long reserve_snat_rev_mapping(const struct snat_rev_key *key,
						     const struct snat_rev_value *expected)
{
	struct snat_rev_value *existing;
	struct snat_fwd_key stale_fwd_key = {};
	long err;

	err = bpf_map_update_elem(&snat_rev_map, key, expected, BPF_NOEXIST);
	if (err == 0)
		return SNAT_RESERVE_INSERTED;
	if (err != -EEXIST)
		return err;

	existing = bpf_map_lookup_elem(&snat_rev_map, key);
	if (!existing)
		return -1;
	if (existing->target_ip == expected->target_ip &&
	    existing->target_port == expected->target_port &&
	    existing->host_ip == expected->host_ip) {
		if (snat_flow_is_closing(existing->state) && expected->state == SNAT_FLOW_ACTIVE) {
			existing->state = SNAT_FLOW_ACTIVE;
			existing->last_seen_ns = expected->last_seen_ns;
		} else {
			touch_snat_rev_value(existing, expected->last_seen_ns, expected->state);
		}
		return SNAT_RESERVE_REUSED;
	}
	if (existing->state == SNAT_FLOW_CLOSING && expected->state == SNAT_FLOW_ACTIVE) {
		stale_fwd_key.src_ip = existing->target_ip;
		stale_fwd_key.dst_ip = key->src_ip;
		stale_fwd_key.src_port = existing->target_port;
		stale_fwd_key.dst_port = key->src_port;
		stale_fwd_key.proto = key->proto;
		bpf_map_delete_elem(&snat_fwd_map, &stale_fwd_key);
		existing->target_ip = expected->target_ip;
		existing->host_ip = expected->host_ip;
		existing->target_port = expected->target_port;
		existing->translated_src = key->dst_port;
		existing->state = SNAT_FLOW_ACTIVE;
		existing->last_seen_ns = expected->last_seen_ns;
		bump_stat(STAT_SNAT_FULL_CLOSE_RECLAIM);
		return SNAT_RESERVE_INSERTED;
	}

	return SNAT_RESERVE_CONFLICT;
}

static __always_inline __u16 select_translated_id(const struct snat_rev_key *template_key,
						  const struct snat_rev_value *expected,
						  __u16 preferred,
						  __u8 *inserted_new)
{
	struct snat_rev_key probe = *template_key;
	__u32 range;
	__u32 seed;
	__u32 stride;
	__u32 offset;
	long reserve_result;
	int i;

	if (inserted_new)
		*inserted_new = 0;

	probe.dst_port = preferred;
	reserve_result = reserve_snat_rev_mapping(&probe, expected);
	if (reserve_result == SNAT_RESERVE_INSERTED ||
	    reserve_result == SNAT_RESERVE_REUSED) {
		if (inserted_new)
			*inserted_new = reserve_result != SNAT_RESERVE_REUSED;
		return preferred;
	}
	if (reserve_result < 0)
		return 0;

	bump_stat(STAT_SNAT_ALLOC_COLLISION);
	range = SNAT_PORT_MAX - SNAT_PORT_MIN + 1;
	seed = template_key->src_ip ^ template_key->dst_ip ^
		((__u32)template_key->src_port << 16) ^ template_key->dst_port ^
		((__u32)template_key->proto << 24) ^ expected->target_ip;
	seed ^= seed >> 16;
	seed *= 2654435761U;
	stride = ((seed >> 16) | 1);
	for (i = 0; i < SNAT_PORT_ATTEMPTS; i++) {
		offset = (seed + ((__u32)i * stride)) % range;
		probe.dst_port = (__u16)(SNAT_PORT_MIN + offset);
		reserve_result = reserve_snat_rev_mapping(&probe, expected);
		if (reserve_result == SNAT_RESERVE_INSERTED ||
		    reserve_result == SNAT_RESERVE_REUSED) {
			if (inserted_new)
				*inserted_new = reserve_result != SNAT_RESERVE_REUSED;
			return probe.dst_port;
		}
		if (reserve_result < 0)
			return 0;
	}

	bump_stat(STAT_SNAT_ALLOC_EXHAUSTED);
	return 0;
}

static __always_inline int program_snat_mapping(struct snat_fwd_key *fwd_key,
						struct snat_rev_key *rev_key,
						struct snat_rev_value *rev_value,
						__u32 host_ip, __u16 preferred_src,
						__u64 now, __u8 state,
						__u8 *inserted_new)
{
	struct snat_fwd_value next = {
		.host_ip = host_ip,
		.state = state,
		.last_seen_ns = now,
	};
	__u16 translated;

	rev_value->host_ip = host_ip;
	rev_value->state = state;
	rev_value->last_seen_ns = now;
	translated = select_translated_id(rev_key, rev_value, preferred_src, inserted_new);

	if (translated == 0)
		return -1;

	rev_key->dst_port = translated;
	next.translated_src = translated;
	if (bpf_map_update_elem(&snat_fwd_map, fwd_key, &next, BPF_ANY) < 0)
		return -1;

	remember_snat_reverse_tuple(rev_key, now);
	return translated;
}

static __always_inline int handle_snat_reverse_tcp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct tcphdr *tcph;
	struct snat_rev_key key = {};
	struct snat_rev_value *value;
	struct snat_fwd_key fwd_key = {};
	__u8 state;
	__u64 now;
	__u64 l3_off;
	__u64 l4_off;
	bool delete_after_rewrite;

	if (parse_tcp4(skb, &data, &data_end, &iph, &tcph, &l3_off, &l4_off) < 0)
		return 0;

	key.src_ip = bpf_ntohl(iph->saddr);
	key.dst_ip = bpf_ntohl(iph->daddr);
	key.src_port = bpf_ntohs(tcph->source);
	key.dst_port = bpf_ntohs(tcph->dest);
	key.proto = IPPROTO_TCP;
	key.flags = SNAT_ENTRY_REVERSE;
	value = bpf_map_lookup_elem(&snat_rev_map, &key);
	if (!value) {
		if (known_snat_reverse_tuple(&key))
			record_snat_tcp_reverse_miss(tcph);
		return 0;
	}
	state = snat_tcp_close_state(tcph, SNAT_FLOW_REPLY_CLOSING);
	now = bpf_ktime_get_ns();
	delete_after_rewrite = value->state == SNAT_FLOW_CLOSING && state == SNAT_FLOW_ACTIVE;
	remember_snat_reverse_tuple(&key, now);
	touch_snat_rev_value(value, now, state);

	fwd_key.src_ip = value->target_ip;
	fwd_key.dst_ip = key.src_ip;
	fwd_key.src_port = value->target_port;
	fwd_key.dst_port = key.src_port;
	fwd_key.proto = IPPROTO_TCP;
	touch_snat_fwd_mapping(&fwd_key, value->host_ip, key.dst_port, now, state);

	if (rewrite_tcp_dst(skb, iph, tcph, l3_off, l4_off, value->target_ip, value->target_port) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return 0;
	}

	if (delete_after_rewrite) {
		record_snat_tcp_full_close_delete(true);
		delete_snat_tcp_mapping(&fwd_key, &key);
	} else {
		mark_snat_tcp_mapping_close(&fwd_key, &key, now, state);
	}
	bump_stat(STAT_SNAT_REV_HIT);
	return 1;
}

static __always_inline int handle_snat_reverse_udp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	struct snat_rev_key key = {};
	struct snat_rev_value *value;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_udp4(skb, &data, &data_end, &iph, &udph, &l3_off, &l4_off) < 0)
		return 0;

	key.src_ip = bpf_ntohl(iph->saddr);
	key.dst_ip = bpf_ntohl(iph->daddr);
	key.src_port = bpf_ntohs(udph->source);
	key.dst_port = bpf_ntohs(udph->dest);
	key.proto = IPPROTO_UDP;
	key.flags = SNAT_ENTRY_REVERSE;
	value = bpf_map_lookup_elem(&snat_rev_map, &key);
	if (!value)
		return 0;
	touch_snat_rev_value(value, bpf_ktime_get_ns(), SNAT_FLOW_ACTIVE);

	if (rewrite_udp_dst(skb, iph, udph, l3_off, l4_off, value->target_ip, value->target_port) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_SNAT_REV_HIT);
	return 1;
}

static __always_inline int handle_snat_reverse_icmp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct icmphdr *icmph;
	struct snat_rev_key key = {};
	struct snat_rev_value *value;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_icmp4(skb, &data, &data_end, &iph, &icmph, &l3_off, &l4_off) < 0)
		return 0;
	if (icmph->type != ICMP_ECHOREPLY)
		return 0;

	key.src_ip = bpf_ntohl(iph->saddr);
	key.dst_ip = bpf_ntohl(iph->daddr);
	key.src_port = 0;
	key.dst_port = bpf_ntohs(icmph->un.echo.id);
	key.proto = IPPROTO_ICMP;
	key.flags = SNAT_ENTRY_REVERSE;
	value = bpf_map_lookup_elem(&snat_rev_map, &key);
	if (!value)
		return 0;
	touch_snat_rev_value(value, bpf_ktime_get_ns(), SNAT_FLOW_ACTIVE);

	if (rewrite_icmp_dst(skb, iph, icmph, l3_off, l4_off, value->target_ip, value->target_port) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_SNAT_REV_HIT);
	return 1;
}

static __always_inline int handle_snat_egress_tcp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct tcphdr *tcph;
	struct uplink_addr_key uplink_key = {};
	struct uplink_addr_value *uplink_addr;
	struct snat_fwd_key fwd_key = {};
	struct snat_fwd_value *current;
	struct snat_rev_key rev_key = {};
	struct snat_rev_value rev_value = {};
	struct config_value *cfg;
	__u32 src_ip;
	__u32 dst_ip;
	__u16 src_port;
	__u16 dst_port;
	__u64 l3_off;
	__u64 l4_off;
	__u64 now;
	__u8 state;
	__u8 inserted_new = 0;
	bool initial_syn;
	bool delete_after_rewrite = false;
	int translated;

	if (parse_tcp4(skb, &data, &data_end, &iph, &tcph, &l3_off, &l4_off) < 0)
		return TC_ACT_OK;

	src_ip = bpf_ntohl(iph->saddr);
	dst_ip = bpf_ntohl(iph->daddr);
	src_port = bpf_ntohs(tcph->source);
	dst_port = bpf_ntohs(tcph->dest);
	now = bpf_ktime_get_ns();
	state = snat_tcp_close_state(tcph, SNAT_FLOW_ORIG_CLOSING);
	initial_syn = snat_tcp_initial_syn(tcph);

	cfg = lookup_config();
	if (!in_sandbox_cidr_with_config(cfg, src_ip))
		return TC_ACT_OK;
	if (is_native_route_with_config(cfg, dst_ip)) {
		bump_stat(STAT_NATIVE_ROUTE_SKIP);
		return TC_ACT_OK;
	}

	uplink_key.ifindex = skb->ifindex;
	uplink_addr = bpf_map_lookup_elem(&uplink_addr_map, &uplink_key);
	if (!uplink_addr) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	fwd_key.src_ip = src_ip;
	fwd_key.dst_ip = dst_ip;
	fwd_key.src_port = src_port;
	fwd_key.dst_port = dst_port;
	fwd_key.proto = IPPROTO_TCP;

	current = bpf_map_lookup_elem(&snat_fwd_map, &fwd_key);
	if (current && current->host_ip == uplink_addr->addr &&
	    !(snat_flow_is_closing(current->state) && initial_syn)) {
		translated = current->translated_src;
		delete_after_rewrite = current->state == SNAT_FLOW_CLOSING && state == SNAT_FLOW_ACTIVE;
		touch_snat_fwd_value(current, now, state);
		rev_key.src_ip = dst_ip;
		rev_key.dst_ip = uplink_addr->addr;
		rev_key.src_port = dst_port;
		rev_key.dst_port = translated;
		rev_key.proto = IPPROTO_TCP;
		rev_key.flags = SNAT_ENTRY_REVERSE;
		touch_snat_rev_mapping(&rev_key, src_ip, src_port, uplink_addr->addr, now, state);
		bump_stat(STAT_SNAT_FWD_HIT);
	} else {
		rev_key.src_ip = dst_ip;
		rev_key.dst_ip = uplink_addr->addr;
		rev_key.src_port = dst_port;
		rev_key.dst_port = src_port;
		rev_key.proto = IPPROTO_TCP;
		rev_key.flags = SNAT_ENTRY_REVERSE;
		rev_value.target_ip = src_ip;
		rev_value.host_ip = uplink_addr->addr;
		rev_value.target_port = src_port;
		rev_value.translated_src = 0;
		if (!initial_syn) {
			record_snat_tcp_non_syn_miss(tcph, current ? SNAT_NON_SYN_MISS_FWD_HOST_MISMATCH : SNAT_NON_SYN_MISS_FWD_LOOKUP);
			bump_stat(STAT_SNAT_FALLBACK_HIT);
			return TC_ACT_OK;
		}
		translated = program_snat_mapping(&fwd_key, &rev_key, &rev_value, uplink_addr->addr, src_port, now, state, &inserted_new);
		if (translated < 0) {
			bump_stat(STAT_SNAT_FALLBACK_HIT);
			return TC_ACT_OK;
		}
		if (inserted_new)
			bump_stat(STAT_SNAT_MAPPING_PROGRAMMED);
	}

	if (rewrite_tcp_src(skb, iph, tcph, l3_off, l4_off, uplink_addr->addr, translated) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	if (delete_after_rewrite) {
		record_snat_tcp_full_close_delete(false);
		delete_snat_tcp_mapping(&fwd_key, &rev_key);
	} else {
		mark_snat_tcp_mapping_close(&fwd_key, &rev_key, now, state);
	}
	bump_stat(STAT_SNAT_HIT);
	return TC_ACT_OK;
}

static __always_inline int handle_snat_egress_udp_parsed(struct __sk_buff *skb,
							 struct iphdr *iph,
							 struct udphdr *udph,
							 __u64 l3_off,
							 __u64 l4_off)
{
	struct uplink_addr_key uplink_key = {};
	struct uplink_addr_value *uplink_addr;
	struct snat_rev_key rev_key = {};
	struct snat_rev_key alias_key = {};
	struct snat_rev_value rev_value = {};
	struct snat_rev_value *current;
	struct snat_rev_value *alias;
	struct snat_rev_value alias_value = {};
	struct config_value *cfg;
	__u32 src_ip;
	__u32 dst_ip;
	__u16 src_port;
	__u16 dst_port;
	__u64 now;
	__u8 inserted_new = 0;
	int translated;

	src_ip = bpf_ntohl(iph->saddr);
	dst_ip = bpf_ntohl(iph->daddr);
	src_port = bpf_ntohs(udph->source);
	dst_port = bpf_ntohs(udph->dest);
	now = bpf_ktime_get_ns();

	cfg = lookup_config();
	if (!in_sandbox_cidr_with_config(cfg, src_ip))
		return TC_ACT_OK;
	if (is_native_route_with_config(cfg, dst_ip)) {
		bump_stat(STAT_NATIVE_ROUTE_SKIP);
		return TC_ACT_OK;
	}

	uplink_key.ifindex = skb->ifindex;
	uplink_addr = bpf_map_lookup_elem(&uplink_addr_map, &uplink_key);
	if (!uplink_addr) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	rev_key.src_ip = dst_ip;
	rev_key.dst_ip = uplink_addr->addr;
	rev_key.src_port = dst_port;
	rev_key.dst_port = src_port;
	rev_key.proto = IPPROTO_UDP;
	rev_key.flags = SNAT_ENTRY_REVERSE;
	rev_value.target_ip = src_ip;
	rev_value.host_ip = uplink_addr->addr;
	rev_value.target_port = src_port;
	rev_value.translated_src = 0;
	rev_value.state = SNAT_FLOW_ACTIVE;
	rev_value.last_seen_ns = now;

	current = bpf_map_lookup_elem(&snat_rev_map, &rev_key);
	if (current &&
	    current->host_ip == uplink_addr->addr &&
	    current->target_ip == src_ip &&
	    current->target_port == src_port) {
		translated = src_port;
		touch_snat_rev_value(current, now, SNAT_FLOW_ACTIVE);
		bump_stat(STAT_SNAT_FWD_HIT);
	} else {
		alias_key.src_ip = src_ip;
		alias_key.dst_ip = dst_ip;
		alias_key.src_port = src_port;
		alias_key.dst_port = dst_port;
		alias_key.proto = IPPROTO_UDP;
		alias_key.flags = SNAT_ENTRY_ALIAS;
		alias = bpf_map_lookup_elem(&snat_rev_map, &alias_key);
		if (alias &&
		    alias->host_ip == uplink_addr->addr &&
		    alias->target_ip == src_ip &&
		    alias->target_port == src_port &&
		    alias->translated_src != 0) {
			translated = alias->translated_src;
			touch_snat_rev_value(alias, now, SNAT_FLOW_ACTIVE);
			rev_key.dst_port = translated;
			touch_snat_rev_mapping(&rev_key, src_ip, src_port, uplink_addr->addr, now, SNAT_FLOW_ACTIVE);
			bump_stat(STAT_SNAT_FWD_HIT);
		} else {
			translated = select_translated_id(&rev_key, &rev_value, src_port, &inserted_new);
			if (translated < 0) {
				bump_stat(STAT_SNAT_FALLBACK_HIT);
				return TC_ACT_OK;
			}
			if (translated == 0) {
				bump_stat(STAT_SNAT_FALLBACK_HIT);
				return TC_ACT_OK;
			}
			if (translated != src_port) {
				alias_value.target_ip = src_ip;
				alias_value.host_ip = uplink_addr->addr;
				alias_value.target_port = src_port;
				alias_value.translated_src = translated;
				alias_value.state = SNAT_FLOW_ACTIVE;
				alias_value.last_seen_ns = now;
				if (bpf_map_update_elem(&snat_rev_map, &alias_key, &alias_value, BPF_ANY) < 0) {
					bump_stat(STAT_SNAT_FALLBACK_HIT);
					return TC_ACT_OK;
				}
			}
			if (inserted_new)
				bump_stat(STAT_SNAT_MAPPING_PROGRAMMED);
		}
	}

	if (translated == src_port)
		bump_stat(STAT_SNAT_UDP_SAME_PORT_HIT);
	else
		bump_stat(STAT_SNAT_UDP_PORT_REWRITE_HIT);
	if (udph->check != 0)
		bump_stat(STAT_SNAT_UDP_CHECKSUM_PRESENT_HIT);

	if (rewrite_udp_src(skb, iph, udph, l3_off, l4_off, uplink_addr->addr, translated) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	bump_stat(STAT_SNAT_HIT);
	return TC_ACT_OK;
}

static __always_inline int handle_snat_egress_udp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_udp4(skb, &data, &data_end, &iph, &udph, &l3_off, &l4_off) < 0)
		return TC_ACT_OK;

	return handle_snat_egress_udp_parsed(skb, iph, udph, l3_off, l4_off);
}

static __always_inline int handle_snat_egress_icmp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct icmphdr *icmph;
	struct uplink_addr_key uplink_key = {};
	struct uplink_addr_value *uplink_addr;
	struct snat_fwd_key fwd_key = {};
	struct snat_fwd_value *current;
	struct snat_rev_key rev_key = {};
	struct snat_rev_value rev_value = {};
	struct config_value *cfg;
	__u32 src_ip;
	__u32 dst_ip;
	__u16 echo_id;
	__u64 l3_off;
	__u64 l4_off;
	__u64 now;
	__u8 inserted_new = 0;
	int translated;

	if (parse_icmp4(skb, &data, &data_end, &iph, &icmph, &l3_off, &l4_off) < 0)
		return TC_ACT_OK;
	if (icmph->type != ICMP_ECHO)
		return TC_ACT_OK;

	src_ip = bpf_ntohl(iph->saddr);
	dst_ip = bpf_ntohl(iph->daddr);
	echo_id = bpf_ntohs(icmph->un.echo.id);
	now = bpf_ktime_get_ns();

	cfg = lookup_config();
	if (!in_sandbox_cidr_with_config(cfg, src_ip))
		return TC_ACT_OK;
	if (is_native_route_with_config(cfg, dst_ip)) {
		bump_stat(STAT_NATIVE_ROUTE_SKIP);
		return TC_ACT_OK;
	}

	uplink_key.ifindex = skb->ifindex;
	uplink_addr = bpf_map_lookup_elem(&uplink_addr_map, &uplink_key);
	if (!uplink_addr) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	fwd_key.src_ip = src_ip;
	fwd_key.dst_ip = dst_ip;
	fwd_key.src_port = echo_id;
	fwd_key.dst_port = 0;
	fwd_key.proto = IPPROTO_ICMP;

	current = bpf_map_lookup_elem(&snat_fwd_map, &fwd_key);
	if (current && current->host_ip == uplink_addr->addr) {
		translated = current->translated_src;
		touch_snat_fwd_value(current, now, SNAT_FLOW_ACTIVE);
		rev_key.src_ip = dst_ip;
		rev_key.dst_ip = uplink_addr->addr;
		rev_key.src_port = 0;
		rev_key.dst_port = translated;
		rev_key.proto = IPPROTO_ICMP;
		rev_key.flags = SNAT_ENTRY_REVERSE;
		touch_snat_rev_mapping(&rev_key, src_ip, echo_id, uplink_addr->addr, now, SNAT_FLOW_ACTIVE);
		bump_stat(STAT_SNAT_FWD_HIT);
	} else {
		rev_key.src_ip = dst_ip;
		rev_key.dst_ip = uplink_addr->addr;
		rev_key.src_port = 0;
		rev_key.dst_port = echo_id;
		rev_key.proto = IPPROTO_ICMP;
		rev_key.flags = SNAT_ENTRY_REVERSE;
		rev_value.target_ip = src_ip;
		rev_value.host_ip = uplink_addr->addr;
		rev_value.target_port = echo_id;
		rev_value.translated_src = 0;
		translated = program_snat_mapping(&fwd_key, &rev_key, &rev_value, uplink_addr->addr, echo_id, now, SNAT_FLOW_ACTIVE, &inserted_new);
		if (translated < 0) {
			bump_stat(STAT_SNAT_FALLBACK_HIT);
			return TC_ACT_OK;
		}
		if (inserted_new)
			bump_stat(STAT_SNAT_MAPPING_PROGRAMMED);
	}

	if (rewrite_icmp_src(skb, iph, icmph, l3_off, l4_off, uplink_addr->addr, translated) < 0) {
		bump_stat(STAT_SNAT_FALLBACK_HIT);
		return TC_ACT_OK;
	}

	bump_stat(STAT_SNAT_HIT);
	return TC_ACT_OK;
}

#endif
