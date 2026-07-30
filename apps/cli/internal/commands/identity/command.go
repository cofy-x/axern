package identity

import (
	"fmt"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "identity", Short: "Inspect the authenticated Principal"}
	root.AddCommand(&cobra.Command{Use: "whoami", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.Identity.WhoAmI(s.Context, &identityv1.WhoAmIRequest{})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintIdentityJSON(cmd.OutOrStdout(), resp)
		}
		p := resp.GetPrincipal()
		c := resp.GetCredential()
		fmt.Fprintf(cmd.OutOrStdout(), "Principal:  %s (%s)\nKind:       %s\nCredential: %s\nExpires:    %s\n", p.GetName(), p.GetPrincipalID(), p.GetKind(), c.GetLabel(), c.GetCertificateNotAfter().AsTime().Format("2006-01-02T15:04:05Z"))
		for _, role := range resp.GetRoles() {
			scope := role.GetScopeType()
			if role.GetNamespace() != "" {
				scope += "/" + role.GetNamespace()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Role:       %s @ %s\n", role.GetRole(), scope)
		}
		return nil
	}})
	return root
}
