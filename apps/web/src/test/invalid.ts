import { within } from "@testing-library/react";
import { expect } from "vitest";

// invalid checks that the field labelled label, in scope, is marked invalid and described by message, as every form's
// refusal of a field must be (QA-UI-01): the message sits beside the field and a screen reader reads it with it.
export async function invalid(scope: HTMLElement, label: string | RegExp, message: string): Promise<void> {
  const field = await within(scope).findByLabelText(label);
  expect(field.getAttribute("aria-invalid"), `${String(label)} is not marked invalid`).toBe("true");
  const described = (field.getAttribute("aria-describedby") ?? "").split(" ").map((x) => document.getElementById(x)?.textContent ?? "");
  expect(described, `${String(label)} is not described by its message`).toContain(message);
}
