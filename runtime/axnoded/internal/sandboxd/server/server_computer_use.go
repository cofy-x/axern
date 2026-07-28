package server

import (
	"net/http"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/computeruse"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (s *Server) handleComputerUse(w http.ResponseWriter, r *http.Request) {
	if s.computerUse == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "computer_use provider unavailable")
		return
	}
	switch r.URL.Path {
	case wire.PathComputerUseStatus:
		s.handleComputerUseStatus(w, r)
	case wire.PathComputerUseScreenshot:
		s.handleComputerUseScreenshot(w, r)
	case wire.PathComputerUseDisplay:
		s.handleComputerUseDisplay(w, r)
	case wire.PathComputerUseMouse:
		s.handleComputerUseMouse(w, r)
	case wire.PathComputerUseKeyboard:
		s.handleComputerUseKeyboard(w, r)
	default:
		writeNotFound(w)
	}
}

func (s *Server) handleComputerUseStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.computerUse.Status(r.Context()))
}

func (s *Server) handleComputerUseScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	request, err := screenshotRequestFromQuery(r)
	if err != nil {
		writeComputerUseError(w, err)
		return
	}
	result, err := s.computerUse.Screenshot(r.Context(), request)
	if err != nil {
		writeComputerUseError(w, err)
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

func (s *Server) handleComputerUseDisplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	display, err := s.computerUse.Display(r.Context())
	if err != nil {
		writeComputerUseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, display)
}

func (s *Server) handleComputerUseMouse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request computeruse.MouseRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid mouse request")
		return
	}
	if err := s.computerUse.Mouse(r.Context(), request); err != nil {
		writeComputerUseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse())
}

func (s *Server) handleComputerUseKeyboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request computeruse.KeyboardRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid keyboard request")
		return
	}
	if err := s.computerUse.Keyboard(r.Context(), request); err != nil {
		writeComputerUseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse())
}

func writeComputerUseError(w http.ResponseWriter, err error) {
	writeProviderError(w, err, computeruse.ErrUnavailable, computeruse.ErrInvalidArgument)
}
