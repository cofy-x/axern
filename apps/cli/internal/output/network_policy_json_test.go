package output

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestRunJSONPreservesAdmittedNetworkPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		network *commonv1.NetworkSpec
		want    string
	}{
		{"unrestricted", &commonv1.NetworkSpec{}, `{"mode":"unspecified"}`},
		{"isolated", &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED}, `{"mode":"isolated"}`},
		{"dns deny", &commonv1.NetworkSpec{EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{
			DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"blocked.example"}},
		}}}, `{"mode":"unspecified","egress_policy":{"dns_deny":{"denied_domains":["blocked.example"]}}}`},
		{"strict", &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{
			Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{"allowed.example"}, AllowedCidrs: []*commonv1.CIDREgressRule{
				{Cidr: "198.19.0.2/32", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 80, End: 81}}},
				{Cidr: "198.19.0.3/32", Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP, Ports: []*commonv1.PortRange{{Start: 53, End: 53}}},
			}},
		}}}, `{"mode":"default","egress_policy":{"strict":{"allowed_domains":["allowed.example"],"allowed_cidrs":[{"cidr":"198.19.0.2/32","protocol":"tcp","ports":[{"start":80,"end":81}]},{"cidr":"198.19.0.3/32","protocol":"udp","ports":[{"start":53,"end":53}]}]}}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			if err := PrintRunResponseJSON(&output, &runv1.Run{ID: "run-network", Config: &commonv1.ExecutionConfig{Network: test.network}}); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Run struct {
					Config struct {
						Network map[string]any `json:"network"`
					} `json:"config"`
				} `json:"run"`
			}
			if err := json.Unmarshal([]byte(output.String()), &got); err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Run.Config.Network, want) {
				t.Fatalf("network JSON = %#v, want %#v", got.Run.Config.Network, want)
			}
			assertNoProtoJSONLeak(t, output.String())
		})
	}
}
