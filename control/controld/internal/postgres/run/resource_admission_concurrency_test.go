package pgrun

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestConcurrentAdmissionDoesNotOversellNodeResources(t *testing.T) {
	tests := []struct {
		name        string
		allocatable *commonv1.ResourceQuantity
		requested   *commonv1.ResourceQuantity
		slots       int32
		wantAllowed int64
	}{
		{name: "cpu", allocatable: &commonv1.ResourceQuantity{CpuMilli: 4000, MemoryBytes: 64 << 30, EphemeralStorageBytes: 64 << 30}, requested: &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1 << 20}, slots: 32, wantAllowed: 4},
		{name: "memory", allocatable: &commonv1.ResourceQuantity{CpuMilli: 32000, MemoryBytes: 4 << 30, EphemeralStorageBytes: 64 << 30}, requested: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 1 << 30}, slots: 32, wantAllowed: 4},
		{name: "ephemeral_storage", allocatable: &commonv1.ResourceQuantity{CpuMilli: 32000, MemoryBytes: 64 << 30, EphemeralStorageBytes: 4 << 30}, requested: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 1 << 20, EphemeralStorageBytes: 1 << 30}, slots: 32, wantAllowed: 4},
		{name: "runtime_slot", allocatable: &commonv1.ResourceQuantity{CpuMilli: 32000, MemoryBytes: 64 << 30, EphemeralStorageBytes: 64 << 30}, requested: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 1 << 20}, slots: 2, wantAllowed: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newEnvironmentTestDB(t)
			now := time.Now().UTC()
			envStore := NewStore(db)
			t.Cleanup(envStore.Close)
			env, err := envStore.CreateEnvironment(context.Background(), runkernel.CreateEnvironmentParams{
				Spec: &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"}},
			}, now)
			if err != nil {
				t.Fatal(err)
			}

			summary := controldtest.ReadySummary(now)
			summary.Allocatable = tt.allocatable
			summary.Capacity = &commonv1.ResourceQuantity{CpuMilli: tt.allocatable.GetCpuMilli(), MemoryBytes: tt.allocatable.GetMemoryBytes() + (1 << 30), EphemeralStorageBytes: tt.allocatable.GetEphemeralStorageBytes()}
			controldtest.SetReadySummaryMemory(summary, tt.allocatable.GetMemoryBytes())
			summary.Pools.RuntimeSlots = &nodev1.PoolState{Capacity: tt.slots, Idle: tt.slots}
			summaryJSON, err := protojson.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			const nodeID = "node-admission-concurrency"
			if _, err := db.Pool().Exec(context.Background(), `
				INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
				VALUES ($1, '127.0.0.1:24010', repeat('0', 64), $2, $2, 'active')
			`, nodeID, now); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(context.Background(), `
				INSERT INTO node_summaries (node_id, summary) VALUES ($1, $2::jsonb)
			`, nodeID, summaryJSON); err != nil {
				t.Fatal(err)
			}

			config := executionkernel.NormalizeConfig(&commonv1.ExecutionConfig{
				Resources: &commonv1.ResourceSpec{Requests: tt.requested},
				Network:   &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST},
			})
			requests := config.GetResources().GetRequests()
			limits := config.GetResources().GetLimits()
			record := &nodekernel.Record{NodeID: nodeID, NodeTarget: "127.0.0.1:24010", Lifecycle: nodekernel.LifecycleActive, AdmittedAt: now, LastHeartbeatAt: now, Summary: summary}
			request := &placementkernel.Request{
				RootfsKey: env.GetID(), RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE, MountType: nodev1.MountType_MOUNT_TYPE_OCI,
				RootfsWritable: true, MemoryLimitBytes: limits.GetMemoryBytes(), EphemeralStorageLimitBytes: limits.GetEphemeralStorageBytes(),
				RequestedCpuMilli: requests.GetCpuMilli(), RequestedMemoryBytes: requests.GetMemoryBytes(), RequestedEphemeralStorageBytes: requests.GetEphemeralStorageBytes(), Network: "host",
			}
			engine := placement.NewEngine(placement.Config{})
			evaluation := engine.Evaluate(record, request, now)
			if evaluation.GetState() != placementkernel.CandidateStateEligible {
				t.Fatalf("fixture candidate rejected: %#v", evaluation.GetRejectionReasons())
			}
			candidate := &placementkernel.Candidate{Record: record, Evaluation: evaluation, BaseRequest: request, Request: request}

			const contenders = 12
			start := make(chan struct{})
			var allowed atomic.Int64
			var wg sync.WaitGroup
			errs := make(chan error, contenders)
			for range contenders {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					store := NewStore(db, WithPlacementEvaluator(engine))
					defer store.Close()
					_, err := store.AdmitRun(context.Background(), runkernel.AdmitRunParams{
						Namespace: "default", Environment: env, Config: config, Candidates: []*placementkernel.Candidate{candidate},
					}, now)
					if err == nil {
						allowed.Add(1)
						return
					}
					if grpcstatus.Code(err) != codes.ResourceExhausted {
						errs <- err
					}
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("unexpected admission error: %v", err)
			}
			if got := allowed.Load(); got != tt.wantAllowed {
				t.Fatalf("allowed admissions = %d, want %d", got, tt.wantAllowed)
			}

			var count, cpu, memory, ephemeral int64
			if err := db.Pool().QueryRow(context.Background(), `
				SELECT COUNT(*), COALESCE(SUM(cpu_request_milli), 0), COALESCE(SUM(sandbox_memory_request_bytes), 0), COALESCE(SUM(ephemeral_storage_request_bytes), 0)
				FROM allocations WHERE node_id = $1 AND lifecycle_state <> 'ALLOCATION_LIFECYCLE_STATE_RELEASED'
			`, nodeID).Scan(&count, &cpu, &memory, &ephemeral); err != nil {
				t.Fatal(err)
			}
			if count != tt.wantAllowed || cpu > tt.allocatable.GetCpuMilli() || memory > tt.allocatable.GetMemoryBytes() || ephemeral > tt.allocatable.GetEphemeralStorageBytes() || count > int64(tt.slots) {
				t.Fatalf("durable charge count=%d cpu=%d memory=%d ephemeral=%d exceeds capacity=%#v slots=%d", count, cpu, memory, ephemeral, tt.allocatable, tt.slots)
			}
		})
	}
}
