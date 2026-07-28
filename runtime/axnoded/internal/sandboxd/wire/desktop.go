package wire

import "time"

type ComputerUseStatusResponse struct {
	Available    bool                          `json:"available"`
	Display      string                        `json:"display,omitempty"`
	Backend      string                        `json:"backend,omitempty"`
	Reason       string                        `json:"reason,omitempty"`
	Dependencies []ComputerUseDependencyStatus `json:"dependencies,omitempty"`
}

type ComputerUseDependencyStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

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
