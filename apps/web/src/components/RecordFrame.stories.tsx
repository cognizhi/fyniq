import type { Meta, StoryObj } from "@storybook/react-vite";
import { RecordFrame, RecordHeader } from "./RecordFrame";

// The record page anatomy (experience/navigation): a header with the number, status, contact, key amounts and actions, body tabs
// and a collapsible right panel.
const meta = { title: "Shared/RecordFrame", component: RecordFrame, parameters: { layout: "fullscreen" } } satisfies Meta<typeof RecordFrame>;
export default meta;
type Story = StoryObj<typeof meta>;

const header = (
  <RecordHeader
    title="Bill BILL-0007"
    number="BILL-0007"
    status={<span className="fy-chip fy-chip--good">Posted</span>}
    contact="Atlassian Pty Ltd"
    amounts={[
      { label: "Total", value: "1,090.00 SGD" },
      { label: "Open", value: "1,090.00 SGD" },
    ]}
    primary={<button type="button" className="fy-btn fy-btn--primary">Record payment</button>}
    more={[{ label: "Void", onSelect: () => undefined }, { label: "Duplicate", onSelect: () => undefined }]}
  />
);

export const WithEveryTab: Story = {
  args: {
    header,
    tabs: {
      details: <p>The supplier, the dates and the terms.</p>,
      lines: <p>Each line with its account, GST code and amount.</p>,
      ledger: <p>Debit 6200 Office rent, credit 2010 Accounts payable.</p>,
      attachments: <p>The supplier's invoice, as a PDF.</p>,
      activity: <p>Prepared by Ah Kow, posted by Wei Ling, and the comments.</p>,
      related: <p>Its payments, and the credit notes against it.</p>,
    },
    panel: <p className="fy-card fy-card__body">Two checks pass. No warnings.</p>,
  },
};

export const WithoutAPanel: Story = {
  args: { header, tabs: { details: <p>A record with nothing at the side.</p>, activity: <p>Its comments.</p> } },
};
