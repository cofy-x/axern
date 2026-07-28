package functionhttp

import (
	"bytes"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
)

const BundlePathPrefix = "/runtime/function-bundles/"

var timeZero = time.Time{}

func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(BundlePathPrefix, func(w http.ResponseWriter, r *http.Request) {
		if cfg.ReadBundle == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := strings.TrimSpace(cfg.Token)
		if token != "" && !authorizedBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, BundlePathPrefix), "/")
		if name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		storageURI := functionkernel.FunctionBundleStorageURIPrefix + name
		bundle, ok, err := cfg.ReadBundle(r.Context(), storageURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if bundle.MediaType != "" {
			w.Header().Set("Content-Type", bundle.MediaType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("X-Axern-Function-Bundle-Digest", bundle.Digest)
		http.ServeContent(w, r, name, timeZero, bytes.NewReader(bundle.Payload))
	})
	return mux
}

func authorizedBearer(r *http.Request, token string) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return false
	}
	got := strings.TrimSpace(header[len("bearer "):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
