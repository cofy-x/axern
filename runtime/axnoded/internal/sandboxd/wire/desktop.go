package wire

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
