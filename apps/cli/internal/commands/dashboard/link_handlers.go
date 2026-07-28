package dashboard

import (
	"net/http"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
)

func (s *server) handleLinks(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeResult(w, appdashboard.BuildLinks(s.linksConfig), nil)
}
