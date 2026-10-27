import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ReceiptText } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { date } from "../../format/date";
import { Icon, problemOf } from "../../ui";
import { money, StageChip, stages, type Stage } from "./claims";

type Whose = "waiting_for_me" | "everyone";

// ClaimsReview is Purchases › Expense claims (docs/design/screens ClaimsReview; FR-AP-17, FR-AP-07, FR-APR-01): the
// claims waiting for the person's approval, and every member's claims for those who see them all. Opening one shows its
// receipts, the expense policy's outcome and what approving it posts; it is approved or returned with a comment there,
// signed as any approval is. Nobody approves their own claim: the API refuses it, and the page offers it to no one.
export function ClaimsReview() {
  const [whose, setWhose] = useState<Whose>("waiting_for_me");
  const [stage, setStage] = useState<Stage | "">("");
  const list = useInfiniteQuery({
    queryKey: ["expense-claims", whose, stage],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/expense-claims", {
        params: { query: { whose, stage: stage === "scheduled" || stage === "" ? undefined : stage, page_size: 100, cursor: pageParam } },
      });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const claims = list.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
          </nav>
          <h1 className="fy-h1">Expense claims</h1>
          <p className="fy-secondary">Members' claims: those waiting for you, with what the expense policy says about each, and everyone's.</p>
        </div>
      </div>
      <nav className="fy-tabs" aria-label="Claim lists">
        <button type="button" aria-current={whose === "waiting_for_me" ? "page" : undefined} onClick={() => setWhose("waiting_for_me")}>
          To approve
        </button>
        <button type="button" aria-current={whose === "everyone" ? "page" : undefined} onClick={() => setWhose("everyone")}>
          All claims
        </button>
      </nav>
      {whose === "everyone" && (
        <div className="fy-chipset" role="group" aria-label="Filter by stage">
          <button className="fy-filter" type="button" aria-pressed={stage === ""} onClick={() => setStage("")}>
            All
          </button>
          {stages
            .filter((s) => s.stage !== "scheduled")
            .map((s) => (
              <button key={s.stage} className="fy-filter" type="button" aria-pressed={stage === s.stage} onClick={() => setStage(s.stage)}>
                {s.label}
              </button>
            ))}
        </div>
      )}
      {list.isError && <Refusal problem={problemOf(list.error)} title="The claims could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the claims…</p>}
      {list.data && claims.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={ReceiptText} size={24} />
          <h2 className="fy-h3">{whose === "waiting_for_me" ? "Nothing waits for you" : "No claims"}</h2>
          <p className="fy-secondary">{whose === "waiting_for_me" ? "A member's claim shows here when it waits for your approval." : "Members' claims show here once they save them."}</p>
        </div>
      )}
      {claims.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label={whose === "waiting_for_me" ? "Claims waiting for you" : "All claims"}>
            <thead>
              <tr>
                <th scope="col">Claim</th>
                <th scope="col">Claimant</th>
                <th scope="col">Date</th>
                <th scope="col">Stage</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
              </tr>
            </thead>
            <tbody>
              {claims.map((c) => (
                <tr key={c.id}>
                  <td data-card="title">
                    <Link to="/purchases/claims/$claimId" params={{ claimId: c.id }}>
                      {c.title || c.number || "Claim"}
                    </Link>
                    {c.number && <span className="fy-small fy-muted fy-mono block">{c.number}</span>}
                  </td>
                  <td data-card="meta">{c.claimant.name}</td>
                  <td data-card="meta">{date(c.date)}</td>
                  <td data-card="status">
                    <StageChip stage={c.stage} />
                  </td>
                  <td data-card="amount" className="fy-num">
                    {c.total ? money(c.total) : "Not complete"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && (
        <div>
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Show more
          </button>
        </div>
      )}
    </div>
  );
}
