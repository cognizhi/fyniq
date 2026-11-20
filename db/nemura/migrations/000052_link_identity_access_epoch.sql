-- One sign-in across the apps (design D16, D17): what Fyniq says of an identity is versioned by the sending event's sequence, and a sequence starts again at 1 with each link (a link is made again at the next
-- epoch). The answer is kept with the epoch it came in, newest (epoch, version) wins, and only the live link's epoch is read, so an answer from an ended link neither decides a switch nor blocks the new link's.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.link_identity_access ADD COLUMN epoch integer NOT NULL DEFAULT 1;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.link_identity_access_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.issuer IS DISTINCT FROM OLD.issuer OR NEW.subject IS DISTINCT FROM OLD.subject OR (NEW.epoch, NEW.version) <= (OLD.epoch, OLD.version) THEN
    RAISE EXCEPTION 'What Fyniq says of an identity is replaced by a newer version of a newer or the same epoch, never changed back or deleted' USING ERRCODE = 'NM193';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (epoch), UPDATE (epoch) ON nemura.link_identity_access TO nemura_api;
