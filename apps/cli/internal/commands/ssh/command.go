package sshcmd

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

type options struct {
	targetID              string
	preferredAllocationID string
	endpoint              string
	identityFile          string
	remoteCommand         string
	containerUser         string
	requestTTY            bool
	strictHostKeyChecking bool
	sshOptions            []string
}

func Command(runtime command.Runtime) *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "ssh <allocation-id|service-id> [shell]",
		Short: "Open an SSH-compatible terminal to an allocation or service",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return command.Usage(fmt.Errorf("allocation or service id is required"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.targetID = strings.TrimSpace(args[0])
			if strings.Contains(opts.targetID, "@") {
				return command.Usage(fmt.Errorf("pass only the allocation or service id; the SSH endpoint comes from context or --ssh-endpoint"))
			}
			if len(args) > 1 {
				if opts.remoteCommand != "" {
					return command.Usage(fmt.Errorf("use either --shell or trailing shell arguments, not both"))
				}
				opts.remoteCommand = strings.Join(args[1:], " ")
			}
			if err := resolveConnection(runtime, cmd, &opts); err != nil {
				return err
			}
			if opts.containerUser != "" && !validContainerUser(opts.containerUser) {
				return command.Usage(fmt.Errorf("container user may contain only letters, numbers, '_', '-', '.', and at most one ':'"))
			}
			allocationID, err := resolveTargetAllocation(cmd, runtime, opts)
			if err != nil {
				return err
			}
			args, err = buildArgs(opts, allocationID)
			if err != nil {
				return command.Usage(err)
			}
			if _, err := exec.LookPath("ssh"); err != nil {
				return fmt.Errorf("ssh executable not found in PATH")
			}
			return runOpenSSH(cmd, args, opts.requestTTY)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.endpoint, "ssh-endpoint", "", "gateway SSH host:port")
	f.StringVarP(&opts.identityFile, "identity-file", "i", "", "gateway SSH private key")
	f.StringVar(&opts.preferredAllocationID, "allocation-id", "", "ready allocation to use when the target is a service")
	f.StringVar(&opts.remoteCommand, "shell", "", "interactive shell path")
	f.StringVarP(&opts.containerUser, "user", "u", "", "container user")
	f.BoolVar(&opts.requestTTY, "tty", true, "request a local OpenSSH pseudo-terminal")
	f.BoolVar(&opts.strictHostKeyChecking, "strict-host-key-checking", false, "use the local OpenSSH known_hosts policy")
	f.StringArrayVar(&opts.sshOptions, "ssh-option", nil, "extra OpenSSH option; may be repeated")
	return cmd
}

func resolveConnection(runtime command.Runtime, cmd *cobra.Command, opts *options) error {
	if !hasExplicitSSHConnection(cmd) {
		_, profile, ok, err := runtime.ResolveContext()
		if err != nil {
			return command.Usage(err)
		}
		if ok {
			opts.endpoint = profile.SSHEndpoint
			opts.identityFile = profile.SSHIdentityFile
		}
	}
	if value := strings.TrimSpace(os.Getenv("AXERN_SSH_ENDPOINT")); value != "" {
		opts.endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("AXERN_SSH_IDENTITY_FILE")); value != "" {
		opts.identityFile = value
	}
	if cmd.Flags().Changed("ssh-endpoint") {
		opts.endpoint, _ = cmd.Flags().GetString("ssh-endpoint")
	}
	if cmd.Flags().Changed("identity-file") {
		opts.identityFile, _ = cmd.Flags().GetString("identity-file")
	}
	if strings.TrimSpace(opts.endpoint) == "" {
		return command.Usage(fmt.Errorf("SSH endpoint is not configured; set context ssh_endpoint or pass --ssh-endpoint"))
	}
	if strings.TrimSpace(opts.identityFile) == "" {
		return command.Usage(fmt.Errorf("SSH identity file is not configured; set context ssh_identity_file or pass --identity-file"))
	}
	return nil
}

