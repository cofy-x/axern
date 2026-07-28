package dashboard

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
)

//go:embed assets/*
var embeddedAssets embed.FS

type Handler struct {
	auth      auth.DevToken
	assets    http.Handler
	vendorDir string
	resolver  ServiceReplicaResolver
}

func New(token auth.DevToken, vendorDir string, resolver ServiceReplicaResolver) (*Handler, error) {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		return nil, fmt.Errorf("open embedded dashboard assets: %w", err)
	}
	return &Handler{
		auth:      token,
		assets:    http.FileServer(http.FS(assets)),
		vendorDir: strings.TrimSpace(vendorDir),
		resolver:  resolver,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.auth.Authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch {
	case r.URL.Path == "/dashboard" || r.URL.Path == "/dashboard/":
		h.serveIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/dashboard/assets/"):
		h.serveAssets(w, r)
	case strings.HasPrefix(r.URL.Path, "/dashboard/vendor/"):
		h.serveVendor(w, r)
	case strings.HasPrefix(r.URL.Path, "/dashboard/api/services/"):
		h.serveServiceReplicas(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) Handles(path string) bool {
	return path == "/dashboard" ||
		path == "/dashboard/" ||
		strings.HasPrefix(path, "/dashboard/assets/") ||
		strings.HasPrefix(path, "/dashboard/vendor/") ||
		strings.HasPrefix(path, "/dashboard/api/")
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := embeddedAssets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "dashboard asset unavailable", http.StatusInternalServerError)
		return
	}
	tokenQuery := ""
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		tokenQuery = "?token=" + html.EscapeString(url.QueryEscape(token))
	}
	content := strings.ReplaceAll(string(data), "{{TOKEN_QUERY}}", tokenQuery)
	if h.vendorReady() {
		content = strings.ReplaceAll(content, "{{VENDOR_READY}}", "true")
	} else {
		content = strings.ReplaceAll(content, "{{VENDOR_READY}}", "false")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

func (h *Handler) serveAssets(w http.ResponseWriter, r *http.Request) {
	r2 := new(http.Request)
	*r2 = *r
	r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/dashboard/assets/")
	h.assets.ServeHTTP(w, r2)
}

func (h *Handler) serveVendor(w http.ResponseWriter, r *http.Request) {
	if h.vendorDir == "" {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/dashboard/vendor/")
	switch name {
	case "xterm.js", "xterm.css", "addon-fit.js":
	default:
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(h.vendorDir, name))
}

func (h *Handler) vendorReady() bool {
	for _, name := range []string{"xterm.js", "xterm.css", "addon-fit.js"} {
		info, err := os.Stat(filepath.Join(h.vendorDir, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
