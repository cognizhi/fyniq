-- Grievances (PRD-002 NM-SEC-06): a confidential channel with an inquiry record, a written outcome and restricted access.
--
-- Who can read a grievance is decided here, not only in the API. A grievance has a complainant (the member who filed it), the people complained about (subject_people, who may have a login or not),
-- and a handler: another authorised person (an HR administrator or the Owner) who is neither. The tables are readable only by the complainant and the handler, and a RESTRICTIVE policy hides every
-- row from a member who is one of the people complained about, whatever role they hold: it asks the live person record, so a login given to a subject later is covered too. Every statement on these
-- tables runs for a named acting person (app.person_id): with none, nothing is visible and nothing can be written, so a code path that forgets to name one fails closed. No role but the API's holds
-- any privilege here: not the worker, the read-only role or the assistant.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Whether a member is one of the people complained about: the live person record decides, as the acting person's own tenancy lets them read it.
-- +goose StatementBegin
CREATE FUNCTION nemura.is_subject_member(subjects uuid[], who uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT who IS NOT NULL AND EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = nemura.current_org() AND p.id = ANY (subjects) AND p.member_id = who)
$$;
-- +goose StatementEnd

-- Whether a member holds one of the roles that may handle a grievance today, as an active member: an HR administrator or the Owner.
-- +goose StatementBegin
CREATE FUNCTION nemura.can_handle_grievance(who uuid, on_day date) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT EXISTS (
    SELECT 1 FROM nemura.role_assignment a JOIN nemura.member m ON m.organisation_id = a.organisation_id AND m.id = a.member_id
     WHERE a.organisation_id = nemura.current_org() AND a.member_id = who AND a.role IN ('hr_admin', 'owner') AND m.deactivated_at IS NULL
       AND a.effective_from <= on_day AND (a.effective_to IS NULL OR a.effective_to > on_day))
$$;
-- +goose StatementEnd

-- The day the acting person's work is dated: the transaction's own (app.on) or the database's.
-- +goose StatementBegin
CREATE FUNCTION nemura.acting_day() RETURNS date
LANGUAGE sql STABLE
AS $$
  SELECT coalesce(nullif(pg_catalog.current_setting('app.on', true), '')::date, current_date)
$$;
-- +goose StatementEnd

