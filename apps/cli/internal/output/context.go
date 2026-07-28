package output

import "io"

type ContextListRow struct {
	Active      bool
	Name        string
	Endpoint    string
	ServiceURL  string
	SSHEndpoint string
	ProxyMode   string
}

func RenderContextTable(w io.Writer, contexts []ContextListRow) {
	rows := make([][]string, 0, len(contexts))
	for _, context := range contexts {
		active := ""
		if context.Active {
			active = "*"
		}
		rows = append(rows, []string{
			active,
			context.Name,
			firstNonEmpty(context.Endpoint, "-"),
			firstNonEmpty(context.ServiceURL, "-"),
			firstNonEmpty(context.SSHEndpoint, "-"),
			firstNonEmpty(context.ProxyMode, "env"),
		})
	}
	RenderTable(w, []string{"ACTIVE", "NAME", "ENDPOINT", "SERVICE_URL", "SSH_ENDPOINT", "PROXY_MODE"}, rows)
}
