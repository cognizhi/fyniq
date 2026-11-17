import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { noViolations } from "../test/axe";
import { collect, initialValues, ManifestFields, type ManifestValues } from "./ManifestFields";

const schema = {
  required: ["base_url"],
  properties: {
    base_url: { type: "string", title: "Base URL", description: "Where the service answers.", default: "https://api.example.test" },
    region: { type: "string", title: "Region", enum: ["sg", "us"] },
    timeout: { type: "integer", title: "Timeout", default: 30 },
    sandbox: { type: "boolean", title: "Sandbox", description: "Send everything to the test environment." },
    headers: { type: "object", title: "Headers" },
    secret_ref: { type: "string", title: "Secret" },
  },
};

function Form({ bad = {}, seen }: { bad?: Record<string, string>; seen: (v: ManifestValues) => void }) {
  const [values, setValues] = useState(() => initialValues(schema, { sandbox: false, headers: { a: 1 } }));
  return (
    <form>
      <ManifestFields
        schema={schema}
        values={values}
        bad={bad}
        skip={["secret_ref"]}
        onChange={(v) => {
          setValues(v);
          seen(v);
        }}
      />
    </form>
  );
}

// description reads the text of the elements an element's aria-describedby names, as a screen reader would.
function description(el: HTMLElement): string {
  return (el.getAttribute("aria-describedby") ?? "")
    .split(" ")
    .map((id) => document.getElementById(id)?.textContent ?? "")
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}

// req: FR-INT-25
describe("a connector manifest's fields", () => {
  it("draw one field per property by its type, with its title, description and default; axe finds nothing", async () => {
    let last: ManifestValues = {};
    render(<Form seen={(v) => (last = v)} />);
    const url = screen.getByRole<HTMLInputElement>("textbox", { name: "Base URL" });
    expect(url.required).toBe(true);
    expect(description(url)).toBe("Where the service answers. Leave it empty for https://api.example.test.");
    expect(screen.getByRole("combobox", { name: "Region" })).toBeTruthy();
    expect(description(screen.getByRole("spinbutton", { name: "Timeout" }))).toBe("Leave it empty for 30.");
    expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "Headers" }).value).toBe('{\n  "a": 1\n}');
    expect(screen.queryByRole("textbox", { name: "Secret" })).toBeNull();

    const sandbox = screen.getByRole("switch", { name: "Sandbox" });
    expect(sandbox.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(sandbox);
    expect(last.sandbox).toBe(true);
    fireEvent.change(url, { target: { value: " https://svc.test " } });
    expect(last.base_url).toBe(" https://svc.test ");
    await noViolations(document.body);
  });

  it("mark a field wrong with its message", () => {
    render(<Form bad={{ headers: "Headers is not valid JSON." }} seen={() => {}} />);
    const headers = screen.getByRole("textbox", { name: "Headers" });
    expect(headers.getAttribute("aria-invalid")).toBe("true");
    expect(description(headers)).toBe("Headers is not valid JSON.");
  });

  it("read back into a configuration: an empty field left out, JSON parsed, a bad one named", () => {
    expect(collect(schema, { base_url: " https://svc.test ", region: "", timeout: "45", sandbox: true, headers: '{"a":1}' })).toEqual({
      out: { base_url: "https://svc.test", timeout: 45, sandbox: true, headers: { a: 1 } },
      errs: {},
    });
    expect(collect(schema, { headers: "{a:" }).errs).toEqual({ headers: "Headers is not valid JSON. Check the brackets, quotes and commas, then save again." });
  });
});
