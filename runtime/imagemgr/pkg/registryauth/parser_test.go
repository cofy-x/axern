package registryauth

import "testing"

func TestParseFlatFormat(t *testing.T) {
	data := []byte(`{
		"reg.antgroup-inc.cn": {"Auth":"host-auth"},
		"reg.antgroup-inc.cn/faas-swe/image": {"Auth":"repo-auth"}
	}`)

	auths, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := auths["reg.antgroup-inc.cn"].Auth; got != "host-auth" {
		t.Fatalf("auths[host].Auth = %q, want %q", got, "host-auth")
	}
	if got := auths["reg.antgroup-inc.cn/faas-swe/image"].Auth; got != "repo-auth" {
		t.Fatalf("auths[host/repo].Auth = %q, want %q", got, "repo-auth")
	}
}

func TestParseDockerAuthsFormat(t *testing.T) {
	data := []byte(`{
		"auths": {
			"reg.antgroup-inc.cn": {"auth":"host-auth"},
			"reg.antgroup-inc.cn/faas-swe/image": {"Auth":"repo-auth"}
		},
		"credsStore": "desktop"
	}`)

	auths, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := auths["reg.antgroup-inc.cn"].Auth; got != "host-auth" {
		t.Fatalf("auths[host].Auth = %q, want %q", got, "host-auth")
	}
	if got := auths["reg.antgroup-inc.cn/faas-swe/image"].Auth; got != "repo-auth" {
		t.Fatalf("auths[host/repo].Auth = %q, want %q", got, "repo-auth")
	}
}

func TestParseDockerURLKeyNormalization(t *testing.T) {
	data := []byte(`{
		"auths": {
			"https://reg.antgroup-inc.cn/v1/": {"auth":"host-auth"},
			"https://reg.antgroup-inc.cn/faas-swe/image/": {"auth":"repo-auth"}
		}
	}`)

	auths, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := auths["reg.antgroup-inc.cn"].Auth; got != "host-auth" {
		t.Fatalf("auths[normalized host].Auth = %q, want %q", got, "host-auth")
	}
	if got := auths["reg.antgroup-inc.cn/faas-swe/image"].Auth; got != "repo-auth" {
		t.Fatalf("auths[normalized repo].Auth = %q, want %q", got, "repo-auth")
	}
}

func TestConfigResolvePrefersRepositoryAuth(t *testing.T) {
	auths := Config{
		"registry.example":         {Auth: "host-auth"},
		"registry.example/ns/repo": {Auth: "repo-auth"},
	}
	if got := auths.Resolve("registry.example", "ns/repo"); got != "repo-auth" {
		t.Fatalf("Resolve() = %q, want repo-auth", got)
	}
	if got := auths.Resolve("registry.example", "other/repo"); got != "host-auth" {
		t.Fatalf("Resolve() fallback = %q, want host-auth", got)
	}
}
