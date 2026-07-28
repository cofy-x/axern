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
	Capacity resourceQuantity `json:"capacity"`
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
		rows := resourceRows(snapshot)
		if context.Bool("json") {
			encoded, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(encoded))
			return nil
		}
		printResourceRows(os.Stdout, rows)
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
			Capacity:     snapshot.Node.Capacity.MemoryBytes,
			Unbounded:    snapshot.Resources.Memory.AxnodedUnboundedCount,
			RunningCount: running,
		},
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
