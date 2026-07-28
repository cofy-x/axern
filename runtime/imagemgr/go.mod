module github.com/cofy-x/axern/runtime/imagemgr

go 1.25.12

require (
	github.com/avast/retry-go/v4 v4.7.0
	github.com/cofy-x/axern/lib/go/imageref v0.0.0
	github.com/google/go-containerregistry v0.20.7
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/moby/sys/mountinfo v0.7.2
	github.com/sirupsen/logrus v1.9.4
	go.etcd.io/bbolt v1.4.3
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	golang.org/x/sync v0.21.0
	golang.org/x/sys v0.46.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.19.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cofy-x/axern/lib/go/observability v0.0.0
	github.com/containerd/stargz-snapshotter/estargz v0.18.1 // indirect
	github.com/docker/cli v29.5.3+incompatible // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/vbatts/tar-split v0.12.2 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/observability => ../../lib/go/observability

replace github.com/cofy-x/axern/lib/go/imageref => ../../lib/go/imageref
