type AgentIdentity = Readonly<{
  agentId?: string;
  agentDefinition?: string;
  agentType?: string;
}>;

export const agentDisplayLabel = (agent: AgentIdentity): string => {
  if (agent.agentDefinition) return agent.agentDefinition;
  if (agent.agentType && agent.agentType !== "root") return agent.agentType;
  if (agent.agentId === "main") return "Main agent";
  return agent.agentId || "Unnamed agent";
};
