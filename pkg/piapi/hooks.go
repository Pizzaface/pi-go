package piapi

// HookConfig describes a single [[hooks]] entry from pi.toml. It declares a
// tool-backed lifecycle hook for an extension.
type HookConfig struct {
	Event    string   `toml:"event"    json:"event"`
	Command  string   `toml:"command"  json:"command"`
	Tools    []string `toml:"tools"    json:"tools"`
	Timeout  int      `toml:"timeout"  json:"timeout"`
	Critical bool     `toml:"critical" json:"critical"`
}
