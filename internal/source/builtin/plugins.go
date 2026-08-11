package builtin

import (
	"github.com/theoden9014/agentmetry/internal/source/claude"
	"github.com/theoden9014/agentmetry/internal/source/codex"
	source "github.com/theoden9014/agentmetry/sourceplugin"
)

// Registry returns the source plugins compiled into the standard distribution.
func Registry() source.Registry {
	return source.NewRegistry(claude.New(), codex.New())
}
