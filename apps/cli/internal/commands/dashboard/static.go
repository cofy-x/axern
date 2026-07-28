package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	data, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refresh := s.refresh
	if refresh <= 0 {
		refresh = 5 * time.Second
	}
	html := strings.Replace(string(data), "refreshMs: 5000", fmt.Sprintf("refreshMs: %d", refresh.Milliseconds()), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}
