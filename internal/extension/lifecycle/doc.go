// Package lifecycle orchestrates approve/deny/revoke/start/stop/restart
// for hosted extensions on top of host.Manager, host.Gate, and
// host.LaunchHosted. The Service interface is the programmatic surface
// the TUI (and eventually piapi.API.Extensions) consume.
package lifecycle
