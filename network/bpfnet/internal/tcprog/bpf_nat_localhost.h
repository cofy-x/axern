#ifndef BPF_NAT_LOCALHOST_H
#define BPF_NAT_LOCALHOST_H

static __always_inline bool is_local_ipv4(__u32 addr)
{
	struct local_addr_key key = {
		.addr = addr,
	};

	return bpf_map_lookup_elem(&local_addr_map, &key) != 0;
}

static __always_inline bool is_host_netns(void *ctx)
{
	__u32 key = 0;
	__u64 *host_cookie = bpf_map_lookup_elem(&host_netns_cookie_map, &key);

	if (!host_cookie || *host_cookie == 0)
		return false;
	return bpf_get_netns_cookie(ctx) == *host_cookie;
}

static __always_inline int handle_localhost_connect4(struct bpf_sock_addr *ctx)
{
	struct service_key svc_key = {};
	struct service_value *svc_value;
	struct localhost_sock_key sock_key = {};
	struct localhost_sock_value sock_value = {};
	__u32 host_ip;
	__u16 host_port;

	if (ctx->user_family != AF_INET || ctx->family != AF_INET)
		return 1;
	if (ctx->type != SOCK_STREAM)
		return 1;
	if (!is_host_netns(ctx))
		return 1;

	host_ip = bpf_ntohl(ctx->user_ip4);
	if (!is_local_ipv4(host_ip))
		return 1;

	host_port = bpf_ntohs((__u16)ctx->user_port);
	svc_key.proto = IPPROTO_TCP;
	svc_key.host_port = host_port;
	svc_value = bpf_map_lookup_elem(&service_map, &svc_key);
	if (!svc_value) {
		bump_stat(STAT_LOCALHOST_FALLBACK_HIT);
		return 1;
	}

	sock_key.cookie = bpf_get_socket_cookie(ctx);
	if (!sock_key.cookie) {
		bump_stat(STAT_LOCALHOST_FALLBACK_HIT);
		return 1;
	}

	sock_value.host_ip = host_ip;
	sock_value.host_port = host_port;
	if (bpf_map_update_elem(&localhost_sock_map, &sock_key, &sock_value, BPF_ANY) < 0) {
		bump_stat(STAT_LOCALHOST_FALLBACK_HIT);
		return 1;
	}

	ctx->user_ip4 = bpf_htonl(svc_value->target_ip);
	ctx->user_port = bpf_htons(svc_value->target_port);

	bump_stat(STAT_LOCALHOST_CONNECT_HIT);
	return 1;
}

static __always_inline int handle_localhost_getpeername4(struct bpf_sock_addr *ctx)
{
	struct localhost_sock_key sock_key = {};
	struct localhost_sock_value *sock_value;

	if (ctx->user_family != AF_INET || ctx->family != AF_INET)
		return 1;
	if (ctx->type != SOCK_STREAM)
		return 1;
	if (!is_host_netns(ctx))
		return 1;

	sock_key.cookie = bpf_get_socket_cookie(ctx);
	if (!sock_key.cookie)
		return 1;

	sock_value = bpf_map_lookup_elem(&localhost_sock_map, &sock_key);
	if (!sock_value)
		return 1;

	ctx->user_ip4 = bpf_htonl(sock_value->host_ip);
	ctx->user_port = bpf_htons(sock_value->host_port);

	bump_stat(STAT_LOCALHOST_GETPEER_HIT);
	return 1;
}

static __always_inline int handle_localhost_sock_release(struct bpf_sock *ctx)
{
	struct localhost_sock_key sock_key = {
		.cookie = bpf_get_socket_cookie(ctx),
	};

	if (sock_key.cookie)
		bpf_map_delete_elem(&localhost_sock_map, &sock_key);
	return 1;
}

#endif
