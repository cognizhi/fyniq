-- Hiring, the rest of job posts (PRD-002 NM-REC-02): the careers page, a new advert after a material change and the refusal of an advert closed over three months ago.
--
-- A careers page is an organisation's public list of its published posts under a slug it chose. The list is read with no sign-in through one definer function that sets the organisation
-- from the directory itself, so no application role reads another organisation's posts and the directory holds no person, no figure and no salary. The advert's date is still recorded once,
-- except that a renewal after a change of occupation, salary, number of vacancies or UEN archives the old advert and records a new one, once for each renewal.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.job_post ADD COLUMN advert_closed_on date, ADD COLUMN advert_renewals integer NOT NULL DEFAULT 0;

-- The adverts a renewal replaced, kept as evidence and never changed.
CREATE TABLE nemura.job_advert (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  job_post_id     uuid        NOT NULL,
  posted_on       date        NOT NULL,
  reference       text,
  closed_on       date,
  change_kind     text        NOT NULL,   -- why the advert was replaced
  change_note     text,
  replaced_by     uuid        NOT NULL,
  replaced_at     timestamptz NOT NULL,
  CONSTRAINT job_advert_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT job_advert_post_fkey FOREIGN KEY (organisation_id, job_post_id) REFERENCES nemura.job_post (organisation_id, id),
  CONSTRAINT job_advert_by_fkey FOREIGN KEY (organisation_id, replaced_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT job_advert_kind_known CHECK (change_kind IN ('occupation', 'salary', 'vacancies', 'uen')),
  CONSTRAINT job_advert_note_length CHECK (change_note IS NULL OR length(change_note) <= 500)
);
-- +goose StatementBegin
CREATE FUNCTION nemura.job_advert_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'A replaced advert is evidence: it is never changed or deleted' USING ERRCODE = 'NM213';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER job_advert_guard BEFORE UPDATE OR DELETE ON nemura.job_advert FOR EACH ROW EXECUTE FUNCTION nemura.job_advert_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.job_advert DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('job_post', 'job_post_id');
ALTER TABLE nemura.job_advert ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.job_advert FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.job_advert USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.job_advert FROM PUBLIC;
GRANT SELECT ON nemura.job_advert TO nemura_api, nemura_readonly;
GRANT INSERT (id, job_post_id, posted_on, reference, closed_on, change_kind, change_note, replaced_by, replaced_at) ON nemura.job_advert TO nemura_api;

-- The post's guard: the advert's date is recorded once, and replaced only by a later date with the renewal counted in the same update (the renewal's archive row is written with it). The day an advert came
-- down (advert_closed_on) is recorded once too, and never before the day it went up.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.job_post_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A job post is never deleted: it is closed' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.requisition_id IS DISTINCT FROM OLD.requisition_id OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'A job post stays with its requisition' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.state <> 'draft' AND (NEW.title, NEW.body, NEW.requirement_note, NEW.published_on) IS DISTINCT FROM (OLD.title, OLD.body, OLD.requirement_note, OLD.published_on) THEN
    RAISE EXCEPTION 'A published job post never changes' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.state = 'closed' THEN
    RAISE EXCEPTION 'A closed job post is final' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.advert_posted_on IS DISTINCT FROM OLD.advert_posted_on THEN
    IF OLD.advert_posted_on IS NOT NULL AND NOT (NEW.advert_renewals = OLD.advert_renewals + 1 AND NEW.advert_posted_on > OLD.advert_posted_on AND NEW.advert_closed_on IS NULL) THEN
      RAISE EXCEPTION 'The advert date is recorded once; a later advert is a renewal after a material change' USING ERRCODE = 'NM213';
    END IF;
  ELSIF NEW.advert_renewals IS DISTINCT FROM OLD.advert_renewals THEN
    RAISE EXCEPTION 'The renewals are counted only when an advert is replaced' USING ERRCODE = 'NM213';
  END IF;
  IF OLD.exemption IS NOT NULL AND NEW.exemption IS DISTINCT FROM OLD.exemption THEN
    RAISE EXCEPTION 'An exemption is recorded once' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.advert_closed_on IS DISTINCT FROM OLD.advert_closed_on AND (OLD.advert_closed_on IS NOT NULL AND NEW.advert_closed_on IS NOT NULL) THEN
    RAISE EXCEPTION 'The day an advert came down is recorded once' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.advert_closed_on IS NOT NULL AND (NEW.advert_posted_on IS NULL OR NEW.advert_closed_on < NEW.advert_posted_on) THEN
    RAISE EXCEPTION 'An advert comes down on or after the day it went up' USING ERRCODE = 'NM213';
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state AND NOT ((OLD.state = 'draft' AND NEW.state = 'published') OR (OLD.state = 'published' AND NEW.state = 'closed')) THEN
    RAISE EXCEPTION 'A job post is a draft, then published, then closed' USING ERRCODE = 'NM213';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
GRANT UPDATE (advert_closed_on, advert_renewals) ON nemura.job_post TO nemura_api;

-- The organisation's careers page: its slug, whether it is on and the words above its list (the public read starts from the directory below).
CREATE TABLE nemura.careers_page (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  slug            text        NOT NULL,
  enabled         boolean     NOT NULL DEFAULT false,
  company         text        NOT NULL,
  intro           text,
  updated_by      uuid        NOT NULL,
  updated_at      timestamptz NOT NULL,
  CONSTRAINT careers_page_pkey PRIMARY KEY (organisation_id),
  CONSTRAINT careers_page_by_fkey FOREIGN KEY (organisation_id, updated_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT careers_page_slug_form CHECK (slug ~ '^[a-z0-9](?:[a-z0-9-]{1,38})[a-z0-9]$'),
  CONSTRAINT careers_page_company_length CHECK (length(btrim(company)) BETWEEN 1 AND 120),
  CONSTRAINT careers_page_intro_length CHECK (intro IS NULL OR length(intro) <= 1000)
);
CREATE UNIQUE INDEX careers_page_slug_key ON nemura.careers_page (slug);
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.careers_page DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('careers_page', 'organisation_id');
ALTER TABLE nemura.careers_page ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.careers_page FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.careers_page USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.careers_page FROM PUBLIC;
GRANT SELECT ON nemura.careers_page TO nemura_api, nemura_readonly;
GRANT INSERT (slug, enabled, company, intro, updated_by, updated_at), UPDATE (slug, enabled, company, intro, updated_by, updated_at) ON nemura.careers_page TO nemura_api;

-- The directory of addresses the public read starts from: no row-level security (nobody knows the organisation yet), no grant to any application role; the definer functions alone read and write it, and the
-- one that writes refuses another organisation's row. An address is unique across companies.
CREATE TABLE nemura.careers_directory (
  slug            text    NOT NULL,
  organisation_id uuid    NOT NULL,
  enabled         boolean NOT NULL,
  CONSTRAINT careers_directory_pkey PRIMARY KEY (slug),
  CONSTRAINT careers_directory_org_key UNIQUE (organisation_id)
);
REVOKE ALL ON nemura.careers_directory FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION nemura.careers_register(p_org uuid, p_slug text, p_enabled boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'An address is registered for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.careers_directory d WHERE d.slug = p_slug AND d.organisation_id <> p_org) THEN
    RETURN false;
  END IF;
  DELETE FROM nemura.careers_directory WHERE organisation_id = p_org AND slug <> p_slug;
  INSERT INTO nemura.careers_directory (slug, organisation_id, enabled) VALUES (p_slug, p_org, p_enabled)
    ON CONFLICT (slug) DO UPDATE SET enabled = EXCLUDED.enabled WHERE nemura.careers_directory.organisation_id = p_org;
  RETURN true;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.careers_register(uuid, text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.careers_register(uuid, text, boolean) TO nemura_api;

-- The public list: the page's company and words and each published, approved post that is still open (title, team, text, the day it went up). It names no salary, no person and no requisition. A slug that is
-- unknown or switched off answers no rows.
-- +goose StatementBegin
CREATE FUNCTION nemura.careers_public(p_slug text) RETURNS TABLE (company text, intro text, post_id uuid, title text, department text, body text, published_on date)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE o uuid;
BEGIN
  SELECT d.organisation_id INTO o FROM nemura.careers_directory d WHERE d.slug = p_slug AND d.enabled;
  IF o IS NULL THEN
    RETURN;
  END IF;
  PERFORM pg_catalog.set_config('app.org_id', o::text, true);
  RETURN QUERY
    SELECT c.company, c.intro, NULL::uuid, NULL::text, NULL::text, NULL::text, NULL::date FROM nemura.careers_page c WHERE c.organisation_id = o
    UNION ALL
    SELECT NULL::text, NULL::text, j.id, j.title, r.department, j.body, j.published_on
      FROM nemura.job_post j JOIN nemura.requisition r ON r.organisation_id = j.organisation_id AND r.id = j.requisition_id
     WHERE j.organisation_id = o AND j.state = 'published' AND r.state = 'approved';
  PERFORM pg_catalog.set_config('app.org_id', '', true);
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.careers_public(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.careers_public(text) TO nemura_api;

