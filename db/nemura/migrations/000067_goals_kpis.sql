-- Goals and KPIs (PRD-002 NM-PRF-01, NM-PRF-02): company, team and personal goals with measurable key results, and role KPIs with targets. A value is entered by a person: a KPI read from Fyniq's metrics arrives
-- with the link [live-verify], and Nemura never holds a company financial figure (NM-ROLE-02).
--
-- A measure never changes: its current value is its latest update's (its start value before any), and an update keeps who gave the value, when and why; the update history is append-only. A goal is open until it is done or cancelled, once.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.goal (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  scope           text        NOT NULL,
  person_id       uuid,                                  -- a personal goal's owner
  team            text,                                  -- a team goal's team
  parent_id       uuid,                                  -- the goal it aligns with
  title           text        NOT NULL,
  starts_on       date        NOT NULL,
  due_on          date        NOT NULL,
  state           text        NOT NULL DEFAULT 'open',
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  closed_at       timestamptz,
  CONSTRAINT goal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT goal_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT goal_parent_fkey FOREIGN KEY (organisation_id, parent_id) REFERENCES nemura.goal (organisation_id, id),
  CONSTRAINT goal_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT goal_scope_known CHECK (scope IN ('company', 'team', 'personal')),
  CONSTRAINT goal_scope_whole CHECK ((scope = 'personal') = (person_id IS NOT NULL) AND (scope = 'team') = (team IS NOT NULL)),
  CONSTRAINT goal_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT goal_team_length CHECK (team IS NULL OR length(btrim(team)) BETWEEN 1 AND 120),
  CONSTRAINT goal_dates CHECK (due_on >= starts_on),
  CONSTRAINT goal_state_known CHECK (state IN ('open', 'done', 'cancelled')),
  CONSTRAINT goal_closed_whole CHECK ((state = 'open') = (closed_at IS NULL))
);
CREATE INDEX goal_person_idx ON nemura.goal (organisation_id, person_id);

CREATE TABLE nemura.measure (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  goal_id         uuid,                                  -- a key result belongs to a goal
  person_id       uuid,                                  -- a role KPI belongs to a person
  title           text          NOT NULL,
  unit            text          NOT NULL DEFAULT '',
  start_value     numeric(20,4) NOT NULL,
  target          numeric(20,4) NOT NULL,
  down            boolean       NOT NULL DEFAULT false,  -- lower is better
  source          text          NOT NULL DEFAULT 'entered',
  created_by      uuid          NOT NULL,
  created_at      timestamptz   NOT NULL,
  CONSTRAINT measure_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT measure_goal_fkey FOREIGN KEY (organisation_id, goal_id) REFERENCES nemura.goal (organisation_id, id),
  CONSTRAINT measure_person_fkey FOREIGN KEY (organisation_id, person_id) REFERENCES nemura.person (organisation_id, id),
  CONSTRAINT measure_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT measure_owner_whole CHECK ((goal_id IS NULL) <> (person_id IS NULL)),
  CONSTRAINT measure_title_length CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  CONSTRAINT measure_unit_length CHECK (length(unit) <= 30),
  CONSTRAINT measure_distance CHECK (target <> start_value AND down = (target < start_value)),
  CONSTRAINT measure_source_known CHECK (source = 'entered')
);

CREATE TABLE nemura.measure_update (
  organisation_id uuid          NOT NULL DEFAULT nemura.current_org(),
  id              uuid          NOT NULL DEFAULT uuidv7(),
  measure_id      uuid          NOT NULL,
  value           numeric(20,4) NOT NULL,
  note            text,
  by_member       uuid          NOT NULL,
  at              timestamptz   NOT NULL,
  CONSTRAINT measure_update_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT measure_update_measure_fkey FOREIGN KEY (organisation_id, measure_id) REFERENCES nemura.measure (organisation_id, id),
  CONSTRAINT measure_update_by_fkey FOREIGN KEY (organisation_id, by_member) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT measure_update_note_length CHECK (note IS NULL OR length(note) <= 300)
);

-- A goal's words, scope and dates never change; only its state, once. A measure and its updates are never changed or deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.goal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.state <> 'open' OR NEW.state = 'open'
     OR (NEW.scope, NEW.person_id, NEW.team, NEW.parent_id, NEW.title, NEW.starts_on, NEW.due_on, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.scope, OLD.person_id, OLD.team, OLD.parent_id, OLD.title, OLD.starts_on, OLD.due_on, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A goal is done or cancelled once, and its words, scope and dates never change' USING ERRCODE = 'NM214';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER goal_guard BEFORE UPDATE OR DELETE ON nemura.goal FOR EACH ROW EXECUTE FUNCTION nemura.goal_guard();
CREATE TRIGGER measure_append_only BEFORE UPDATE OR DELETE ON nemura.measure FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A measure');
CREATE TRIGGER measure_update_append_only BEFORE UPDATE OR DELETE ON nemura.measure_update FOR EACH ROW EXECUTE FUNCTION nemura.append_only_guard('A measure update');

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.goal DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('goal', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.measure_update DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('measure_update', 'id');

ALTER TABLE nemura.goal ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.goal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.goal USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.measure ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.measure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.measure USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.measure_update ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.measure_update FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.measure_update USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.goal, nemura.measure, nemura.measure_update FROM PUBLIC;
GRANT SELECT ON nemura.goal, nemura.measure, nemura.measure_update TO nemura_api, nemura_readonly;
GRANT INSERT (id, scope, person_id, team, parent_id, title, starts_on, due_on, created_by, created_at), UPDATE (state, closed_at) ON nemura.goal TO nemura_api;
GRANT INSERT (id, goal_id, person_id, title, unit, start_value, target, down, created_by, created_at) ON nemura.measure TO nemura_api;
GRANT INSERT (id, measure_id, value, note, by_member, at) ON nemura.measure_update TO nemura_api;
