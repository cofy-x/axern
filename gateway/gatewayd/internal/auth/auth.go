package auth

import (
	"net/http"
	"strings"
)

type DevToken struct {
	Token string
}

func (a DevToken) Authorized(r *http.Request) bool {
	token := strings.TrimSpace(a.Token)
	if token == "" {
		return false
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("bearer "):]) == token
	}
	return strings.TrimSpace(r.URL.Query().Get("token")) == token
}
