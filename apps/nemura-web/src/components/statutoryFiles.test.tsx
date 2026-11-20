import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer, bodies, calls } from "../test/setup";
import { StatutoryFiles } from "./StatutoryFiles";

vi.mock("../api/stepup", () => ({ confirmWithPasskey: async () => undefined }));

const rid = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43";
const file = { id: "i", kind: "ais", version: 1, wage_month: "2026-06-01", lines: 1, total: "4500.00", sha256: "ab".repeat(32), note: "n", skipped: [], content_base64: btoa("{}"), file_name: "ir8a-2026.json" };

// req: NM-PAY-13
// req: NM-PAY-14
describe("the statutory files", () => {
  it("asks the employer for its own CPF Submission Number parts and IRAS contact and sends them with the kind, never a guess", async () => {
    URL.createObjectURL = () => "blob:x";
    HTMLAnchorElement.prototype.click = () => undefined;
    answer(`POST /v1/payroll/runs/${rid}/exports`, { status: 200, body: file });
    const done = vi.fn();
    render(<StatutoryFiles run={rid} version={1} onDone={done} />);
    fireEvent.change(screen.getByLabelText(/Payment type of your CPF/), { target: { value: "PTE" } });
    fireEvent.click(screen.getByRole("button", { name: "Make the CPF file" }));
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1));
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/exports`)).toEqual({ version: 1, kind: "cpf", payment_type: "PTE", serial: "01", advice_code: "01" });
    fireEvent.change(screen.getByLabelText("Person authorised to file"), { target: { value: "John Adams" } });
    fireEvent.change(screen.getByLabelText(/Their contact number/), { target: { value: "91234567" } });
    fireEvent.change(screen.getByLabelText("Their email address"), { target: { value: "abc@email.com.sg" } });
    fireEvent.click(screen.getByRole("button", { name: "Make the IR8A file" }));
    await waitFor(() => expect(done).toHaveBeenCalledTimes(2));
    expect(bodies.get(`POST /v1/payroll/runs/${rid}/exports`)).toEqual({ version: 1, kind: "ais", authorised_person: "John Adams", contact_number: "91234567", email: "abc@email.com.sg", submission_type: "O" });
    expect(done.mock.calls[1]?.[0]).toMatchObject({ ok: true });
    expect(calls.filter((c) => c.endsWith("/exports")).length).toBe(2);
  });
});
