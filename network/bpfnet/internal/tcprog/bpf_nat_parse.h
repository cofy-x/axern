#ifndef BPF_NAT_PARSE_H
#define BPF_NAT_PARSE_H

static __always_inline int parse_ipv4(struct __sk_buff *skb, void **data_out, void **data_end_out,
				      struct iphdr **iph_out, __u64 *l3_off_out, __u64 *l4_off_out)
{
	void *data;
	void *data_end;
	struct ethhdr *eth;
	struct iphdr *iph;
	__u64 l3_off = sizeof(*eth);
	__u64 l4_off;

	if (bpf_skb_pull_data(skb, sizeof(struct ethhdr) + sizeof(struct iphdr)) < 0)
		return -1;

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;

	eth = data;
	if ((void *)(eth + 1) > data_end)
		return -1;
	if (eth->h_proto != bpf_htons(ETH_P_IP))
		return -1;

	iph = data + l3_off;
	if ((void *)(iph + 1) > data_end)
		return -1;
	if (iph->ihl < 5)
		return -1;

	l4_off = l3_off + ((__u64)iph->ihl * 4);
	if (data + l4_off > data_end)
		return -1;

	*data_out = data;
	*data_end_out = data_end;
	*iph_out = iph;
	*l3_off_out = l3_off;
	*l4_off_out = l4_off;
	return 0;
}

static __always_inline int parse_tcp4(struct __sk_buff *skb, void **data_out, void **data_end_out,
				      struct iphdr **iph_out, struct tcphdr **tcph_out,
				      __u64 *l3_off_out, __u64 *l4_off_out)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct tcphdr *tcph;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_ipv4(skb, &data, &data_end, &iph, &l3_off, &l4_off) < 0)
		return -1;
	if (iph->protocol != IPPROTO_TCP)
		return -1;
	if (bpf_skb_pull_data(skb, l4_off + sizeof(struct tcphdr)) < 0)
		return -1;

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;
	iph = data + l3_off;
	if ((void *)(iph + 1) > data_end)
		return -1;
	tcph = data + l4_off;
	if ((void *)(tcph + 1) > data_end)
		return -1;

	*data_out = data;
	*data_end_out = data_end;
	*iph_out = iph;
	*tcph_out = tcph;
	*l3_off_out = l3_off;
	*l4_off_out = l4_off;
	return 0;
}

static __always_inline int parse_udp4(struct __sk_buff *skb, void **data_out, void **data_end_out,
				      struct iphdr **iph_out, struct udphdr **udph_out,
				      __u64 *l3_off_out, __u64 *l4_off_out)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_ipv4(skb, &data, &data_end, &iph, &l3_off, &l4_off) < 0)
		return -1;
	if (iph->protocol != IPPROTO_UDP)
		return -1;
	if (bpf_skb_pull_data(skb, l4_off + sizeof(struct udphdr)) < 0)
		return -1;

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;
	iph = data + l3_off;
	if ((void *)(iph + 1) > data_end)
		return -1;
	udph = data + l4_off;
	if ((void *)(udph + 1) > data_end)
		return -1;

	*data_out = data;
	*data_end_out = data_end;
	*iph_out = iph;
	*udph_out = udph;
	*l3_off_out = l3_off;
	*l4_off_out = l4_off;
	return 0;
}

static __always_inline int load_udp4_from_ipv4(struct __sk_buff *skb, __u64 l3_off, __u64 l4_off,
					       void **data_out, void **data_end_out,
					       struct iphdr **iph_out, struct udphdr **udph_out)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;

	if (bpf_skb_pull_data(skb, l4_off + sizeof(struct udphdr)) < 0)
		return -1;

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;
	iph = data + l3_off;
	if ((void *)(iph + 1) > data_end)
		return -1;
	if (iph->protocol != IPPROTO_UDP)
		return -1;
	udph = data + l4_off;
	if ((void *)(udph + 1) > data_end)
		return -1;

	*data_out = data;
	*data_end_out = data_end;
	*iph_out = iph;
	*udph_out = udph;
	return 0;
}

static __always_inline int parse_icmp4(struct __sk_buff *skb, void **data_out, void **data_end_out,
				       struct iphdr **iph_out, struct icmphdr **icmph_out,
				       __u64 *l3_off_out, __u64 *l4_off_out)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct icmphdr *icmph;
	__u64 l3_off;
	__u64 l4_off;

	if (parse_ipv4(skb, &data, &data_end, &iph, &l3_off, &l4_off) < 0)
		return -1;
	if (iph->protocol != IPPROTO_ICMP)
		return -1;
	if (bpf_skb_pull_data(skb, l4_off + sizeof(struct icmphdr)) < 0)
		return -1;

	data = (void *)(long)skb->data;
	data_end = (void *)(long)skb->data_end;
	iph = data + l3_off;
	if ((void *)(iph + 1) > data_end)
		return -1;
	icmph = data + l4_off;
	if ((void *)(icmph + 1) > data_end)
		return -1;

	*data_out = data;
	*data_end_out = data_end;
	*iph_out = iph;
	*icmph_out = icmph;
	*l3_off_out = l3_off;
	*l4_off_out = l4_off;
	return 0;
}

#endif
