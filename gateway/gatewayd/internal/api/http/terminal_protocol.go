package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"
)

type terminalClientMessage struct {
	Type string
	Data []byte
}

func parseTerminalClientMessage(data []byte) (terminalClientMessage, bool) {
	var msg struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if json.Unmarshal(data, &msg) != nil || msg.Type == "" {
		return terminalClientMessage{}, false
	}
	return terminalClientMessage{Type: msg.Type, Data: []byte(msg.Data)}, true
}

func parseResizeMessage(data []byte) (uint32, uint32, bool) {
	var msg struct {
		Cols uint32 `json:"cols"`
		Rows uint32 `json:"rows"`
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
		return msg.Cols, msg.Rows, true
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	cols, err1 := strconv.ParseUint(parts[0], 10, 32)
	rows, err2 := strconv.ParseUint(parts[1], 10, 32)
	if err1 == nil && err2 == nil && cols > 0 && rows > 0 {
		return uint32(cols), uint32(rows), true
	}
	return 0, 0, false
}
