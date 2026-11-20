-- Grievances, the gaps of migration 000063 (PRD-002 NM-SEC-06, NM-SEC-05).
--
-- 1. A stranded grievance: one whose handler has been deactivated, or no longer holds a role that may handle it, would wait for ever, because the Owner has no override. Decision (21 Nov 2026,
--    option A, owner to confirm): an Owner or HR administrator who is neither the complainant nor one of the people complained about may list and read such a grievance and take it. Nothing
--    else widens: the policies still hide it from the people it is about (the restrictive policy), the inquiry record stays the handler's alone (the new handler reads it after taking), and the
--    taker becomes the handler through the same guard that refuses an unauthorised handler.
-- 2. The worker may learn only which grievances wait and whom to tell, by identity (never a word), and may delete a decided grievance only through purge_grievances, after the retention period.
--    Retention is five years from the written outcome [owner to confirm]; a legal hold on the record type or on anyone involved keeps it. The functions are owned by nemura_migrator, so the
--    policies name that role for them; no service connects as it.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A grievance is stranded while it is open and its handler can no longer handle one.
-- +goose StatementBegin
CREATE FUNCTION nemura.is_stranded(g_state text, handler uuid, on_day date) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT g_state = 'open' AND handler IS NOT NULL AND NOT nemura.can_handle_grievance(handler, on_day)
$$;
-- +goose StatementEnd

DROP POLICY parties ON nemura.grievance;
CREATE POLICY parties ON nemura.grievance FOR SELECT TO nemura_api
  USING (organisation_id = nemura.current_org() AND (nemura.current_person() IN (complainant_member, handler_member)
    OR (handed_by = nemura.current_person() AND handover_xact = pg_current_xact_id()::text::bigint)
    OR ((state = 'unrouted' OR nemura.is_stranded(state, handler_member, nemura.acting_day())) AND nemura.can_handle_grievance(nemura.current_person(), nemura.acting_day()))));
DROP POLICY parties_update ON nemura.grievance;
CREATE POLICY parties_update ON nemura.grievance FOR UPDATE TO nemura_api
  USING (organisation_id = nemura.current_org() AND (nemura.current_person() = handler_member
    OR ((state = 'unrouted' OR nemura.is_stranded(state, handler_member, nemura.acting_day())) AND nemura.can_handle_grievance(nemura.current_person(), nemura.acting_day()))))
  WITH CHECK (organisation_id = nemura.current_org());

-- The guard: someone who is not the handler may only take a grievance that is waiting (unrouted, or stranded), as its new handler and with it still open and undecided.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.grievance_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_user = 'nemura_migrator' AND OLD.state = 'decided' THEN
      RETURN OLD; -- only purge_grievances runs as its owner, and it checks the retention period and the holds first
    END IF;
    RAISE EXCEPTION 'A grievance is kept: it is decided, never deleted' USING ERRCODE = 'NM209';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.state = 'decided' OR (NEW.organisation_id, NEW.id, NEW.complainant_member, NEW.subject_people, NEW.summary, NEW.filed_at) IS DISTINCT FROM
       (OLD.organisation_id, OLD.id, OLD.complainant_member, OLD.subject_people, OLD.summary, OLD.filed_at) THEN
      RAISE EXCEPTION 'A decided grievance, and the complainant, the people complained about and the words of any grievance, never change' USING ERRCODE = 'NM209';
    END IF;
    IF OLD.handler_member IS NOT NULL AND nemura.current_person() IS DISTINCT FROM OLD.handler_member
       AND (NEW.handler_member IS DISTINCT FROM nemura.current_person() OR NEW.state <> 'open' OR NEW.finding IS NOT NULL OR NEW.outcome IS NOT NULL) THEN
      RAISE EXCEPTION 'Only the handler changes a grievance, and a person who is not the handler may only take one whose handler has left' USING ERRCODE = 'NM210';
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

-- The inquiry record is append-only; only the purge, as the schema's owner, removes entries with their grievance.
-- +goose StatementBegin
CREATE FUNCTION nemura.grievance_entry_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_user = 'nemura_migrator' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'A grievance entry is append-only: it is never changed or deleted' USING ERRCODE = 'NM209';
END
$$;
-- +goose StatementEnd
DROP TRIGGER grievance_entry_append_only ON nemura.grievance_entry;
CREATE TRIGGER grievance_entry_append_only BEFORE UPDATE OR DELETE ON nemura.grievance_entry FOR EACH ROW EXECUTE FUNCTION nemura.grievance_entry_guard();

-- The functions below run as the schema's owner, which the restrictive policy and the permissive ones did not name.
CREATE POLICY owner_functions ON nemura.grievance FOR ALL TO nemura_migrator USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
CREATE POLICY owner_functions ON nemura.grievance_entry FOR ALL TO nemura_migrator USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
DROP POLICY not_the_subject ON nemura.grievance;
DROP POLICY not_the_subject ON nemura.grievance_entry;
CREATE POLICY not_the_subject ON nemura.grievance AS RESTRICTIVE FOR ALL
  USING (current_user = 'nemura_migrator' OR (nemura.current_person() IS NOT NULL AND NOT nemura.is_subject_member(subject_people, nemura.current_person())));
