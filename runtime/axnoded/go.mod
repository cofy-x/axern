module github.com/cofy-x/axern/runtime/axnoded

go 1.25.12

replace github.com/cofy-x/axern/network/bpfnet => ../../network/bpfnet

replace github.com/cofy-x/axern/lib/go/agentbundle => ../../lib/go/agentbundle

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/nodecapability => ../../lib/go/nodecapability

require (
	github.com/cofy-x/axern/lib/go/agentbundle v0.0.0
	github.com/cofy-x/axern/lib/go/nodecapability v0.0.0
	github.com/cofy-x/axern/network/bpfnet v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/containerd/cgroups/v3 v3.0.1
	github.com/coreos/go-iptables v0.6.0
	github.com/creack/pty v1.1.24
	github.com/google/uuid v1.6.0
	github.com/opencontainers/runtime-spec v1.1.0-rc.1
	github.com/orcaman/concurrent-map/v2 v2.0.1
	github.com/pelletier/go-toml v1.9.5
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.11.1
	github.com/tchap/go-patricia/v2 v2.3.1
	github.com/urfave/cli v1.22.17
	github.com/vishvananda/netlink v1.2.1-beta.2
	go.etcd.io/bbolt v1.4.3
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	golang.org/x/net v0.57.0
	golang.org/x/sys v0.47.0
	golang.org/x/term v0.45.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require go.opentelemetry.io/contrib/instrumentation/runtime v0.68.0 // indirect

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cilium/ebpf v0.22.0 // indirect
	github.com/cofy-x/axern/lib/go/grpcclient v0.0.0
	github.com/cofy-x/axern/lib/go/llmproxy v0.0.0
	github.com/cofy-x/axern/lib/go/observability v0.0.0
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/vishvananda/netns v0.0.0-20210104183010-2eb08e3e575f // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.19.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/cofy-x/axern/lib/go/grpcclient => ../../lib/go/grpcclient

replace github.com/cofy-x/axern/lib/go/llmproxy => ../../lib/go/llmproxy

replace github.com/cofy-x/axern/lib/go/observability => ../../lib/go/observability
