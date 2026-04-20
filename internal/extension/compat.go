package extension

import (
	"github.com/pizzaface/go-pi/internal/extension/loader"
)

// SlashCommand is re-exported from the loader package so legacy TUI
// callers continue to compile.
type SlashCommand = loader.SlashCommand

// ResourceDirs is re-exported for legacy TUI callers.
type ResourceDirs = loader.ResourceDirs

// DiscoverResourceDirs re-exports the loader helper.
func DiscoverResourceDirs(workDir string) loader.ResourceDirs {
	return loader.DiscoverResourceDirs(workDir)
}

// DefaultBuiltinSlashCommands returns the canonical list of built-in TUI
// slash-command names. This intentionally returns a []string (not
// []SlashCommand) to match the legacy shape used by InputModel's
// slashCommands variable.
func DefaultBuiltinSlashCommands() []string {
	return []string{
		"/help",
		"/clear",
		"/model",
		"/effort",
		"/session",
		"/new",
		"/resume",
		"/fork",
		"/tree",
		"/settings",
		"/extensions",
		"/context",
		"/branch",
		"/compact",
		"/history",
		"/login",
		"/theme",
		"/skills",
		"/skill-create",
		"/skill-load",
		"/skill-list",
		"/ping",
		"/debug",
		"/restart",
		"/exit",
		"/quit",
	}
}