CREATE POLICY not_the_subject ON nemura.grievance_entry AS RESTRICTIVE FOR ALL
  USING (current_user = 'nemura_migrator' OR (nemura.current_person() IS NOT NULL AND NOT nemura.is_subject_member(subject_people, nemura.current_person())));

-- What waits, and whom to tell: identities only. A grievance waits while it is unrouted or stranded; each Owner or HR administrator who is active, not the complainant and not involved is to be told.
-- +goose StatementBegin
CREATE FUNCTION nemura.grievances_waiting(p_on date) RETURNS TABLE (grievance_id uuid, notify_member uuid, reason text, handler_key text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT g.id, c.member_id, CASE WHEN g.state = 'unrouted' THEN 'unrouted' ELSE 'handler_left' END, coalesce(g.handler_member::text, 'none')
    FROM nemura.grievance g
    JOIN LATERAL (SELECT DISTINCT a.member_id FROM nemura.role_assignment a JOIN nemura.member m ON m.organisation_id = a.organisation_id AND m.id = a.member_id
                   WHERE a.organisation_id = g.organisation_id AND a.role IN ('hr_admin', 'owner') AND m.deactivated_at IS NULL AND a.effective_from <= p_on AND (a.effective_to IS NULL OR a.effective_to > p_on)
                     AND a.member_id <> g.complainant_member AND NOT nemura.is_subject_member(g.subject_people, a.member_id)) c ON true
   WHERE g.organisation_id = nemura.current_org() AND (g.state = 'unrouted' OR nemura.is_stranded(g.state, g.handler_member, p_on))
$$;
-- +goose StatementEnd

-- The decided grievances, for the retention report: by identity, with a fingerprint of the words and not the words. hold_subject is the person whose legal hold on grievances keeps it, if any
-- (the complainant's, or one of the people complained about), else the complainant's own person.
-- +goose StatementBegin
CREATE FUNCTION nemura.grievances_for_retention() RETURNS TABLE (id uuid, decided_on date, hold_subject uuid, fingerprint bytea)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT g.id, g.decided_at::date,
         coalesce((SELECT h.subject_id FROM nemura.legal_hold h WHERE h.organisation_id = g.organisation_id AND h.record_type = 'grievances' AND h.released_on IS NULL
                      AND h.subject_id = ANY (g.subject_people || coalesce(cp.id, '00000000-0000-0000-0000-000000000000'::uuid)) LIMIT 1), cp.id, '00000000-0000-0000-0000-000000000000'::uuid),
         sha256(convert_to(g.summary || '|' || coalesce(g.outcome, ''), 'UTF8'))
    FROM nemura.grievance g LEFT JOIN nemura.person cp ON cp.organisation_id = g.organisation_id AND cp.member_id = g.complainant_member
   WHERE g.organisation_id = nemura.current_org() AND g.state = 'decided'
$$;
-- +goose StatementEnd

-- The purge: the only way a grievance and its inquiry record are deleted. Five years from the written outcome at the least, and never while a legal hold on grievances (every one, or one
-- naming anyone involved) stands; it refuses the whole call if any named grievance is not due, so a bug cannot delete one early.
-- +goose StatementBegin
CREATE FUNCTION nemura.purge_grievances(p_ids uuid[], p_on date) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  g nemura.grievance;
  n integer := 0;
BEGIN
  FOR g IN SELECT * FROM nemura.grievance WHERE organisation_id = nemura.current_org() AND id = ANY (p_ids) FOR UPDATE LOOP
    IF g.state <> 'decided' OR (g.decided_at::date + interval '5 years')::date > p_on THEN
      RAISE EXCEPTION 'A grievance is kept for five years after its outcome is written' USING ERRCODE = 'NM211';
    END IF;
    IF EXISTS (SELECT 1 FROM nemura.legal_hold h WHERE h.organisation_id = g.organisation_id AND h.record_type = 'grievances' AND h.released_on IS NULL AND h.placed_on <= p_on
                  AND (h.subject_id IS NULL OR h.subject_id = ANY (g.subject_people) OR h.subject_id IN (SELECT p.id FROM nemura.person p WHERE p.member_id = g.complainant_member))) THEN
      RAISE EXCEPTION 'A legal hold keeps this grievance' USING ERRCODE = 'NM211';
    END IF;
    DELETE FROM nemura.grievance_entry WHERE organisation_id = g.organisation_id AND grievance_id = g.id;
    DELETE FROM nemura.grievance WHERE organisation_id = g.organisation_id AND id = g.id;
    n := n + 1;
  END LOOP;
  RETURN n;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION nemura.is_stranded(text, uuid, date), nemura.grievances_waiting(date), nemura.grievances_for_retention(), nemura.purge_grievances(uuid[], date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.is_stranded(text, uuid, date) TO nemura_api;
GRANT EXECUTE ON FUNCTION nemura.grievances_waiting(date), nemura.grievances_for_retention(), nemura.purge_grievances(uuid[], date) TO nemura_worker;
