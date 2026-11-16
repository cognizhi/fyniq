-- The ACRA extract's officers and registered office (PRD-001 FR-CS-01; p1-company-secretary task 2.5, design D3).
--
-- An acra_extract document may also hold the officers the business profile lists, each a name, a role and the day
-- appointed, and the registered office it gives, typed in with its PDF as its holdings are. The corporate profile is
-- reconciled to them on the extract's day (C.2 #34). An extract typed before keeps no officers and stays valid.
--
-- The database is the backstop (FY297, as 000270): its own keys, now with officers and the registered office; each
-- officer of its own keys, a known role, a name and a day appointed on or before the profile's day; an office of at most
-- 500 characters; and no NRIC or FIN in full, a word of its own as 000276 reads it.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
-- An ACRA extract keeps its rules whoever writes it (FY297).
CREATE OR REPLACE FUNCTION fyniq.acra_extract_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  h     jsonb;
  o     jsonb;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'acra_extract' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k
   WHERE k NOT IN ('date', 'profile_on', 'holdings', 'officers', 'registered_office', 'note') LIMIT 1;
  IF extra IS NOT NULL OR coalesce(p->>'profile_on', '') = '' OR jsonb_typeof(p->'holdings') IS DISTINCT FROM 'array'
     OR (p ? 'officers' AND jsonb_typeof(p->'officers') IS DISTINCT FROM 'array') OR length(coalesce(p->>'registered_office', '')) > 500 THEN
    RAISE EXCEPTION 'An ACRA extract keeps its own keys: the business profile''s day, its holdings, and its officers and registered office'
      USING ERRCODE = 'FY297', HINT = 'Type in what the business profile lists (FR-EQ-26, FR-CS-01).';
  END IF;
  FOR h IN SELECT * FROM jsonb_array_elements(p->'holdings') LOOP
    SELECT k INTO extra FROM jsonb_object_keys(h) k WHERE k NOT IN ('holder_id', 'class_account_id', 'shares') LIMIT 1;
    IF extra IS NOT NULL OR NOT coalesce(h->>'shares', '') ~ '^[1-9][0-9]{0,13}$' THEN
      RAISE EXCEPTION 'An ACRA extract''s holding is a holder''s whole shares of a class' USING ERRCODE = 'FY297',
        HINT = 'Type the shares as the business profile lists them (FR-EQ-26).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.contact c
                    WHERE c.organisation_id = NEW.organisation_id AND c.id::text = h->>'holder_id'
                      AND (c.is_shareholder OR c.is_investor)) THEN
      RAISE EXCEPTION 'An ACRA extract''s holder is a contact marked shareholder or investor' USING ERRCODE = 'FY297',
        HINT = 'Mark the contact a shareholder in Contacts first (FR-EQ-15, FR-EQ-26).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.account a
                    WHERE a.organisation_id = NEW.organisation_id AND a.id::text = h->>'class_account_id'
                      AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group) THEN
      RAISE EXCEPTION 'An ACRA extract''s class is an active share-capital account' USING ERRCODE = 'FY297',
        HINT = 'Choose 3010 Ordinary or 3020 Preference share capital (FR-EQ-26).';
    END IF;
  END LOOP;
  FOR o IN SELECT * FROM jsonb_array_elements(coalesce(p->'officers', '[]'::jsonb)) LOOP
    SELECT k INTO extra FROM jsonb_object_keys(o) k WHERE k NOT IN ('name', 'role', 'appointed_on') LIMIT 1;
    IF extra IS NOT NULL OR length(btrim(coalesce(o->>'name', ''))) NOT BETWEEN 1 AND 200
       OR NOT coalesce(o->>'role', '') IN ('director', 'secretary', 'ceo', 'auditor') OR coalesce(o->>'appointed_on', '') = ''
       OR (o->>'appointed_on')::date > (p->>'profile_on')::date THEN
      RAISE EXCEPTION 'An ACRA extract''s officer is a name, a role and the day appointed, on or before the profile''s day'
        USING ERRCODE = 'FY297', HINT = 'Type each officer as the business profile lists them (FR-CS-01).';
    END IF;
  END LOOP;
  IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
    RAISE EXCEPTION 'An ACRA extract never shows an NRIC or FIN in full' USING ERRCODE = 'FY297',
      HINT = 'Type names only; the identifiers stay in the business profile''s PDF (SEC-15).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
