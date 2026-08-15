package canonical

// IsSemanticSpan reports whether a span carries explicit evidence used by the
// Agentmetry product. Incidental runtime instrumentation remains in the lossless
// journal and can be replayed later, but is not materialized into query tables.
func IsSemanticSpan(span Span) bool {
	if IsSemanticEventName(span.Name) || span.ToolName != "" || span.TargetAgentID != "" || span.TargetAgentType != "" || span.Content != "" || span.CostUSD != nil {
		return true
	}
	agent := span.Agent
	return agent.RunID != "" || agent.AgentID != "" || agent.AgentDefinition != "" || agent.AgentType != "" || agent.ParentAgentID != "" || agent.Model != "" || agent.Tokens.AnyReported()
}

func IsSemanticEventName(name string) bool {
	switch name {
	case "gen_ai.response.completed",
		"gen_ai.user_prompt",
		"gen_ai.model.request",
		"gen_ai.model.request.trace",
		"gen_ai.model.error",
		"gen_ai.agent.delegation",
		"gen_ai.agent.message",
		"gen_ai.tool",
		"gen_ai.tool.call",
		"gen_ai.tool_result",
		"gen_ai.tool.result":
		return true
	default:
		return false
	}
}
