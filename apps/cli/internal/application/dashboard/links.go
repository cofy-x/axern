package dashboard

import "strings"

func BuildLinks(cfg LinksConfig) Links {
	out := Links{ContextName: strings.TrimSpace(cfg.ContextName)}
	if target := normalizeHTTPURL(cfg.ServiceURL); target != "" {
		out.Links = append(out.Links, Link{
			Name: "Gateway terminal dashboard",
			URL:  strings.TrimRight(target, "/") + "/dashboard?token=axern-local-dev",
			Kind: "gateway",
		})
	}
	out.Links = append(out.Links,
		Link{Name: "Grafana LGTM compose default", URL: "http://127.0.0.1:13000", Kind: "observability"},
		Link{Name: "Grafana LGTM kind default", URL: "http://127.0.0.1:13001", Kind: "observability"},
	)
	return out
}

func normalizeHTTPURL(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	return "http://" + target
}
