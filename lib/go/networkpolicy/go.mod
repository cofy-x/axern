module github.com/cofy-x/axern/lib/go/networkpolicy

go 1.26.10

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	golang.org/x/net v0.62.0
	google.golang.org/protobuf v1.36.12
)

require golang.org/x/text v0.43.0 // indirect

replace github.com/cofy-x/axern/sdk/go => ../../../sdk/go
