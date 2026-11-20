-- What NM-LTR-05 left after migration 000061: a recipient who holds the document can check it by its hash instead of typing the reference and the code; the person a letter
-- is about can share the letter's own facts through a link that expires and can be revoked; and the public look-ups are limited by a counter in the database, so several API
-- instances share one limit instead of each having its own.
--
-- The directory (letter_verification) gains the SHA-256 of the exact PDF issued, for the look-up by hash. The disclosure link is a tenant table like letter_link, but it can be revoked
-- (one column, once), and revoking it never touches the letter or its verification. The counter holds a hash of what is counted (a reference or "all"), a window start and a count;
-- no role reads it, only the function that takes a hit does.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.letter_verification ADD COLUMN content_hash bytea;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX letter_verification_content_key ON nemura.letter_verification (content_hash) WHERE content_hash IS NOT NULL;

-- The letters issued before this were registered without their file's hash: take it from the letter itself, one organisation at a time under its own tenancy.
-- +goose StatementBegin
DO $$
DECLARE o uuid;
BEGIN
  FOR o IN SELECT DISTINCT organisation_id FROM nemura.letter_verification WHERE content_hash IS NULL AND letter_id IS NOT NULL LOOP
    PERFORM pg_catalog.set_config('app.org_id', o::text, true);
    UPDATE nemura.letter_verification v SET content_hash = i.sha256
      FROM nemura.letter_issued i WHERE i.organisation_id = v.organisation_id AND i.id = v.letter_id AND v.content_hash IS NULL;
  END LOOP;
  PERFORM pg_catalog.set_config('app.org_id', '', true);
END
$$;
-- +goose StatementEnd

DROP FUNCTION nemura.letter_verification_issue(uuid, uuid, uuid, date, text);
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_issue(p_org uuid, p_request uuid, p_letter uuid, p_on date, p_company text, p_content bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'A verification is issued for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  IF p_content IS NULL OR octet_length(p_content) <> 32 THEN
    RAISE EXCEPTION 'A verification keeps the SHA-256 of the letter issued' USING ERRCODE = 'NM206';
  END IF;
  UPDATE nemura.letter_verification SET state = 'valid', letter_id = p_letter, issued_on = p_on, company = p_company, content_hash = p_content, state_at = now()
   WHERE organisation_id = p_org AND request_id = p_request AND state = 'pending';
END
$$;
-- +goose StatementEnd

-- The look-up by the hash of the document itself, answering exactly what the look-up by reference and code answers.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_lookup_content(p_content bytea) RETURNS TABLE (organisation_id uuid, letter_id uuid, state text, type_key text, issued_on date, company text)
LANGUAGE sql SECURITY DEFINER STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT v.organisation_id, v.letter_id, v.state, v.type_key, v.issued_on, v.company FROM nemura.letter_verification v WHERE v.content_hash = p_content AND v.state IN ('valid', 'withdrawn')
$$;
-- +goose StatementEnd

-- Whether a letter's verification stands, for the disclosure page: it shows the facts only while the letter is valid.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_state(p_org uuid, p_letter uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE s text;
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'A verification is read for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  SELECT v.state INTO s FROM nemura.letter_verification v WHERE v.organisation_id = p_org AND v.letter_id = p_letter;
  RETURN s;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION nemura.letter_verification_issue(uuid, uuid, uuid, date, text, bytea), nemura.letter_verification_lookup_content(bytea), nemura.letter_verification_state(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.letter_verification_issue(uuid, uuid, uuid, date, text, bytea), nemura.letter_verification_lookup_content(bytea), nemura.letter_verification_state(uuid, uuid) TO nemura_api;

-- A disclosure link: the token is kept as a hash, expires, and is revoked by the person it was made for; it shows the letter's own facts and nothing else.
CREATE TABLE nemura.letter_disclosure (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  issued_id       uuid        NOT NULL,
  token_hash      bytea       NOT NULL,
  expires_at      timestamptz NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  CONSTRAINT letter_disclosure_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_disclosure_issued_fkey FOREIGN KEY (organisation_id, issued_id) REFERENCES nemura.letter_issued (organisation_id, id),
  CONSTRAINT letter_disclosure_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_disclosure_token CHECK (octet_length(token_hash) = 32),
  CONSTRAINT letter_disclosure_token_once UNIQUE (token_hash),
  CONSTRAINT letter_disclosure_expiry CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 days')
);
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_disclosure_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A disclosure link is kept: it is revoked, never deleted' USING ERRCODE = 'NM207';
  END IF;
  -- Only the revocation changes, and once.
  IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL
     OR (NEW.organisation_id, NEW.id, NEW.issued_id, NEW.token_hash, NEW.expires_at, NEW.created_by, NEW.created_at) IS DISTINCT FROM
        (OLD.organisation_id, OLD.id, OLD.issued_id, OLD.token_hash, OLD.expires_at, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A disclosure link changes only by being revoked, once' USING ERRCODE = 'NM207';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER letter_disclosure_guard BEFORE UPDATE OR DELETE ON nemura.letter_disclosure FOR EACH ROW EXECUTE FUNCTION nemura.letter_disclosure_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_disclosure DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_disclosure', 'id');
ALTER TABLE nemura.letter_disclosure ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_disclosure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_disclosure USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
GRANT SELECT ON nemura.letter_disclosure TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, issued_id, token_hash, expires_at, created_by, created_at), UPDATE (revoked_at) ON nemura.letter_disclosure TO nemura_api;

-- The shared limit of the public look-ups: a count for each window, kept in the database so every API instance adds to the same one. The caller passes the time, so the window is
-- the application clock's. Only the function reads or writes the table; it also forgets windows more than two hours old.
CREATE TABLE nemura.public_rate (
  scope        text        NOT NULL,
  key_hash     bytea       NOT NULL,   -- SHA-256 of what is counted: a reference, a token, or "all"
  window_start timestamptz NOT NULL,
  hits         integer     NOT NULL,
  CONSTRAINT public_rate_pkey PRIMARY KEY (scope, key_hash, window_start)
);
CREATE INDEX public_rate_window ON nemura.public_rate (window_start);
REVOKE ALL ON nemura.public_rate FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION nemura.public_rate_hit(p_scope text, p_key bytea, p_window_seconds integer, p_limit integer, p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  w timestamptz;
  n integer;
BEGIN
  IF p_window_seconds NOT BETWEEN 1 AND 86400 OR p_limit < 1 OR octet_length(p_key) <> 32 OR p_scope !~ '^[a-z][a-z0-9_.-]{0,40}$' THEN
    RAISE EXCEPTION 'A rate window is 1 second to a day, a limit is at least 1, a key is a SHA-256 and a scope a short name' USING ERRCODE = 'NM208';
  END IF;
  w := to_timestamp(floor(extract(epoch FROM p_now) / p_window_seconds) * p_window_seconds);
  DELETE FROM nemura.public_rate WHERE window_start < p_now - interval '2 hours' AND scope = p_scope;
  INSERT INTO nemura.public_rate AS r (scope, key_hash, window_start, hits) VALUES (p_scope, p_key, w, 1)
    ON CONFLICT (scope, key_hash, window_start) DO UPDATE SET hits = r.hits + 1 RETURNING r.hits INTO n;
  RETURN n <= p_limit;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.public_rate_hit(text, bytea, integer, integer, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.public_rate_hit(text, bytea, integer, integer, timestamptz) TO nemura_api;
