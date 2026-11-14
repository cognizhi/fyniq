import { createContext, useContext } from "react";
import type { TurnContext } from "./assistant";

// AssistantContext opens the assistant panel from any page, with what the person asks about (FR-UX-17): "Ask about this"
// on a record, a report cell or a figure.
export const AssistantContext = createContext<{ openWith: (context?: TurnContext) => void } | null>(null);

// useAssistant is the shell's assistant panel, or nothing outside the signed-in frame.
export function useAssistant() {
  return useContext(AssistantContext);
}
