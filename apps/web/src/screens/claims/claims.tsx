import { useQuery } from "@tanstack/react-query";
import { CalendarClock, CircleCheck, PencilLine, Send, TriangleAlert } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { Icon } from "../../ui";

// What the claims screens share (FR-AP-14…18; p1-expense-claims tasks 8.1, 8.2): a claim's stage as a chip, the
// expense policy's categories, the GST codes a receipt takes, and how an amount reads.

export type Claim = components["schemas"]["ExpenseClaim"];
export type ClaimLine = components["schemas"]["ExpenseClaimLine"];
export type Stage = Claim["stage"];
export type Category = components["schemas"]["ClaimCategory"];
export type CardLine = components["schemas"]["CardLine"];
export type Advance = components["schemas"]["CashAdvance"];
type Money = components["schemas"]["Money"];

// stages are a claim's stages in the order a claim moves through them, with how each reads (FR-AP-14): each chip
// carries its icon and its words, so colour never says it alone.
export const stages: { stage: Stage; label: string }[] = [
  { stage: "returned", label: "Returned" },
  { stage: "draft", label: "Draft" },
  { stage: "submitted", label: "Submitted" },
  { stage: "approved", label: "Approved" },
  { stage: "scheduled", label: "Scheduled" },
  { stage: "paid", label: "Paid" },
];

export function StageChip({ stage }: { stage: Stage }) {
  const label = stages.find((s) => s.stage === stage)?.label ?? stage;
  switch (stage) {
    case "returned":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          {label}
        </span>
      );
    case "draft":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PencilLine} />
          {label}
        </span>
      );
    case "submitted":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Send} />
          {label}
        </span>
      );
    case "scheduled":
      return (
        <span className="fy-chip fy-chip--info">
          <Icon of={CalendarClock} />
          {label}
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          {label}
        </span>
      );
  }
}

// money reads an amount with its currency: "27.30 SGD".
export function money(m: Money | undefined | null): string {
  return m ? `${formatAmount(m.amount)} ${m.currency}` : "";
}

// useCategories are the expense policy's categories (FR-AP-15), those in use first.
export function useCategories() {
  return useQuery({
    queryKey: ["claim-categories"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/claim-categories", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data.items;
    },
  });
}

// useClaimGSTCodes are the GST codes a receipt dated on takes (FR-AP-20), read as a bill's are.
export function useClaimGSTCodes(on: string) {
  return useQuery({
    queryKey: ["claim-gst-codes", on],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/expense-claims/gst-codes", { params: { query: { on: on || undefined } } });
      if (error) throw error;
      return data;
    },
  });
}
