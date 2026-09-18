module github.com/cofy-x/axern/control/controld

go 1.26.8

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/nodecapability => ../../lib/go/nodecapability

replace github.com/cofy-x/axern/lib/go/memorybudget => ../../lib/go/memorybudget

replace github.com/cofy-x/axern/lib/go/networkpolicy => ../../lib/go/networkpolicy

replace github.com/cofy-x/axern/lib/go/executionlease => ../../lib/go/executionlease

require (
	github.com/cofy-x/axern/lib/go/executionlease v0.0.0
	github.com/cofy-x/axern/lib/go/imageref v0.0.0
	github.com/cofy-x/axern/lib/go/memorybudget v0.0.0
	github.com/cofy-x/axern/lib/go/networkpolicy v0.0.0
	github.com/cofy-x/axern/lib/go/nodecapability v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/google/go-containerregistry v0.20.7
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.9.2
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otel v1.46.0
	golang.org/x/crypto v0.57.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	go.opentelemetry.io/contrib/instrumentation/runtime v0.70.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cofy-x/axern/internal/proto v0.0.0
	github.com/cofy-x/axern/lib/go/grpcclient v0.0.0
	github.com/cofy-x/axern/lib/go/observability v0.0.0
	github.com/containerd/stargz-snapshotter/estargz v0.18.1 // indirect
	github.com/docker/cli v29.5.3+incompatible // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/vbatts/tar-split v0.12.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.70.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.21.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/log v0.21.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.21.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688
	gotest.tools/v3 v3.5.2 // indirect
)

replace github.com/cofy-x/axern/lib/go/grpcclient => ../../lib/go/grpcclient

replace github.com/cofy-x/axern/lib/go/imageref => ../../lib/go/imageref

replace github.com/cofy-x/axern/lib/go/observability => ../../lib/go/observability

replace github.com/cofy-x/axern/internal/proto => ../../internal/proto
