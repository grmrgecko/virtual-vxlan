package main

// Basic application info.
const (
	serviceName        = "virtual-vxlan"
	serviceDisplayName = "Virtual VXLAN"
	serviceVendor      = "com.mrgeckosmedia"
	serviceDescription = "Virtual VXLAN using TUN interfaces"
	serviceVersion     = "0.2.2"
	defaultConfigFile  = "config.yaml"
)

// The application start.
func main() {
	// Parse the flags.
	ctx := ParseFlags()

	// Configure logging.
	flags.Log.Apply()

	// Run the command and exit.
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
