module github.com/cofy-x/axern/apps/axrun

go 1.25.12

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/llmproxy => ../../lib/go/llmproxy

replace github.com/cofy-x/axern/lib/go/agentprofile => ../../lib/go/agentprofile

replace github.com/cofy-x/axern/lib/go/agentbundle => ../../lib/go/agentbundle

replace github.com/cofy-x/axern/lib/go/clientconfig => ../../lib/go/clientconfig

require (
	github.com/cofy-x/axern/lib/go/agentbundle v0.0.0
	github.com/cofy-x/axern/lib/go/agentprofile v0.0.0
	github.com/cofy-x/axern/lib/go/clientconfig v0.0.0
	github.com/cofy-x/axern/lib/go/llmproxy v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/google/go-containerregistry v0.20.7
	github.com/google/uuid v1.6.0
	github.com/spf13/cobra v1.10.2
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/containerd/stargz-snapshotter/estargz v0.18.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/cli v29.5.3+incompatible // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/vbatts/tar-split v0.12.2 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
