module github.com/cofy-x/axern/lib/go/networkpolicy

go 1.25.12

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	golang.org/x/net v0.57.0
	google.golang.org/protobuf v1.36.11
)

require golang.org/x/text v0.40.0 // indirect

replace github.com/cofy-x/axern/sdk/go => ../../../sdk/go
