package admin

import (
	"fmt"
	"net"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"github.com/spf13/cobra"
)

// PKI provisioning is local operator work: it neither opens a control-plane
// session nor admits a Node. The same bootstrap is used by local deployments.
func pkiCommand() *cobra.Command {
	root := &cobra.Command{Use: "pki", Short: "Provision local deployment identity material"}
	var directory, cluster string
	var dns, ips []string
	var renew bool
	bootstrap := &cobra.Command{Use: "bootstrap", Args: command.NoArgs, Short: "Initialize authority or explicitly renew service certificates", RunE: func(cmd *cobra.Command, _ []string) error {
		var addresses []net.IP
		for _, value := range ips {
			address := net.ParseIP(value)
			if address == nil {
				return command.Usage(fmt.Errorf("invalid IP address %q", value))
			}
			addresses = append(addresses, address)
		}
		if directory == "" || cluster == "" {
			return command.Usage(fmt.Errorf("--directory and --cluster are required"))
		}
		if err := (workloadtls.Bootstrap{Directory: directory, Cluster: cluster, DNSNames: dns, IPAddresses: addresses}).Ensure(renew); err != nil {
			return err
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Workload PKI ready; keep private/signer.pem control-only.")
		return err
	}}
	flags := bootstrap.Flags()
	flags.StringVar(&directory, "directory", "", "private output directory; back up its authority")
	flags.StringVar(&cluster, "cluster", "", "workload URI trust domain")
	flags.StringSliceVar(&dns, "dns", []string{"localhost"}, "service DNS names")
	flags.StringSliceVar(&ips, "ips", []string{"127.0.0.1"}, "service IP addresses")
	flags.BoolVar(&renew, "renew-services", false, "renew service leaves without replacing CA or administrator")
	root.AddCommand(bootstrap)
	return root
}
