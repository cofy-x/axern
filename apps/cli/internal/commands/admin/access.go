package admin

import (
	"fmt"
	"strings"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"github.com/spf13/cobra"
)

func principalCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "principal", Short: "Manage durable platform Principals"}
	var displayName, kind string
	create := &cobra.Command{Use: "create <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		parsed, err := appadmin.PrincipalKind(kind)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.CreatePrincipal(s.Context, &adminv1.CreatePrincipalRequest{Name: args[0], DisplayName: displayName, Kind: parsed})
		if err != nil {
			return err
		}
		return renderPrincipal(cmd, runtime, resp.GetPrincipal())
	}}
	create.Flags().StringVar(&displayName, "display-name", "", "human-readable display name")
	create.Flags().StringVar(&kind, "kind", "human", "human or service")
	_ = create.MarkFlagRequired("display-name")
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.ListPrincipals(s.Context, &adminv1.ListPrincipalsRequest{})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintPrincipalListJSON(cmd.OutOrStdout(), resp.GetPrincipals())
		}
		fmt.Fprintln(cmd.OutOrStdout(), "NAME\tKIND\tSTATUS\tPRINCIPAL ID")
		for _, p := range resp.GetPrincipals() {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", p.GetName(), enumName(p.GetKind().String()), enumName(p.GetStatus().String()), p.GetPrincipalID())
		}
		return nil
	}}
	disable := &cobra.Command{Use: "disable <principal-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.DisablePrincipal(s.Context, &adminv1.DisablePrincipalRequest{PrincipalID: args[0]})
		if err != nil {
			return err
		}
		return renderPrincipal(cmd, runtime, resp.GetPrincipal())
	}}
	root.AddCommand(create, list, disable)
	return root
}

func credentialCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "credential", Short: "Manage Principal X.509 credentials"}
	var certificate, label string
	add := &cobra.Command{Use: "add <principal-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewAccess(s.Clients.AccessAdmin).AddCredential(s.Context, args[0], certificate, label)
		if err != nil {
			return err
		}
		return renderCredential(cmd, runtime, resp.GetCredential())
	}}
	add.Flags().StringVar(&certificate, "certificate", "", "public certificate PEM path")
	add.Flags().StringVar(&label, "label", "", "credential label")
	_ = add.MarkFlagRequired("certificate")
	_ = add.MarkFlagRequired("label")
	var principalID string
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.ListPrincipalCredentials(s.Context, &adminv1.ListPrincipalCredentialsRequest{PrincipalID: principalID})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintPrincipalCredentialListJSON(cmd.OutOrStdout(), resp.GetCredentials())
		}
		fmt.Fprintln(cmd.OutOrStdout(), "LABEL\tEXPIRES\tREVOKED\tCREDENTIAL ID")
		for _, c := range resp.GetCredentials() {
			revoked := ""
			if c.GetRevokedAt() != nil {
				revoked = c.GetRevokedAt().AsTime().Format("2006-01-02T15:04:05Z")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", c.GetLabel(), c.GetCertificateNotAfter().AsTime().Format("2006-01-02T15:04:05Z"), revoked, c.GetCredentialID())
		}
		return nil
	}}
	list.Flags().StringVar(&principalID, "principal-id", "", "Principal ID")
	_ = list.MarkFlagRequired("principal-id")
	revoke := &cobra.Command{Use: "revoke <credential-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.RevokePrincipalCredential(s.Context, &adminv1.RevokePrincipalCredentialRequest{CredentialID: args[0]})
		if err != nil {
			return err
		}
		return renderCredential(cmd, runtime, resp.GetCredential())
	}}
	root.AddCommand(add, list, revoke)
	return root
}

func roleBindingCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "role-binding", Short: "Manage Principal role bindings"}
	var principalID, scope, namespace, role string
	grant := &cobra.Command{Use: "grant", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		sType, rType, err := appadmin.ScopeAndRole(scope, namespace, role)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.GrantRoleBinding(s.Context, &adminv1.GrantRoleBindingRequest{PrincipalID: principalID, ScopeType: sType, Namespace: namespace, Role: rType})
		if err != nil {
			return err
		}
		return renderBinding(cmd, runtime, resp.GetBinding())
	}}
	f := grant.Flags()
	f.StringVar(&principalID, "principal-id", "", "Principal ID")
	f.StringVar(&scope, "scope", "namespace", "platform or namespace")
	f.StringVar(&namespace, "namespace", "", "namespace scope")
	f.StringVar(&role, "role", "", "role name")
	_ = grant.MarkFlagRequired("principal-id")
	_ = grant.MarkFlagRequired("role")
	var includeRevoked bool
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.ListRoleBindings(s.Context, &adminv1.ListRoleBindingsRequest{PrincipalID: principalID, Namespace: namespace, IncludeRevoked: includeRevoked})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintRoleBindingListJSON(cmd.OutOrStdout(), resp.GetBindings())
		}
		fmt.Fprintln(cmd.OutOrStdout(), "ROLE\tSCOPE\tPRINCIPAL ID\tBINDING ID")
		for _, b := range resp.GetBindings() {
			scopeValue := enumName(b.GetScopeType().String())
			if b.GetNamespace() != "" {
				scopeValue += "/" + b.GetNamespace()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", enumName(b.GetRole().String()), scopeValue, b.GetPrincipalID(), b.GetBindingID())
		}
		return nil
	}}
	lf := list.Flags()
	lf.StringVar(&principalID, "principal-id", "", "Principal ID filter")
	lf.StringVar(&namespace, "namespace", "", "namespace filter")
	lf.BoolVar(&includeRevoked, "include-revoked", false, "include revoked bindings")
	revoke := &cobra.Command{Use: "revoke <binding-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.AccessAdmin.RevokeRoleBinding(s.Context, &adminv1.RevokeRoleBindingRequest{BindingID: args[0]})
		if err != nil {
			return err
		}
		return renderBinding(cmd, runtime, resp.GetBinding())
	}}
	root.AddCommand(grant, list, revoke)
	return root
}

func renderPrincipal(cmd *cobra.Command, runtime command.Runtime, p *adminv1.Principal) error {
	if runtime.Options.Output == "json" {
		return output.PrintPrincipalJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", p.GetName(), enumName(p.GetKind().String()), enumName(p.GetStatus().String()), p.GetPrincipalID())
	return nil
}
func renderCredential(cmd *cobra.Command, runtime command.Runtime, c *adminv1.PrincipalCredential) error {
	if runtime.Options.Output == "json" {
		return output.PrintPrincipalCredentialJSON(cmd.OutOrStdout(), c)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", c.GetLabel(), c.GetFingerprint(), c.GetCredentialID())
	return nil
}
func renderBinding(cmd *cobra.Command, runtime command.Runtime, b *adminv1.RoleBinding) error {
	if runtime.Options.Output == "json" {
		return output.PrintRoleBindingJSON(cmd.OutOrStdout(), b)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", enumName(b.GetRole().String()), enumName(b.GetScopeType().String()), b.GetNamespace(), b.GetBindingID())
	return nil
}

func enumName(value string) string {
	value = strings.ToLower(value)
	for _, prefix := range []string{"principal_kind_", "principal_status_", "access_scope_type_", "access_role_"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return value
}
