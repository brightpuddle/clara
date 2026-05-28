package sdk

import (
	"github.com/hashicorp/go-plugin"
)

// Serve starts the actuator binary's plugin server. Call this from main().
//
// Example:
//
//	func main() {
//	    sdk.Serve(&MyActuator{})
//	}
func Serve(impl Actuator) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"actuator": &ActuatorPlugin{Impl: impl},
		},
	})
}
