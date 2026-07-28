package domain

type TaskInstance struct {
	ID           string            `json:"id"`
	Instruction  string            `json:"instruction"`
	Source       *SourceRef        `json:"source,omitempty"`
	Sandbox      SandboxSpec       `json:"sandbox"`
	Verifier     VerifierSpec      `json:"verifier"`
	Timeouts     *TimeoutPolicy    `json:"timeouts,omitempty"`
	Resources    *ResourceSpec     `json:"resources,omitempty"`
	InitialState *InitialStateSpec `json:"initial_state,omitempty"`
	Oracle       *OracleSpec       `json:"oracle,omitempty"`
	Tags         []string          `json:"tags"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Outputs      []TaskOutputSpec  `json:"outputs,omitempty"`
	Metadata     KeyValue          `json:"metadata,omitempty"`
}

type TaskOutputSpec struct {
	Path       string `json:"path" yaml:"path"`
	Required   bool   `json:"required,omitempty" yaml:"required,omitempty"`
	JSONSchema string `json:"json_schema,omitempty" yaml:"json_schema,omitempty"`
}
