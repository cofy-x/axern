//go:build ignore

#include <linux/bpf.h>
#include <linux/errno.h>
#include <linux/if_ether.h>
#include <linux/icmp.h>
#include <linux/in.h>
#include <linux/ip.h>
#include <linux/pkt_cls.h>
#include <linux/socket.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <stdbool.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

#include "bpf_nat_maps.h"
#include "bpf_nat_parse.h"
#include "bpf_nat_rewrite.h"
#include "bpf_nat_snat.h"
#include "bpf_nat_ingress.h"
#include "bpf_nat_egress.h"
#include "bpf_nat_localhost.h"

SEC("tc")
int dataplane_ingress(struct __sk_buff *skb)
{
	if (handle_snat_reverse_tcp(skb))
		return TC_ACT_OK;
	if (handle_snat_reverse_udp(skb))
		return TC_ACT_OK;
	if (handle_snat_reverse_icmp(skb))
		return TC_ACT_OK;

	if (handle_service_ingress_tcp(skb))
		return TC_ACT_OK;
	if (handle_service_ingress_udp(skb))
		return TC_ACT_OK;

	return TC_ACT_OK;
}

SEC("tc")
int dataplane_egress(struct __sk_buff *skb)
{
	void *data;
	void *data_end;
	struct iphdr *iph;
	struct udphdr *udph;
	__u64 l3_off;
	__u64 l4_off;

	if (handle_rev_nat_egress_tcp(skb))
		return TC_ACT_OK;
	if (handle_rev_nat_egress_udp(skb))
		return TC_ACT_OK;

	if (parse_ipv4(skb, &data, &data_end, &iph, &l3_off, &l4_off) < 0)
		return TC_ACT_OK;

	switch (iph->protocol) {
	case IPPROTO_TCP:
		handle_snat_egress_tcp(skb);
		break;
	case IPPROTO_UDP:
		if (load_udp4_from_ipv4(skb, l3_off, l4_off, &data, &data_end, &iph, &udph) == 0)
			handle_snat_egress_udp_parsed(skb, iph, udph, l3_off, l4_off);
		break;
	case IPPROTO_ICMP:
		handle_snat_egress_icmp(skb);
		break;
	default:
		break;
	}

	return TC_ACT_OK;
}

SEC("cgroup/connect4")
int localhost_connect4(struct bpf_sock_addr *ctx)
{
	return handle_localhost_connect4(ctx);
}

SEC("cgroup/getpeername4")
int localhost_getpeername4(struct bpf_sock_addr *ctx)
{
	return handle_localhost_getpeername4(ctx);
}

SEC("cgroup/sock_release")
int localhost_sock_release(struct bpf_sock *ctx)
{
	return handle_localhost_sock_release(ctx);
}

char __license[] SEC("license") = "Dual BSD/GPL";
