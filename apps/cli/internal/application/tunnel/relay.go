package tunnel

type RelayDialConfig struct {
	Insecure   bool
	CACert     string
	Cert       string
	Key        string
	ServerName string
	ProxyMode  string
}
