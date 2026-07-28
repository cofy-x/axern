#ifndef BPF_NAT_EGRESS_H
#define BPF_NAT_EGRESS_H

static __always_inline int handle_rev_nat_egress_tcp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct tcphdr *tcph;
	struct rev_nat_key key = {};
	struct rev_nat_value *value;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_tcp4(skb, &data, &data_end, &iph, &tcph, &l3_off, &l4_off) < 0)
		return 0;

	key.src_ip = bpf_ntohl(iph->saddr);
	key.dst_ip = bpf_ntohl(iph->daddr);
	key.src_port = bpf_ntohs(tcph->source);
	key.dst_port = bpf_ntohs(tcph->dest);
	key.proto = IPPROTO_TCP;
	value = bpf_map_lookup_elem(&rev_nat_map, &key);
	if (!value)
		return 0;

	if (rewrite_tcp_src(skb, iph, tcph, l3_off, l4_off, value->host_ip, value->host_port) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_REV_NAT_HIT);
	return 1;
}

static __always_inline int handle_rev_nat_egress_udp(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	struct rev_nat_key key = {};
	struct rev_nat_value *value;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_udp4(skb, &data, &data_end, &iph, &udph, &l3_off, &l4_off) < 0)
		return 0;

	key.src_ip = bpf_ntohl(iph->saddr);
	key.dst_ip = bpf_ntohl(iph->daddr);
	key.src_port = bpf_ntohs(udph->source);
	key.dst_port = bpf_ntohs(udph->dest);
	key.proto = IPPROTO_UDP;
	value = bpf_map_lookup_elem(&rev_nat_map, &key);
	if (!value)
		return 0;

	if (rewrite_udp_src(skb, iph, udph, l3_off, l4_off, value->host_ip, value->host_port) < 0) {
		bump_stat(STAT_FALLBACK_HIT);
		return 0;
	}

	bump_stat(STAT_REV_NAT_HIT);
	return 1;
}

#endif
