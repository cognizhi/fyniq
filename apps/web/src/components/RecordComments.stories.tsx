import type { Meta, StoryObj } from "@storybook/react-vite";
import { CommentsOnDemand } from "./RecordComments";

// The comments on a record, and a box to add one with @mentions (FR-UX-13). Closed, it reads nothing until it is asked for.
const meta = { title: "Shared/RecordComments", component: CommentsOnDemand } satisfies Meta<typeof CommentsOnDemand>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Closed: Story = { args: { recordType: "bill", recordId: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30" } };
