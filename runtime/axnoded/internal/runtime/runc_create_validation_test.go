package runtime

import (
	"context"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestRuncRejectsRestoreBeforeWritableReservation(t *testing.T) {
	handler, err := NewRuncServiceHandler(
		config.Config{RootDir: t.TempDir()},
		config.RuntimeNameRunc,
		config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := &apipb.CreateContainerRequest{CkptDir: "/checkpoint"}
	if _, err := handler.CreateContainer(context.Background(), request, contract.HandlerOptions{ContainerID: "restore"}); err == nil {
		t.Fatal("CreateContainer must reject unsupported restore")
	}
	if hasWritableReservation(handler.writableCapacity, "restore") {
		t.Fatal("unsupported restore must not reserve writable capacity")
	}
	if _, err := handler.PrepareExecutionEnvelope(context.Background(), request, contract.HandlerOptions{ContainerID: "envelope-restore"}); err == nil {
		t.Fatal("PrepareExecutionEnvelope must reject unsupported restore")
	}
	if hasWritableReservation(handler.writableCapacity, "envelope-restore") {
		t.Fatal("unsupported envelope restore must not reserve writable capacity")
	}
}

func hasWritableReservation(manager *writableCapacityManager, containerID string) bool {
	if manager == nil {
		return false
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	_, found := manager.reservations[containerID]
	return found
}
