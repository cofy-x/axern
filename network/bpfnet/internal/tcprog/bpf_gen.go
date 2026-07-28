package tcprog

//go:generate bpf2go -cc clang -target bpfel,bpfeb -type service_key -type service_value -type local_addr_key -type local_addr_value -type rev_nat_key -type rev_nat_value -type config_value -type uplink_addr_key -type uplink_addr_value -type native_route_key -type native_route_value -type snat_fwd_key -type snat_fwd_value -type snat_rev_key -type snat_rev_value -type localhost_sock_key -type localhost_sock_value Dataplane ./bpf_nat.c -- -O2 -g -Wall -Werror -I/usr/include/aarch64-linux-gnu -I/usr/include/x86_64-linux-gnu
