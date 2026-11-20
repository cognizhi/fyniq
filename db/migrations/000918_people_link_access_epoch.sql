-- One sign-in across the apps (p1-nemura-fyniq-link design D16, D17): what Nemura says of an identity is versioned by the sending event's sequence, and a sequence starts again at 1 with each link (a link is made
-- again at the next epoch). The answer is kept with the epoch it came in, newest (epoch, version) wins, and only the live link's epoch is read, so an answer from an ended link neither decides a switch nor blocks
-- the new link's.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.people_link_access ADD COLUMN epoch integer NOT NULL DEFAULT 1;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.people_link_access_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.subject IS DISTINCT FROM OLD.subject
     OR (NEW.epoch, NEW.version) <= (OLD.epoch, OLD.version) THEN
    RAISE EXCEPTION 'What Nemura says of an identity is replaced by a newer version of a newer or the same epoch, never changed back or deleted' USING ERRCODE = 'FY900',
      HINT = 'The version is the sending event''s sequence (FR-PPL-15).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (epoch), UPDATE (epoch) ON fyniq.people_link_access TO fyniq_api;
