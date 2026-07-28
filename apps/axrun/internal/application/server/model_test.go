package server

import "testing"

func TestRolloutRequestUsesOnlyTaskSetReference(t *testing.T) {
	request := RolloutRequest{
		TaskSetRef:  "registry.local/tasksets/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Agent:       "oracle",
		Model:       "test/model",
		BackendName: "local",
		Concurrency: 2,
		Attempts:    3,
	}
	if err := request.validate(); err != nil {
		t.Fatal(err)
	}
	params := request.toParams()
	if params.TaskSetRef != request.TaskSetRef || params.Concurrency != 2 || params.Attempts != 3 {
		t.Fatalf("params=%#v", params)
	}
}
