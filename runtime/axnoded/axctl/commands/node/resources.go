package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli"
)

const defaultInventoryURL = "http://127.0.0.1:23001/inventoryz"

type inventorySnapshot struct {
	Resources  inventoryResources  `json:"resources"`
	Node       inventoryNode       `json:"node"`
	Components inventoryComponents `json:"components"`
}

type inventoryResources struct {
	CPU    inventoryCPU    `json:"cpu"`
	Memory inventoryMemory `json:"memory"`
}

type inventoryCPU struct {
	AxnodedCommittedMilli int64 `json:"axnoded_committed_milli"`
	AxnodedUsedMilli      int64 `json:"axnoded_used_milli"`
	AxnodedUnboundedCount int64 `json:"axnoded_unbounded_count"`
}

type inventoryMemory struct {
	AxnodedCommittedBytes int64 `json:"axnoded_committed_bytes"`
	AxnodedUsedBytes      int64 `json:"axnoded_used_bytes"`
	AxnodedUnboundedCount int64 `json:"axnoded_unbounded_count"`
}

type resourceQuantity struct {
	CPUMilli    int64 `json:"cpu_milli"`
	MemoryBytes int64 `json:"memory_bytes"`
}

type inventoryNode struct {
	NodeID       string                `json:"node_id"`
	Capacity     resourceQuantity      `json:"capacity"`
	MemoryBudget inventoryMemoryBudget `json:"memory_budget"`
}

type inventoryMemoryBudget struct {
	PhysicalCapacityBytes     int64  `json:"physical_capacity_bytes"`
	SourceAllocatableBytes    int64  `json:"source_allocatable_bytes"`
	DelegatedRootLimitBytes   int64  `json:"delegated_root_limit_bytes"`
	DelegatedRootLimitFinite  bool   `json:"delegated_root_limit_finite"`
	SystemReserveBytes        int64  `json:"system_reserve_bytes"`
	EffectiveAllocatableBytes int64  `json:"effective_allocatable_bytes"`
	LocalCommitmentBytes      int64  `json:"local_commitment_bytes"`
	CleanupDebtBytes          int64  `json:"cleanup_debt_bytes"`
	InternalCurrentBytes      int64  `json:"internal_current_bytes"`
	RetiringCgroupCount       int64  `json:"retiring_cgroup_count"`
	OldestRetiringAgeSeconds  int64  `json:"oldest_retiring_age_seconds"`
	SystemReserveExhausted    bool   `json:"system_reserve_exhausted"`
	CapacityIdentity          string `json:"capacity_identity"`
	Mode                      string `json:"mode"`
	SampledAt                 string `json:"sampled_at"`
}

type inventoryComponents struct {
	Axnoded inventoryAxnoded `json:"axnoded"`
}

type inventoryAxnoded struct {
	RunningContainers int64 `json:"running_containers"`
}

type resourceRow struct {
	Resource     string `json:"resource"`
	Committed    int64  `json:"committed"`
	Used         int64  `json:"used"`
	Capacity     int64  `json:"capacity"`
	Unbounded    int64  `json:"unbounded_count"`
	RunningCount int64  `json:"running_containers"`
}

type resourceReport struct {
	NodeID       string                `json:"node_id"`
	Resources    []resourceRow         `json:"resources"`
	MemoryBudget inventoryMemoryBudget `json:"memory_budget"`
}

var ResourcesCmd = cli.Command{
	Name:  "resources",
	Usage: "Print local axnoded resource commitment and usage",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "inventory-url",
			Usage:  "local axnoded inventory URL",
			Value:  defaultInventoryURL,
			EnvVar: "AXNODED_INVENTORY_URL",
		},
		cli.BoolFlag{Name: "json", Usage: "print JSON output"},
	},
	Action: func(context *cli.Context) error {
		snapshot, err := fetchInventory(context)
		if err != nil {
			return err
		}
		report := buildResourceReport(snapshot)
		if context.Bool("json") {
			encoded, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(encoded))
			return nil
		}
		fmt.Printf("Node ID: %s\n", fallbackValue(report.NodeID, "-"))
		printResourceRows(os.Stdout, report.Resources)
		printMemoryBudget(os.Stdout, report.MemoryBudget)
		return nil
	},
}

