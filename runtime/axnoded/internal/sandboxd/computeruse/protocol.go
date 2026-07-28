package computeruse

type StatusResponse struct {
	Available    bool               `json:"available"`
	Display      string             `json:"display,omitempty"`
	Backend      string             `json:"backend,omitempty"`
	Reason       string             `json:"reason,omitempty"`
	Dependencies []DependencyStatus `json:"dependencies,omitempty"`
}

type DependencyStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ScreenshotRequest struct {
	ShowCursor bool    `json:"showCursor,omitempty"`
	Region     *Region `json:"region,omitempty"`
	Format     string  `json:"format,omitempty"`
	Quality    int     `json:"quality,omitempty"`
	Scale      float64 `json:"scale,omitempty"`
}

type ScreenshotResponse struct {
	Data        []byte
	ContentType string
}

type DisplayResponse struct {
	Display string `json:"display,omitempty"`
	Backend string `json:"backend,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

type MouseRequest struct {
	Action    string `json:"action,omitempty"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	ToX       int    `json:"toX,omitempty"`
	ToY       int    `json:"toY,omitempty"`
	Button    string `json:"button,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
}

type KeyboardRequest struct {
	Text    string   `json:"text,omitempty"`
	Key     string   `json:"key,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	DelayMS int      `json:"delayMs,omitempty"`
}
