package browser

import "time"

type StatusResponse struct {
	Available    bool       `json:"available"`
	Command      string     `json:"command,omitempty"`
	Running      bool       `json:"running"`
	Pid          int        `json:"pid,omitempty"`
	URL          string     `json:"url,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	LastActionAt *time.Time `json:"lastActionAt,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
}

type OpenRequest struct {
	URL string `json:"url,omitempty"`
}

type NavigateRequest struct {
	URL string `json:"url"`
}

type ResizeRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ClickRequest struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button string `json:"button,omitempty"`
}

type TypeRequest struct {
	Text    string `json:"text"`
	DelayMS int    `json:"delayMs,omitempty"`
}

type WaitRequest struct {
	TimeoutMS int `json:"timeoutMs,omitempty"`
}