func hasExplicitSSHConnection(cmd *cobra.Command) bool {
	endpoint := strings.TrimSpace(os.Getenv("AXERN_SSH_ENDPOINT")) != ""
	identity := strings.TrimSpace(os.Getenv("AXERN_SSH_IDENTITY_FILE")) != ""
	if cmd.Flags().Changed("ssh-endpoint") {
		value, _ := cmd.Flags().GetString("ssh-endpoint")
		endpoint = strings.TrimSpace(value) != ""
	}
	if cmd.Flags().Changed("identity-file") {
		value, _ := cmd.Flags().GetString("identity-file")
		identity = strings.TrimSpace(value) != ""
	}
	return endpoint && identity
}

func resolveTargetAllocation(cmd *cobra.Command, runtime command.Runtime, opts options) (string, error) {
	if !strings.HasPrefix(opts.targetID, "svc-") {
		if opts.preferredAllocationID != "" {
			return "", command.Usage(fmt.Errorf("--allocation-id is only valid when the target is a service"))
		}
		return opts.targetID, nil
	}
	session, err := runtime.Open(cmd.Context())
	if err != nil {
		return "", err
	}
	defer session.Close()
	candidates, err := appservice.New(session.Clients.Service).CurrentReadyAllocationCandidates(session.Context, opts.targetID)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("service %s has no current ready allocation", opts.targetID)
	}
	if opts.preferredAllocationID != "" {
		for _, candidate := range candidates {
			if candidate.ID == opts.preferredAllocationID {
				return candidate.ID, nil
			}
		}
		return "", fmt.Errorf("allocation %s is not a current ready replica of service %s", opts.preferredAllocationID, opts.targetID)
	}
	if len(candidates) == 1 {
		return candidates[0].ID, nil
	}
	return promptServiceAllocation(cmd, opts.targetID, candidates)
}

func promptServiceAllocation(cmd *cobra.Command, serviceID string, candidates []appservice.AllocationCandidate) (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return "", command.Usage(fmt.Errorf("service %s has %d current ready allocations; pass --allocation-id", serviceID, len(candidates)))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Service %s has %d current ready allocations:\n", serviceID, len(candidates))
	for i, candidate := range candidates {
		nodeID := candidate.NodeID
		if nodeID == "" {
			nodeID = "-"
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "  %d) %s node=%s\n", i+1, candidate.ID, nodeID)
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Select allocation: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(candidates) {
		return "", command.Usage(fmt.Errorf("invalid allocation selection"))
	}
	return candidates[choice-1].ID, nil
}

func buildArgs(opts options, allocationID string) ([]string, error) {
	host, port, err := net.SplitHostPort(opts.endpoint)
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("SSH endpoint must be host:port")
	}
	args := make([]string, 0, 16)
	if opts.requestTTY {
		args = append(args, "-tt")
	}
	args = append(args, "-i", opts.identityFile, "-p", port)
	if !opts.strictHostKeyChecking {
		args = append(args, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "GlobalKnownHostsFile=/dev/null")
	}
	for _, option := range opts.sshOptions {
		if strings.TrimSpace(option) != "" {
			args = append(args, "-o", option)
		}
	}
	if opts.containerUser != "" {
		args = append(args, "-o", "SetEnv=AXERN_EXEC_USER="+opts.containerUser)
	}
	args = append(args, fmt.Sprintf("%s@%s", allocationID, host))
	if opts.remoteCommand != "" {
		args = append(args, opts.remoteCommand)
	}
	return args, nil
}

func runOpenSSH(cmd *cobra.Command, args []string, requestTTY bool) error {
	if requestTTY {
		defer output.RestoreTerminal(cmd.OutOrStdout())
	}
	process := exec.CommandContext(cmd.Context(), "ssh", args...)
	process.Stdin = os.Stdin
	process.Stdout = cmd.OutOrStdout()
	process.Stderr = cmd.ErrOrStderr()
	if err := process.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return command.ExitError{Code: exitErr.ExitCode()}
		}
		return err
	}
	return nil
}

func validContainerUser(user string) bool {
	user = strings.TrimSpace(user)
	if user == "" || len(user) > 128 {
		return false
	}
	userPart, groupPart, hasGroup := strings.Cut(user, ":")
	if userPart == "" || (hasGroup && groupPart == "") {
		return false
	}
	for _, part := range []string{userPart, groupPart} {
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
				return false
			}
		}
	}
	return true
}
