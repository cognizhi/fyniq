import type { Meta, StoryObj } from "@storybook/react-vite";
import { Refusal } from "./Refusal";

// A refusal of the API (ENG-23): what is wrong and how to fix it, with its code, and each field by name.
const meta = { title: "Shared/Refusal", component: Refusal } satisfies Meta<typeof Refusal>;
export default meta;
type Story = StoryObj<typeof meta>;

export const OneProblem: Story = {
  args: { title: "The journal could not be posted", problem: { code: "JOURNAL-02", detail: "The journal is posted, not a draft", fix: "Reverse it instead" } },
};

export const FieldProblems: Story = {
  args: {
    title: "The export could not be asked for",
    problem: {
      code: "API-INPUT-01",
      detail: "The request has 2 problems",
      fix: "Correct the fields below",
      errors: [
        { field: "body.to", message: "is not a date", fix: "Send the day as YYYY-MM-DD" },
        { field: "body.lines[0].account_id", message: "the account is a group, which takes no lines.", fix: "Choose one of its accounts" },
      ],
    },
  },
};
