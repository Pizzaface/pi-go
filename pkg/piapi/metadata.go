package piapi

import (
	"fmt"
	"regexp"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var capRe = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)

// Metadata describes a single extension.
type Metadata struct {
	Name                  string
	Version               string
	Description           string
	Prompt                string
	RequestedCapabilities []string
	Entry                 string
}

// Validate returns a non-nil error if the metadata is incomplete or
// malformed. Called at registration time for compiled-in extensions and
// at handshake time for hosted ones.
func (m Metadata) Validate() error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("piapi: invalid name %q (must match %s)", m.Name, nameRe)
	}
	if m.Version == "" {
		return fmt.Errorf("piapi: version is required")
	}
	for _, cap := range m.RequestedCapabilities {
		if !capRe.MatchString(cap) {
			return fmt.Errorf("piapi: malformed capability %q (must be service.method)", cap)
		}
	}
	return nil
}
