-- The assistant's hiring drafts (PRD-002 NM-AI-05, rev 0.76): a draft of a job post and a draft of an interview kit, each attached to a requisition by the executor after a person approves it. The executor holds
-- no grant on the requisition table (it carries the salary range): it learns only the requisition's state, through one narrow function, to refuse a draft for a requisition that is closed.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.ai_draft DROP CONSTRAINT ai_draft_kind;
ALTER TABLE nemura.ai_draft ADD CONSTRAINT ai_draft_kind CHECK (kind IN ('letter', 'flex_reply', 'job_post', 'interview_kit')) NOT VALID;

-- An Owner may now turn the two kinds of draft on, like the others: closed by default.
ALTER TABLE nemura.ai_settings DROP CONSTRAINT ai_settings_task_types_known;
ALTER TABLE nemura.ai_settings ADD CONSTRAINT ai_settings_task_types_known CHECK (task_types <@ ARRAY['people.draft_letter', 'people.draft_fwa_reply', 'people.draft_job_post', 'people.draft_interview_kit']::text[]) NOT VALID;

ALTER TABLE nemura.ai_proposal DROP CONSTRAINT ai_proposal_task_known;
ALTER TABLE nemura.ai_proposal ADD CONSTRAINT ai_proposal_task_known CHECK (task_type IN ('people.draft_letter', 'people.draft_fwa_reply', 'people.draft_job_post', 'people.draft_interview_kit')) NOT VALID;

-- +goose StatementBegin
CREATE FUNCTION nemura.requisition_state_for_draft(p_id uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT r.state FROM nemura.requisition r WHERE r.organisation_id = nemura.current_org() AND r.id = p_id
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.requisition_state_for_draft(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.requisition_state_for_draft(uuid) TO nemura_executor;
