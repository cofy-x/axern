package app

import (
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func int32ptr(v int32) *int32 { return &v }

func stringptr(v string) *string { return &v }

func updateMask(paths ...string) *fieldmaskpb.FieldMask {
	return &fieldmaskpb.FieldMask{Paths: paths}
}

func countServiceEvents(events []*servicev1.ServiceEvent, want servicev1.ServiceEventType) int {
	count := 0
	for _, event := range events {
		if event.GetType() == want {
			count++
		}
	}
	return count
}

func findFirstServiceEvent(events []*servicev1.ServiceEvent, want servicev1.ServiceEventType) *servicev1.ServiceEvent {
	for _, event := range events {
		if event.GetType() == want {
			return event
		}
	}
	return nil
}
