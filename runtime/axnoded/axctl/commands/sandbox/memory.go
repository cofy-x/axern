package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/encoding/protojson"
)

type memoryRPCClient interface {
	GetSandboxMemory(sandboxID string) (*nodeoperatorv1.GetSandboxMemoryResponse, error)
	Close() error
}

var newMemoryRPCClient = func(ctx *cli.Context) (memoryRPCClient, error) {
	return client.New(ctx)
}

var MemoryCmd = cli.Command{
	Name:  "memory",
	Usage: "Inspect the host cgroup memory domain for a sandbox",
	Flags: []cli.Flag{
		cli.BoolFlag{Name: "json", Usage: "print the structured memory observation as JSON"},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one sandbox id must be specified")
		}
		opsClient, err := newMemoryRPCClient(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.GetSandboxMemory(context.Args().First())
		if err != nil {
			return err
		}
		observation := resp.GetObservation()
		if observation == nil {
			return fmt.Errorf("sandbox memory observation is unavailable")
		}
		if context.Bool("json") {
			encoded, err := (protojson.MarshalOptions{Indent: "  ", UseProtoNames: true}).Marshal(observation)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, string(encoded))
			return nil
		}
		renderSandboxMemory(os.Stdout, observation)
		return nil
	},
}

func renderSandboxMemory(w io.Writer, memory *controlnodev1.AllocationMemoryObservation) {
	if memory == nil {
		return
	}
	fmt.Fprintf(w, "  Allocation: %s (attempt %d)\n", memory.GetAllocationID(), memory.GetAttempt())
	fmt.Fprintf(w, "  Runtime: %s\n", fallbackString(memory.GetRuntime(), "-"))
	fmt.Fprintf(w, "  Observed At: %s (revision %d)\n", formatTimestamp(memory.GetObservedAt()), memory.GetRevision())
	fmt.Fprintf(w, "  Request / Limit: %d / %d bytes\n", memory.GetRequestBytes(), memory.GetLimitBytes())
	peakSource := "sampled current"
	if memory.GetPeakAvailable() {
		peakSource = "kernel memory.peak"
	}
	fmt.Fprintf(w, "  Current / Peak / Swap: %d / %d / %d bytes (peak source: %s)\n", memory.GetCurrentBytes(), memory.GetPeakBytes(), memory.GetSwapCurrentBytes(), peakSource)
	fmt.Fprintf(w, "  Anon / File / Shmem / Kernel: %d / %d / %d / %d bytes\n", memory.GetAnonBytes(), memory.GetFileBytes(), memory.GetShmemBytes(), memory.GetKernelBytes())
	fmt.Fprintf(w, "  Dirty / Writeback: %d / %d bytes\n", memory.GetDirtyBytes(), memory.GetWritebackBytes())
	fmt.Fprintf(w, "  Events high/max/oom/oom_kill/group_kill: %d/%d/%d/%d/%d\n", memory.GetEventHigh(), memory.GetEventMax(), memory.GetEventOom(), memory.GetEventOomKill(), memory.GetEventOomGroupKill())
	if memory.GetPsiAvailable() {
		fmt.Fprintf(w, "  PSI some/full avg10: %.3f / %.3f (total usec %d / %d)\n", memory.GetPsiSomeAvg10(), memory.GetPsiFullAvg10(), memory.GetPsiSomeTotalUsec(), memory.GetPsiFullTotalUsec())
	} else {
		fmt.Fprintln(w, "  PSI: unavailable")
	}
	fmt.Fprintf(w, "  Enforcement parent/leaf/PIDs: %t/%t/%t\n", memory.GetParentControlsVerified(), memory.GetLeafControlsVerified(), memory.GetPidRolesVerified())
	cleanupState := strings.ToLower(strings.TrimPrefix(memory.GetCleanupState().String(), "ALLOCATION_MEMORY_CLEANUP_STATE_"))
	fmt.Fprintf(w, "  Cgroup: %s (%s)\n", fallbackString(memory.GetCgroupIdentity(), "-"), fallbackString(cleanupState, "-"))
}
