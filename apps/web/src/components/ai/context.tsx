import { createContext, useContext } from "react";
import type { TurnContext } from "./assistant";

// AssistantContext opens the assistant panel from any page, with what the person asks about (FR-UX-17): "Ask about this"
// on a record, a report or a figure. enabled is the organisation's AI switch as the session gives it.
export const AssistantContext = createContext<{ openWith: (context?: TurnContext) => void; enabled: boolean } | null>(null);

// useAssistant is the shell's assistant panel, or nothing outside the signed-in frame.
export function useAssistant() {
  return useContext(AssistantContext);
}
