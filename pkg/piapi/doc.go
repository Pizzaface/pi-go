// Package piapi defines the public types used by go-pi extensions.
//
// This package is imported by:
//   - host-side code in internal/extension to wire implementations,
//   - the hosted-Go SDK in pkg/piext to provide an RPC-backed API,
//   - external extension authors who compile against the interface.
//
// It declares no implementations and has no dependencies beyond the
// standard library, so external consumers can depend on it without
// pulling in the full go-pi host.
package piapi
