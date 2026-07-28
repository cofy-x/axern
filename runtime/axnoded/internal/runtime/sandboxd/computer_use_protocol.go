package sandboxd

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

type ComputerUseRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ComputerUseScreenshotRequest struct {
	ShowCursor bool               `json:"showCursor,omitempty"`
	Region     *ComputerUseRegion `json:"region,omitempty"`
	Format     string             `json:"format,omitempty"`
	Quality    int                `json:"quality,omitempty"`
	Scale      float64            `json:"scale,omitempty"`
}

type ComputerUseScreenshotResponse struct {
	Data        []byte
	ContentType string
}

type ComputerUseDisplayResponse struct {
	Display string `json:"display,omitempty"`
	Backend string `json:"backend,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

type ComputerUseMouseRequest struct {
	Action    string `json:"action,omitempty"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	ToX       int    `json:"toX,omitempty"`
	ToY       int    `json:"toY,omitempty"`
	Button    string `json:"button,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
}

type ComputerUseKeyboardRequest struct {
	Text    string   `json:"text,omitempty"`
	Key     string   `json:"key,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	DelayMS int      `json:"delayMs,omitempty"`
}
