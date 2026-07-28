package server

import (
	"errors"
	"net/http"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/browser"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (s *Server) handleBrowser(w http.ResponseWriter, r *http.Request) {
	if s.browser == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "browser provider unavailable")
		return
	}
	switch r.URL.Path {
	case wire.PathBrowserStatus:
		s.handleBrowserStatus(w, r)
	case wire.PathBrowserOpen:
		s.handleBrowserOpen(w, r)
	case wire.PathBrowserClose:
		s.handleBrowserClose(w, r)
	case wire.PathBrowserNavigate:
		s.handleBrowserNavigate(w, r)
	case wire.PathBrowserResize:
		s.handleBrowserResize(w, r)
	case wire.PathBrowserClick:
		s.handleBrowserClick(w, r)
	case wire.PathBrowserType:
		s.handleBrowserType(w, r)
	case wire.PathBrowserWait:
		s.handleBrowserWait(w, r)
	default:
		writeNotFound(w)
	}
}

func (s *Server) handleBrowserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.browser.Status())
}

func (s *Server) handleBrowserOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.OpenRequest
	if err := decodeOptionalJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser open request")
		return
	}
	status, err := s.browser.Open(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	status, err := s.browser.Close(r.Context())
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserNavigate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.NavigateRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser navigate request")
		return
	}
	status, err := s.browser.Navigate(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserResize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.ResizeRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser resize request")
		return
	}
	status, err := s.browser.Resize(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserClick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.ClickRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser click request")
		return
	}
	status, err := s.browser.Click(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserType(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.TypeRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser type request")
		return
	}
	status, err := s.browser.Type(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBrowserWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request browser.WaitRequest
	if err := decodeOptionalJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid browser wait request")
		return
	}
	status, err := s.browser.Wait(r.Context(), request)
	if err != nil {
		writeBrowserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func writeBrowserError(w http.ResponseWriter, err error) {
	if errors.Is(err, browser.ErrCommandFailed) {
		writeError(w, http.StatusInternalServerError, errorCodeCommandFailed, err.Error())
		return
	}
	writeProviderError(w, err, browser.ErrUnavailable, browser.ErrInvalidArgument)
}
