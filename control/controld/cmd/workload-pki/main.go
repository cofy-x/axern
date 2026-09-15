// workload-pki provisions deployment-owned authority. It never creates Node
// certificates or admits Nodes; their keys originate on the admitted node.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
)

func main() {
	var directory, cluster, dns, ips string
	var renew bool
	flag.StringVar(&directory, "directory", "", "private PKI output directory (back up before rotation)")
	flag.StringVar(&cluster, "cluster", "", "workload URI trust domain")
	flag.StringVar(&dns, "dns", "localhost,host.docker.internal", "comma-separated service DNS names")
	flag.StringVar(&ips, "ips", "127.0.0.1", "comma-separated service IP addresses")
	flag.BoolVar(&renew, "renew-services", false, "explicitly renew service leaves without replacing CA or administrator identity")
	flag.Parse()
	var addresses []net.IP
	for _, value := range strings.Split(ips, ",") {
		if value == "" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			fmt.Fprintln(os.Stderr, "invalid service IP address")
			os.Exit(1)
		}
		addresses = append(addresses, ip)
	}
	err := (workloadtls.Bootstrap{Directory: directory, Cluster: cluster, DNSNames: strings.Split(dns, ","), IPAddresses: addresses}).Ensure(renew)
	if err != nil {
		fmt.Fprintln(os.Stderr, "workload-pki:", err)
		os.Exit(1)
	}
	fmt.Println("workload PKI ready; signer remains control-only")
}
