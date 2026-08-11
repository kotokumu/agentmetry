package builtin

import (
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/source/claude"
	"github.com/theoden9014/agentmetry/internal/source/codex"
)

// PlanUsageParser returns the account-usage parser compiled for a source.
// Source-specific payload knowledge stays behind this composition boundary.
func PlanUsageParser(id string) (planusage.Parser, bool) {
	parsers := []planusage.Parser{
		claude.NewPlanUsageParser(),
		codex.NewPlanUsageParser(),
	}
	for _, parser := range parsers {
		if parser.ID() == id {
			return parser, true
		}
	}
	return nil, false
}