func fetchInventory(ctx *cli.Context) (*inventorySnapshot, error) {
	reqCtx, cancel := commandContext(ctx)
	defer cancel()
	url := normalizeInventoryURL(ctx.String("inventory-url"))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch inventory from %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch inventory from %s: status %d", url, resp.StatusCode)
	}
	out := &inventorySnapshot{}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("decode inventory: %w", err)
	}
	return out, nil
}

func commandContext(ctx *cli.Context) (context.Context, context.CancelFunc) {
	if timeout := ctx.GlobalDuration("timeout"); timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

func normalizeInventoryURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultInventoryURL
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "http://" + strings.TrimRight(value, "/") + "/inventoryz"
}

func resourceRows(snapshot *inventorySnapshot) []resourceRow {
	if snapshot == nil {
		return nil
	}
	running := snapshot.Components.Axnoded.RunningContainers
	memoryCapacity := snapshot.Node.MemoryBudget.EffectiveAllocatableBytes
	if memoryCapacity <= 0 {
		memoryCapacity = snapshot.Node.Capacity.MemoryBytes
	}
	return []resourceRow{
		{
			Resource:     "cpu_milli",
			Committed:    snapshot.Resources.CPU.AxnodedCommittedMilli,
			Used:         snapshot.Resources.CPU.AxnodedUsedMilli,
			Capacity:     snapshot.Node.Capacity.CPUMilli,
			Unbounded:    snapshot.Resources.CPU.AxnodedUnboundedCount,
			RunningCount: running,
		},
		{
			Resource:     "memory_bytes",
			Committed:    snapshot.Resources.Memory.AxnodedCommittedBytes,
			Used:         snapshot.Resources.Memory.AxnodedUsedBytes,
			Capacity:     memoryCapacity,
			Unbounded:    snapshot.Resources.Memory.AxnodedUnboundedCount,
			RunningCount: running,
		},
	}
}

func buildResourceReport(snapshot *inventorySnapshot) resourceReport {
	if snapshot == nil {
		return resourceReport{}
	}
	return resourceReport{
		NodeID:       snapshot.Node.NodeID,
		Resources:    resourceRows(snapshot),
		MemoryBudget: snapshot.Node.MemoryBudget,
	}
}

func printResourceRows(out io.Writer, rows []resourceRow) {
	tw := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tCOMMITTED\tUSED\tCAPACITY\tUNBOUNDED\tRUNNING")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n", row.Resource, row.Committed, row.Used, row.Capacity, row.Unbounded, row.RunningCount)
	}
	_ = tw.Flush()
}

func printMemoryBudget(out io.Writer, budget inventoryMemoryBudget) {
	fmt.Fprintln(out, "Memory budget:")
	tw := tabwriter.NewWriter(out, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "PHYSICAL\tSOURCE ALLOCATABLE\tRAW\tRESERVE\tEFFECTIVE\tCOMMITTED\tCLEANUP DEBT\tINTERNAL\tRETIRING\tEXHAUSTED")
	fmt.Fprintf(tw, "%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%t\n",
		budget.PhysicalCapacityBytes, budget.SourceAllocatableBytes,
		budget.EffectiveAllocatableBytes+budget.SystemReserveBytes,
		budget.SystemReserveBytes, budget.EffectiveAllocatableBytes,
		budget.LocalCommitmentBytes, budget.CleanupDebtBytes, budget.InternalCurrentBytes,
		budget.RetiringCgroupCount, budget.SystemReserveExhausted)
	_ = tw.Flush()
	if budget.DelegatedRootLimitFinite {
		fmt.Fprintf(out, "Delegated root limit: %d bytes\n", budget.DelegatedRootLimitBytes)
	} else {
		fmt.Fprintln(out, "Delegated root limit: max")
	}
	fmt.Fprintf(out, "Mode: %s\n", fallbackValue(budget.Mode, "-"))
	fmt.Fprintf(out, "Capacity identity: %s\n", fallbackValue(budget.CapacityIdentity, "-"))
	fmt.Fprintf(out, "Sampled at: %s\n", fallbackValue(budget.SampledAt, "-"))
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
