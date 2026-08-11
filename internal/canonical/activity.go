package canonical

import "strings"

func DeriveActivity(name string, attributes map[string]any) (ActivityKind, string, string, string) {
	toolName := firstString(attributes, "gen_ai.tool.name", "tool.name")
	targetAgentID := firstString(attributes, "gen_ai.agent.target.id")
	content := firstString(attributes, "content", "message", "prompt", "input", "response", "output", "task")

	combined := strings.ToLower(name + " " + toolName)
	switch {
	case strings.Contains(combined, "spawn_agent"), strings.Contains(combined, "followup_task"), strings.Contains(combined, "delegation"):
		return ActivityDelegation, toolName, targetAgentID, content
	case strings.Contains(combined, "send_message"), strings.Contains(combined, "agent_message"):
		return ActivityMessage, toolName, targetAgentID, content
	case strings.Contains(combined, "user_prompt"), strings.Contains(combined, "prompt"):
		return ActivityPrompt, toolName, targetAgentID, content
	case strings.Contains(combined, "reasoning"):
		return ActivityReasoning, toolName, targetAgentID, content
	case strings.Contains(combined, "response"), strings.Contains(combined, "assistant"):
		return ActivityResponse, toolName, targetAgentID, content
	case strings.Contains(combined, "model.request"), strings.Contains(combined, "llm_request"):
		return ActivityResponse, toolName, targetAgentID, content
	case toolName != "", strings.Contains(combined, "tool"):
		return ActivityTool, toolName, targetAgentID, content
	default:
		return ActivityUnknown, toolName, targetAgentID, content
	}
}

func DeriveTargetAgentType(attributes map[string]any) string {
	return firstString(attributes, "gen_ai.agent.target.type")
}
