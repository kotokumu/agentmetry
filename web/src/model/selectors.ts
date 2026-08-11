import type { Model, Overview, Session } from "./update";

export const selectedSession = (model: Model): Session | undefined => {
  if (model.requestedConversation) {
    return model.conversationStatus === "ready" ? model.routedConversation : undefined;
  }
  return model.overview?.sessions.find((session) => session.id === model.selectedSessionId && session.sourceId === model.selectedSessionSourceId);
};

export const observedActivityCount = (overview?: Overview): number =>
  overview?.sessions.reduce((total, session) => total + session.activityCount, 0) ?? 0;
