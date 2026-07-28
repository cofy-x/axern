package sandboxd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientProcessEndpoints(t *testing.T) {
	var processStarted bool
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/processes":
			switch r.Method {
			case http.MethodGet:
				fmt.Fprint(w, `{"processes":[{"id":"proc-1","state":"running","pid":12}]}`)
				return
			case http.MethodPost:
			default:
				t.Fatalf("processes method = %s", r.Method)
			}
			var request ProcessStartRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode process request: %v", err)
			}
			if strings.Join(request.Args, " ") != "/bin/sh -c echo ok" || !request.CaptureOutput {
				t.Fatalf("process request = %#v", request)
			}
			processStarted = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"proc-1","state":"running","pid":12}`)
		case "/processes/proc-1":
			fmt.Fprint(w, `{"id":"proc-1","state":"running","pid":12}`)
		case "/processes/proc-1/signal":
			if r.Method != http.MethodPost {
				t.Fatalf("process signal method = %s", r.Method)
			}
			fmt.Fprint(w, `{"id":"proc-1","state":"running","pid":12}`)
		case "/processes/proc-1/stdin":
			if r.Method != http.MethodPost {
				t.Fatalf("process stdin method = %s", r.Method)
			}
			var request ProcessStdinRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode stdin request: %v", err)
			}
			if string(request.Data) != "payload" {
				t.Fatalf("stdin request = %#v", request)
			}
			fmt.Fprint(w, `{"id":"proc-1","state":"running","pid":12}`)
		case "/processes/proc-1/stdin-close":
			if r.Method != http.MethodPost {
				t.Fatalf("process stdin close method = %s", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read stdin close body: %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("stdin close body = %q, want empty", string(body))
			}
			fmt.Fprint(w, `{"id":"proc-1","state":"running","pid":12}`)
		case "/processes/proc-1/stream":
			w.Header().Set("Content-Type", "application/x-ndjson")
			fmt.Fprintln(w, `{"stdout":"b2sK"}`)
			fmt.Fprintln(w, `{"stderr":"ZXJyCg=="}`)
		case "/processes/proc-1/wait":
			fmt.Fprint(w, `{"id":"proc-1","state":"exited","pid":12,"exitCode":0,"stdout":"ok\n"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer shutdown()

	client := NewClient(socketPath)
	started, err := client.StartProcess(context.Background(), ProcessStartRequest{
		Args:          []string{"/bin/sh", "-c", "echo ok"},
		CaptureOutput: true,
	})
	if err != nil {
		t.Fatalf("StartProcess() error = %v", err)
	}
	if started.ID != "proc-1" || !processStarted {
		t.Fatalf("started = %#v, processStarted = %v", started, processStarted)
	}
	processes, err := client.ListProcesses(context.Background())
	if err != nil {
		t.Fatalf("ListProcesses() error = %v", err)
	}
	if len(processes.Processes) != 1 || processes.Processes[0].ID != started.ID {
		t.Fatalf("process list = %#v", processes)
	}
	if _, err := client.ProcessStatus(context.Background(), started.ID); err != nil {
		t.Fatalf("ProcessStatus() error = %v", err)
	}
	if _, err := client.SignalProcess(context.Background(), started.ID, "TERM"); err != nil {
		t.Fatalf("SignalProcess() error = %v", err)
	}
	if _, err := client.WriteProcessStdin(context.Background(), started.ID, []byte("payload")); err != nil {
		t.Fatalf("WriteProcessStdin() error = %v", err)
	}
	if _, err := client.CloseProcessStdin(context.Background(), started.ID); err != nil {
		t.Fatalf("CloseProcessStdin() error = %v", err)
	}
	var streamed []ProcessStreamEvent
	if err := client.StreamProcess(context.Background(), started.ID, func(event ProcessStreamEvent) error {
		streamed = append(streamed, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamProcess() error = %v", err)
	}
	if len(streamed) != 2 || string(streamed[0].Stdout) != "ok\n" || string(streamed[1].Stderr) != "err\n" {
		t.Fatalf("streamed = %#v", streamed)
	}
	waited, err := client.WaitProcess(context.Background(), started.ID)
	if err != nil {
		t.Fatalf("WaitProcess() error = %v", err)
	}
	if waited.ExitCode == nil || *waited.ExitCode != 0 || waited.Stdout != "ok\n" {
		t.Fatalf("waited = %#v", waited)
	}
}
