package domain

type PromptSourceType string

const (
	PromptSourceInstruction PromptSourceType = "instruction"
	PromptSourceInline      PromptSourceType = "inline"
	PromptSourceTemplate    PromptSourceType = "template"
)

type PromptSpec struct {
	Source       PromptSourceType  `json:"source,omitempty"`
	TemplatePath string            `json:"template_path,omitempty"`
	Inline       string            `json:"inline,omitempty"`
	Rounds       []PromptRoundSpec `json:"rounds,omitempty"`
}

type PromptRoundSpec struct {
	Index             int              `json:"index"`
	Source            PromptSourceType `json:"source,omitempty"`
	TemplatePath      string           `json:"template_path,omitempty"`
	Inline            string           `json:"inline,omitempty"`
	RenderedPromptRef string           `json:"rendered_prompt_ref,omitempty"`
	SessionID         string           `json:"session_id,omitempty"`
	ResumePrevious    bool             `json:"resume_previous,omitempty"`
}

type AgentSessionMode string

const (
	AgentSessionModeNone       AgentSessionMode = "none"
	AgentSessionModeCreate     AgentSessionMode = "create"
	AgentSessionModeResume     AgentSessionMode = "resume"
	AgentSessionModeMultiRound AgentSessionMode = "multi_round"
)

type AgentSessionSpec struct {
	Mode      AgentSessionMode `json:"mode,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
}
