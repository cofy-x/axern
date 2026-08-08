module github.com/cofy-x/axern/runtime/volumed

go 1.25.12

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
)
