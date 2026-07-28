package probes

type Config struct {
	HTTP *HTTPConfig `json:"http,omitempty"`
	TCP  *TCPConfig  `json:"tcp,omitempty"`

	InitialDelayMilliseconds int64 `json:"initialDelayMilliseconds,omitempty"`
	PeriodMilliseconds       int64 `json:"periodMilliseconds,omitempty"`
	TimeoutMilliseconds      int64 `json:"timeoutMilliseconds,omitempty"`
	SuccessThreshold         int32 `json:"successThreshold,omitempty"`
	FailureThreshold         int32 `json:"failureThreshold,omitempty"`
}

type HTTPConfig struct {
	Port   int32  `json:"port,omitempty"`
	Path   string `json:"path,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

type TCPConfig struct {
	Port int32 `json:"port,omitempty"`
}
