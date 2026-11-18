import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { initialValues, ManifestFields } from "./ManifestFields";

// A form drawn from a connector manifest's configuration schema (FR-INT-25): text, a choice, a number, a switch and
// JSON, each with its title, its description and its default.
const schema = {
  required: ["base_url"],
  properties: {
    base_url: { type: "string", title: "Base URL", description: "Where the service answers.", default: "https://api.example.test" },
    region: { type: "string", title: "Region", description: "The service's region.", enum: ["sg", "us"] },
    timeout: { type: "integer", title: "Timeout", description: "Seconds to wait for an answer.", default: 30 },
    sandbox: { type: "boolean", title: "Sandbox", description: "Send everything to the service's test environment." },
    headers: { type: "object", title: "Headers", description: "Extra headers, as JSON." },
  },
};

function Form({ bad }: { bad: Record<string, string> }) {
  const [values, setValues] = useState(() => initialValues(schema, { sandbox: true, headers: { "X-Team": "finance" } }));
  return (
    <form className="fy-stack fy-stack--4 max-w-[480px]">
      <ManifestFields schema={schema} values={values} onChange={setValues} bad={bad} />
    </form>
  );
}

const meta = { title: "Shared/ManifestFields", component: ManifestFields } satisfies Meta<typeof ManifestFields>;
export default meta;
type Story = StoryObj<typeof meta>;

const args = { schema, values: {}, onChange: () => {}, bad: {} };

export const Fields: Story = { args, render: () => <Form bad={{}} /> };

export const WithAnError: Story = {
  args,
  render: () => <Form bad={{ headers: "Headers is not valid JSON. Check the brackets, quotes and commas, then save again." }} />,
};
