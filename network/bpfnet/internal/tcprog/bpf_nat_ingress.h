#ifndef BPF_NAT_INGRESS_H
#define BPF_NAT_INGRESS_H

static __always_inline int handle_service_ingress_tcp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct tcphdr *tcph;
	struct local_addr_key local_key = {};
	struct local_addr_value *local_value;
	struct service_key svc_key = {};
	struct service_value *svc_value;
	struct rev_nat_key rev_key = {};
	struct rev_nat_value rev_value = {};
	__u64 l3_off;
	__u64 l4_off;

	if (parse_tcp4(skb, &data, &data_end, &iph, &tcph, &l3_off, &l4_off) < 0)
		return 0;

	local_key.addr = bpf_ntohl(iph->daddr);
	local_value = bpf_map_lookup_elem(&local_addr_map, &local_key);
	if (!local_value)
		return 0;

	svc_key.proto = IPPROTO_TCP;
	svc_key.host_port = bpf_ntohs(tcph->dest);
	svc_value = bpf_map_lookup_elem(&service_map, &svc_key);
	if (!svc_value) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	rev_key.src_ip = svc_value->target_ip;
	rev_key.dst_ip = bpf_ntohl(iph->saddr);
	rev_key.src_port = svc_value->target_port;
	rev_key.dst_port = bpf_ntohs(tcph->source);
	rev_key.proto = IPPROTO_TCP;
	rev_value.host_ip = local_key.addr;
	rev_value.host_port = svc_key.host_port;

	if (bpf_map_update_elem(&rev_nat_map, &rev_key, &rev_value, BPF_ANY) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	if (rewrite_tcp_dst(skb, iph, tcph, l3_off, l4_off, svc_value->target_ip, svc_value->target_port) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_SERVICE_HIT);
	return 1;
}

static __always_inline int handle_service_ingress_udp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	struct local_addr_key local_key = {};
	struct local_addr_value *local_value;
	struct service_key svc_key = {};
	struct service_value *svc_value;
	struct rev_nat_key rev_key = {};
	struct rev_nat_value rev_value = {};
	__u64 l3_off;
	__u64 l4_off;

	if (parse_udp4(skb, &data, &data_end, &iph, &udph, &l3_off, &l4_off) < 0)
		return 0;

	local_key.addr = bpf_ntohl(iph->daddr);
	local_value = bpf_map_lookup_elem(&local_addr_map, &local_key);
	if (!local_value)
		return 0;

	svc_key.proto = IPPROTO_UDP;
	svc_key.host_port = bpf_ntohs(udph->dest);
	svc_value = bpf_map_lookup_elem(&service_map, &svc_key);
	if (!svc_value) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	rev_key.src_ip = svc_value->target_ip;
	rev_key.dst_ip = bpf_ntohl(iph->saddr);
	rev_key.src_port = svc_value->target_port;
	rev_key.dst_port = bpf_ntohs(udph->source);
	rev_key.proto = IPPROTO_UDP;
	rev_value.host_ip = local_key.addr;
	rev_value.host_port = svc_key.host_port;

	if (bpf_map_update_elem(&rev_nat_map, &rev_key, &rev_value, BPF_ANY) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	if (rewrite_udp_dst(skb, iph, udph, l3_off, l4_off, svc_value->target_ip, svc_value->target_port) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_SERVICE_HIT);
	return 1;
}

#endif
