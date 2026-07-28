package sandboxd

import "time"

type BrowserStatusResponse struct {
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

type BrowserOpenRequest struct {
	URL string `json:"url,omitempty"`
}

type BrowserNavigateRequest struct {
	URL string `json:"url"`
}

type BrowserResizeRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type BrowserClickRequest struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button string `json:"button,omitempty"`
}

type BrowserTypeRequest struct {
	Text    string `json:"text"`
	DelayMS int    `json:"delayMs,omitempty"`
}

type BrowserWaitRequest struct {
	TimeoutMS int `json:"timeoutMs,omitempty"`
}
