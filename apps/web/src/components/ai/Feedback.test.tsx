import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { TurnFeedback } from "./Feedback";

// req: QA-AI-05
describe("rating an answer", () => {
  it("sends a thumbs down with its reason, once, and says who reviews it; axe finds nothing", async () => {
    const seen = answer({
      "POST /v1/assistant/conversations/c-1/turns/t-1/feedback": { status: 201, body: { id: "f-1", turn_id: "t-1", conversation_id: "c-1", person: "user:u",
        rating: "down", reason: "Wrong month.", state: "waiting", created_at: "2026-11-15T01:00:00Z" } },
    });
    const onRated = vi.fn();
    renderPage(() => <TurnFeedback conversation="c-1" turn="t-1" onRated={onRated} />);
    fireEvent.click(await screen.findByRole("button", { name: "Not helpful" }));
    expect((screen.getByRole("button", { name: "Send the rating" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("What was wrong with it?"), { target: { value: " Wrong month. " } });
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Send the rating" }));
    await waitFor(() => expect(onRated).toHaveBeenCalledWith({ id: "f-1", rating: "down", reason: "Wrong month.", state: "waiting" }));
    expect(seen.find((s) => s.path.endsWith("/feedback"))?.body).toEqual({ rating: "down", reason: "Wrong month." });
  });

  it("shows a rating already given, and how it stands", async () => {
    renderPage(() => <TurnFeedback conversation="c-1" turn="t-1" rated={{ id: "f-1", rating: "up", reason: "Clear", state: "golden" }} onRated={() => {}} />);
    expect(await screen.findByText(/You rated it helpful: it now helps test the assistant/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Helpful" })).toBeNull();
  });
});
