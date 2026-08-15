package axernsdk

var version = "0.5.1"

// Version returns the Go SDK version.
func Version() string {
	return version
}

// PlatformName returns the Axern platform name.
func PlatformName() string {
	return "axern"
}
