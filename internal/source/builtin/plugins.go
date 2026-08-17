package builtin

import (
	"github.com/kotokumu/agentmetry/internal/source/claude"
	"github.com/kotokumu/agentmetry/internal/source/codex"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

// Registry returns the source plugins compiled into the standard distribution.
func Registry() source.Registry {
	return source.NewRegistry(claude.New(), codex.New())
}
