package debughttp

import (
	"encoding/json"
	"net/http"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/nodesz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Nodes any `json:"nodes"`
		}{
			Nodes: cfg.DebugNodes(),
		})
	})
	mux.HandleFunc("/resourcez", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		policy := ResourcePolicySnapshot{}
		if cfg.ResourcePolicy != nil {
			policy = cfg.ResourcePolicy()
		}
		_ = json.NewEncoder(w).Encode(struct {
			Policy ResourcePolicySnapshot `json:"policy"`
		}{
			Policy: policy,
		})
	})
	mux.HandleFunc("/catalogz", func(w http.ResponseWriter, r *http.Request) {
		resp, err := cfg.ListRuntimeTemplates(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeProtoJSON(w, resp)
	})
	mux.HandleFunc("/quotasz", func(w http.ResponseWriter, r *http.Request) {
		resp, err := cfg.ListNamespaceQuotas(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeProtoJSON(w, resp)
	})
	mux.HandleFunc("/allocation-reconcilez", func(w http.ResponseWriter, r *http.Request) {
		items, err := cfg.ListReconcileQueue(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Items any `json:"items"`
		}{
			Items: items,
		})
	})
	mux.HandleFunc("/reconcilez", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		health := reconcilekernel.EmptyHealthSnapshot()
		if cfg.ReconcileHealth != nil {
			health = cfg.ReconcileHealth()
		}
		_ = json.NewEncoder(w).Encode(health)
	})
	mux.HandleFunc("/consistencyz", func(w http.ResponseWriter, r *http.Request) {
		if cfg.ConsistencySnapshot == nil {
			http.Error(w, "consistency snapshot is not configured", http.StatusInternalServerError)
			return
		}
		snapshot, err := cfg.ConsistencySnapshot(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	return mux
}

func writeProtoJSON(w http.ResponseWriter, message proto.Message) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	marshaler := protojson.MarshalOptions{
		UseEnumNumbers:  false,
		EmitUnpopulated: true,
	}
	data, err := marshaler.Marshal(message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}
