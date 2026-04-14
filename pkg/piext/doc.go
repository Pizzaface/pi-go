// Package piext is the hosted-Go SDK for pi-go extensions.
//
// A hosted-Go extension is a separate Go binary that pi-go spawns over
// stdio and talks to via JSON-RPC v2.1. From the extension author's
// perspective the shape is identical to a compiled-in extension:
//
//	func main() {
//	    piext.Run(Metadata, func(pi piapi.API) error {
//	        pi.RegisterTool(...)
//	        return nil
//	    })
//	}
//
// piext.Run handles the stdio wiring, handshake, and backs the piapi.API
// implementation with a transport client.
package piext
