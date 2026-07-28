#ifndef BPF_NAT_REWRITE_H
#define BPF_NAT_REWRITE_H

static __always_inline int update_ipv4_addr(struct __sk_buff *skb, __u64 l3_off,
					    __u64 l4_off, __u8 proto, bool source,
					    __be32 old_addr, __be32 new_addr, __u16 l4_check)
{
	__u64 addr_off = l3_off + (source ? offsetof(struct iphdr, saddr) : offsetof(struct iphdr, daddr));

	if (old_addr == new_addr)
		return 0;

	if (bpf_l3_csum_replace(skb, l3_off + offsetof(struct iphdr, check), old_addr, new_addr, sizeof(new_addr)) < 0)
		return -1;

	if ((proto == IPPROTO_TCP || proto == IPPROTO_UDP) && l4_check != 0) {
		if (bpf_l4_csum_replace(skb, l4_off + l4_check, old_addr, new_addr,
					sizeof(new_addr) | BPF_F_PSEUDO_HDR) < 0)
			return -1;
	}

	if (bpf_skb_store_bytes(skb, addr_off, &new_addr, sizeof(new_addr), 0) < 0)
		return -1;

	return 0;
}

static __always_inline int rewrite_tcp_dst(struct __sk_buff *skb, struct iphdr *iph, struct tcphdr *tcph,
					   __u64 l3_off, __u64 l4_off,
					   __u32 target_ip, __u16 target_port)
{
	__be32 old_addr = iph->daddr;
	__be32 new_addr = bpf_htonl(target_ip);
	__be16 old_port = tcph->dest;
	__be16 new_port = bpf_htons(target_port);

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_TCP, false, old_addr, new_addr,
			     offsetof(struct tcphdr, check)) < 0)
		return -1;

	if (old_port != new_port) {
		if (bpf_l4_csum_replace(skb, l4_off + offsetof(struct tcphdr, check), old_port, new_port, sizeof(new_port)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct tcphdr, dest), &new_port, sizeof(new_port), 0) < 0)
			return -1;
	}

	return 0;
}

static __always_inline int rewrite_tcp_src(struct __sk_buff *skb, struct iphdr *iph, struct tcphdr *tcph,
					   __u64 l3_off, __u64 l4_off,
					   __u32 host_ip, __u16 host_port)
{
	__be32 old_addr = iph->saddr;
	__be32 new_addr = bpf_htonl(host_ip);
	__be16 old_port = tcph->source;
	__be16 new_port = bpf_htons(host_port);

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_TCP, true, old_addr, new_addr,
			     offsetof(struct tcphdr, check)) < 0)
		return -1;

	if (old_port != new_port) {
		if (bpf_l4_csum_replace(skb, l4_off + offsetof(struct tcphdr, check), old_port, new_port, sizeof(new_port)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct tcphdr, source), &new_port, sizeof(new_port), 0) < 0)
			return -1;
	}

	return 0;
}

static __always_inline int rewrite_udp_dst(struct __sk_buff *skb, struct iphdr *iph, struct udphdr *udph,
					   __u64 l3_off, __u64 l4_off,
					   __u32 target_ip, __u16 target_port)
{
	__be32 old_addr = iph->daddr;
	__be32 new_addr = bpf_htonl(target_ip);
	__be16 old_port = udph->dest;
	__be16 new_port = bpf_htons(target_port);
	__be16 udp_check = udph->check;
	__u16 l4_check = udp_check != 0 ? offsetof(struct udphdr, check) : 0;

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_UDP, false, old_addr, new_addr, l4_check) < 0)
		return -1;

	if (old_port != new_port) {
		if (udp_check != 0 &&
		    bpf_l4_csum_replace(skb, l4_off + offsetof(struct udphdr, check), old_port, new_port, sizeof(new_port)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct udphdr, dest), &new_port, sizeof(new_port), 0) < 0)
			return -1;
	}

	return 0;
}

static __always_inline int rewrite_udp_src(struct __sk_buff *skb, struct iphdr *iph, struct udphdr *udph,
					   __u64 l3_off, __u64 l4_off,
					   __u32 host_ip, __u16 host_port)
{
	__be32 old_addr = iph->saddr;
	__be32 new_addr = bpf_htonl(host_ip);
	__be16 old_port = udph->source;
	__be16 new_port = bpf_htons(host_port);
	__be16 udp_check = udph->check;
	__u16 l4_check = udp_check != 0 ? offsetof(struct udphdr, check) : 0;

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_UDP, true, old_addr, new_addr, l4_check) < 0)
		return -1;

	if (old_port != new_port) {
		if (udp_check != 0 &&
		    bpf_l4_csum_replace(skb, l4_off + offsetof(struct udphdr, check), old_port, new_port, sizeof(new_port)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct udphdr, source), &new_port, sizeof(new_port), 0) < 0)
			return -1;
	}

	return 0;
}

static __always_inline int rewrite_icmp_dst(struct __sk_buff *skb, struct iphdr *iph, struct icmphdr *icmph,
					    __u64 l3_off, __u64 l4_off,
					    __u32 target_ip, __u16 target_id)
{
	__be32 old_addr = iph->daddr;
	__be32 new_addr = bpf_htonl(target_ip);
	__be16 old_id = icmph->un.echo.id;
	__be16 new_id = bpf_htons(target_id);

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_ICMP, false, old_addr, new_addr, 0) < 0)
		return -1;

	if (old_id != new_id) {
		if (bpf_l4_csum_replace(skb, l4_off + offsetof(struct icmphdr, checksum), old_id, new_id, sizeof(new_id)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct icmphdr, un.echo.id), &new_id, sizeof(new_id), 0) < 0)
			return -1;
	}

	return 0;
}

static __always_inline int rewrite_icmp_src(struct __sk_buff *skb, struct iphdr *iph, struct icmphdr *icmph,
					    __u64 l3_off, __u64 l4_off,
					    __u32 host_ip, __u16 host_id)
{
	__be32 old_addr = iph->saddr;
	__be32 new_addr = bpf_htonl(host_ip);
	__be16 old_id = icmph->un.echo.id;
	__be16 new_id = bpf_htons(host_id);

	if (update_ipv4_addr(skb, l3_off, l4_off, IPPROTO_ICMP, true, old_addr, new_addr, 0) < 0)
		return -1;

	if (old_id != new_id) {
		if (bpf_l4_csum_replace(skb, l4_off + offsetof(struct icmphdr, checksum), old_id, new_id, sizeof(new_id)) < 0)
			return -1;
		if (bpf_skb_store_bytes(skb, l4_off + offsetof(struct icmphdr, un.echo.id), &new_id, sizeof(new_id), 0) < 0)
			return -1;
	}

	return 0;
}

#endif
