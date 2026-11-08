import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Archive } from "lucide-react";
import { api } from "../../api/client";
import { date } from "../../format/date";
import { Icon } from "../../ui";

// auditReaders are the roles that view the audit trail (Appendix D's "Audit trail"); only they read the purges.
export const auditReaders = ["owner", "administrator", "controller", "accountant", "auditor", "tax_agent", "dpo"];

// PurgedPostings tells a record's page that the ledger's purge after retention removed its postings (AUD-10): the
// record and its evidence are kept, and its balance is in the carry-forward dated the day before the boundary. It
// shows nothing while none was purged, or when the purges cannot be read.
export function PurgedPostings({ recordId }: { recordId: string }) {
  const gone = useQuery({
    queryKey: ["audit-purges", recordId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/audit/purges", { params: { query: { record_id: recordId } } });
      if (error) throw error;
      return data.postings ?? [];
    },
  });
  const postings = gone.data ?? [];
  const last = postings.at(-1);
  if (!last) return null;
  const before = new Date(`${last.boundary}T00:00:00Z`);
  before.setUTCDate(before.getUTCDate() - 1);
  return (
    <div className="fy-banner fy-banner--info" role="note">
      <Icon of={Archive} size={20} />
      <div className="fy-banner__text">
        <strong>Its postings were purged after their retention</strong>
        <span className="fy-small">
          The ledger before {date(last.boundary)} was carried forward: {postings.length === 1 ? "its posting" : `its ${postings.length} postings`} of{" "}
          {postings.map((p) => date(p.posting_date)).join(", ")} went, and their balance is in the carry-forward of {date(before.toISOString().slice(0, 10))}. The record and its
          evidence are kept.
        </span>
      </div>
      <div className="fy-banner__action">
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/compliance/audit-trail">
          Purges and restores
        </Link>
      </div>
    </div>
  );
}
