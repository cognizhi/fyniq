import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AskAboutThis } from "./AskAboutThis";
import { AssistantContext } from "./context";

const invoice = { kind: "invoice", id: "i-1", label: "INV-0136 · Kopi Labs" } as const;

// req: FR-UX-17
describe("Ask about this", () => {
  it("opens the assistant with the record as its context, and is a person's action, without violet; axe finds nothing", async () => {
    const openWith = vi.fn();
    const onAsk = vi.fn();
    renderPage(() => (
      <AssistantContext.Provider value={{ openWith, enabled: true }}>
        <AskAboutThis context={invoice} onAsk={onAsk} />
      </AssistantContext.Provider>
    ));
    const ask = await screen.findByRole("button", { name: /Ask about this/ });
    expect(ask.className).not.toContain("ai");
    fireEvent.click(ask);
    expect(openWith).toHaveBeenCalledWith(invoice);
    expect(onAsk).toHaveBeenCalledOnce();
    await noViolations(document.body);
  });

  it("is not shown while the AI is off, nor outside the signed-in frame", async () => {
    renderPage(() => (
      <>
        <h1>Invoice</h1>
        <AssistantContext.Provider value={{ openWith: () => {}, enabled: false }}>
          <AskAboutThis context={invoice} />
        </AssistantContext.Provider>
        <AskAboutThis context={invoice} />
      </>
    ));
    await screen.findByRole("heading", { name: "Invoice" });
    expect(screen.queryByRole("button", { name: /Ask about this/ })).toBeNull();
  });
});