CREATE TABLE nemura.grievance (
  organisation_id    uuid        NOT NULL DEFAULT nemura.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  complainant_member uuid        NOT NULL,
  subject_people     uuid[]      NOT NULL DEFAULT '{}',   -- the people complained about; none when it is about a practice
  summary            text        NOT NULL,
  state              text        NOT NULL DEFAULT 'open',
  handler_member     uuid,                                -- null only while unrouted
  filed_at           timestamptz NOT NULL,
  routed_at          timestamptz,
  finding            text,
  outcome            text,
  decided_by         uuid,
  decided_at         timestamptz,
  handed_by          uuid,                                -- the handler who last passed it on, and the transaction that did: a handover must be able to read the row it just gave away, for that
  handover_xact      bigint,                              -- one transaction only (an UPDATE's new row has to pass the read policy), and the previous handler reads nothing after it
  CONSTRAINT grievance_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT grievance_complainant_fkey FOREIGN KEY (organisation_id, complainant_member) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT grievance_handler_fkey FOREIGN KEY (organisation_id, handler_member) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT grievance_decider_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT grievance_state_known CHECK (state IN ('unrouted', 'open', 'decided')),
  CONSTRAINT grievance_summary_length CHECK (length(btrim(summary)) BETWEEN 20 AND 4000),
  CONSTRAINT grievance_subjects_few CHECK (cardinality(subject_people) <= 5),
  CONSTRAINT grievance_handler_matches_state CHECK ((state = 'unrouted') = (handler_member IS NULL)),
  CONSTRAINT grievance_handler_not_complainant CHECK (handler_member IS DISTINCT FROM complainant_member),
  CONSTRAINT grievance_decided_whole CHECK ((state = 'decided') = (finding IS NOT NULL AND outcome IS NOT NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL)),
  CONSTRAINT grievance_finding_known CHECK (finding IS NULL OR finding IN ('substantiated', 'partly_substantiated', 'not_substantiated', 'no_finding')),
  CONSTRAINT grievance_outcome_length CHECK (outcome IS NULL OR length(btrim(outcome)) BETWEEN 20 AND 4000),
  CONSTRAINT grievance_decider_is_handler CHECK (decided_by IS NULL OR decided_by = handler_member)
);

-- What may change: the handler (a handover, which names the next one) and the written outcome, once; the people, the words and the complainant never. The handler is always an authorised person who is not
-- the complainant and not one of the people complained about, and is never changed to an unauthorised one.
-- +goose StatementBegin
CREATE FUNCTION nemura.grievance_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A grievance is kept: it is decided, never deleted' USING ERRCODE = 'NM209';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.state = 'decided' OR (NEW.organisation_id, NEW.id, NEW.complainant_member, NEW.subject_people, NEW.summary, NEW.filed_at) IS DISTINCT FROM
       (OLD.organisation_id, OLD.id, OLD.complainant_member, OLD.subject_people, OLD.summary, OLD.filed_at) THEN
      RAISE EXCEPTION 'A decided grievance, and the complainant, the people complained about and the words of any grievance, never change' USING ERRCODE = 'NM209';
    END IF;
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.handler_member IS NULL AND NEW.handler_member IS DISTINCT FROM nemura.current_person() THEN
    RAISE EXCEPTION 'An unrouted grievance is taken up by the authorised person who takes it, not given to another' USING ERRCODE = 'NM210';
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.handler_member IS NOT NULL AND NEW.handler_member IS DISTINCT FROM OLD.handler_member THEN
    NEW.handed_by := OLD.handler_member;
    NEW.handover_xact := pg_current_xact_id()::text::bigint;
  END IF;
  IF NEW.handler_member IS NOT NULL THEN
    IF nemura.is_subject_member(NEW.subject_people, NEW.handler_member) THEN
      RAISE EXCEPTION 'A grievance is never handled by a person it is about' USING ERRCODE = 'NM210';
    END IF;
    IF NOT nemura.can_handle_grievance(NEW.handler_member, nemura.acting_day()) THEN
      RAISE EXCEPTION 'A grievance is handled by an active HR administrator or the Owner' USING ERRCODE = 'NM210';
    END IF;
  END IF;
  IF nemura.is_subject_member(NEW.subject_people, NEW.complainant_member) THEN
    RAISE EXCEPTION 'A grievance is not filed about the person who files it' USING ERRCODE = 'NM210';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER grievance_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.grievance FOR EACH ROW EXECUTE FUNCTION nemura.grievance_guard();

-- The inquiry record: notes, interviews, findings and handovers, append-only, readable by the handler alone.
CREATE TABLE nemura.grievance_entry (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  grievance_id    uuid        NOT NULL,
  kind            text        NOT NULL,
  body            text        NOT NULL,
  by_member       uuid        NOT NULL,
  at              timestamptz NOT NULL,
  subject_people  uuid[]      NOT NULL DEFAULT '{}',   -- copied from the grievance, so the restrictive policy needs no other table
  CONSTRAINT grievance_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT grievance_entry_grievance_fkey FOREIGN KEY (organisation_id, grievance_id) REFERENCES nemura.grievance (organisation_id, id),
  CONSTRAINT grievance_entry_by_fkey FOREIGN KEY (organisation_id, by_member) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT grievance_entry_kind_known CHECK (kind IN ('note', 'interview', 'finding', 'handover', 'routing')),
  CONSTRAINT grievance_entry_body_length CHECK (length(btrim(body)) BETWEEN 1 AND 4000)
);
-- +goose StatementBegin
CREATE FUNCTION nemura.grievance_entry_fill() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  SELECT g.subject_people INTO NEW.subject_people FROM nemura.grievance g WHERE g.organisation_id = NEW.organisation_id AND g.id = NEW.grievance_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'There is no such grievance in the acting person''s view' USING ERRCODE = 'NM209';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER grievance_entry_fill BEFORE INSERT ON nemura.grievance_entry FOR EACH ROW EXECUTE FUNCTION nemura.grievance_entry_fill();
CREATE TRIGGER grievance_entry_append_only BEFORE UPDATE OR DELETE ON nemura.grievance_entry FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A grievance entry');

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.grievance DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('grievance', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.grievance_entry DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('grievance_entry', 'id');

ALTER TABLE nemura.grievance ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.grievance FORCE ROW LEVEL SECURITY;
ALTER TABLE nemura.grievance_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.grievance_entry FORCE ROW LEVEL SECURITY;
-- Who may read: the complainant and the handler, of their own organisation, and nobody else, not even an HR administrator who is neither.
CREATE POLICY parties ON nemura.grievance FOR SELECT TO nemura_api
  USING (organisation_id = nemura.current_org() AND (nemura.current_person() IN (complainant_member, handler_member)
    OR (handed_by = nemura.current_person() AND handover_xact = pg_current_xact_id()::text::bigint)
    OR (state = 'unrouted' AND nemura.can_handle_grievance(nemura.current_person(), nemura.acting_day()))));
CREATE POLICY parties_insert ON nemura.grievance FOR INSERT TO nemura_api WITH CHECK (organisation_id = nemura.current_org() AND complainant_member = nemura.current_person());
CREATE POLICY parties_update ON nemura.grievance FOR UPDATE TO nemura_api
  USING (organisation_id = nemura.current_org() AND (nemura.current_person() = handler_member OR (state = 'unrouted' AND nemura.can_handle_grievance(nemura.current_person(), nemura.acting_day()))))
  WITH CHECK (organisation_id = nemura.current_org());
-- The inquiry record: the handler of the grievance only (the complainant reads the outcome on the grievance, not the notes).
CREATE POLICY handler_reads ON nemura.grievance_entry FOR SELECT TO nemura_api
  USING (organisation_id = nemura.current_org() AND EXISTS (SELECT 1 FROM nemura.grievance g WHERE g.organisation_id = grievance_entry.organisation_id AND g.id = grievance_entry.grievance_id AND g.handler_member = nemura.current_person()));
CREATE POLICY handler_writes ON nemura.grievance_entry FOR INSERT TO nemura_api
  WITH CHECK (organisation_id = nemura.current_org() AND by_member = nemura.current_person()
    AND (kind = 'routing' OR EXISTS (SELECT 1 FROM nemura.grievance g WHERE g.organisation_id = grievance_entry.organisation_id AND g.id = grievance_entry.grievance_id AND g.handler_member = nemura.current_person())));
-- The rule that matters most: a member who is one of the people complained about reads and writes none of it, whatever role they hold, and with no acting person nothing is visible.
CREATE POLICY not_the_subject ON nemura.grievance AS RESTRICTIVE FOR ALL USING (nemura.current_person() IS NOT NULL AND NOT nemura.is_subject_member(subject_people, nemura.current_person()));
CREATE POLICY not_the_subject ON nemura.grievance_entry AS RESTRICTIVE FOR ALL USING (nemura.current_person() IS NOT NULL AND NOT nemura.is_subject_member(subject_people, nemura.current_person()));

REVOKE ALL ON nemura.grievance, nemura.grievance_entry FROM PUBLIC;
GRANT SELECT ON nemura.grievance, nemura.grievance_entry TO nemura_api;
GRANT INSERT (id, complainant_member, subject_people, summary, state, handler_member, filed_at, routed_at) ON nemura.grievance TO nemura_api;
GRANT UPDATE (state, handler_member, routed_at, finding, outcome, decided_by, decided_at) ON nemura.grievance TO nemura_api;
GRANT INSERT (id, grievance_id, kind, body, by_member, at) ON nemura.grievance_entry TO nemura_api;
REVOKE ALL ON FUNCTION nemura.is_subject_member(uuid[], uuid), nemura.can_handle_grievance(uuid, date), nemura.acting_day() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.is_subject_member(uuid[], uuid), nemura.can_handle_grievance(uuid, date), nemura.acting_day() TO nemura_api;
