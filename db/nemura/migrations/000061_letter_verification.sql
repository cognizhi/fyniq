-- Verifying an issued letter (PRD-002 NM-LTR-05). The reference and a code printed on the letter are looked up by anyone, with no sign-in and no organisation in hand, so the lookup is
-- a global directory of hashes: the SHA-256 of the reference and the code, the organisation and letter it names, and the few facts the answer may show (the letter's type, the day it
-- was issued, the company's name, whether it is valid or withdrawn). It holds no person, no figure and no text of the letter. No application role reads or writes the table; the
-- definer functions do, and the three that change it refuse another organisation's rows (nemura.current_org()). The employee's revocation withdraws a letter's verification only; it never
-- withdraws the letter, and the letter and its record stay as they were.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.letter_verification (
  lookup_hash     bytea       NOT NULL,   -- SHA-256 of the reference and the code printed on the letter
  organisation_id uuid        NOT NULL,
  request_id      uuid        NOT NULL,
  type_key        text        NOT NULL,
  letter_id       uuid,
  issued_on       date,
  company         text,
  state           text        NOT NULL DEFAULT 'pending',
  state_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT letter_verification_pkey PRIMARY KEY (lookup_hash),
  CONSTRAINT letter_verification_request_key UNIQUE (organisation_id, request_id),
  CONSTRAINT letter_verification_hash CHECK (octet_length(lookup_hash) = 32),
  CONSTRAINT letter_verification_state CHECK (state IN ('pending', 'valid', 'withdrawn')),
  CONSTRAINT letter_verification_issued_whole CHECK ((state = 'pending') = (letter_id IS NULL AND issued_on IS NULL AND company IS NULL) OR state = 'withdrawn')
);
REVOKE ALL ON nemura.letter_verification FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_register(p_org uuid, p_request uuid, p_type text, p_lookup bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'A verification is registered for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  INSERT INTO nemura.letter_verification (lookup_hash, organisation_id, request_id, type_key) VALUES (p_lookup, p_org, p_request, p_type);
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_issue(p_org uuid, p_request uuid, p_letter uuid, p_on date, p_company text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'A verification is issued for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  UPDATE nemura.letter_verification SET state = 'valid', letter_id = p_letter, issued_on = p_on, company = p_company, state_at = now()
   WHERE organisation_id = p_org AND request_id = p_request AND state = 'pending';
END
$$;
-- +goose StatementEnd

-- A withdrawal is final and only ever from valid: a withdrawn letter is not made valid again.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_withdraw(p_org uuid, p_letter uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  n integer;
BEGIN
  IF p_org IS DISTINCT FROM nemura.current_org() THEN
    RAISE EXCEPTION 'A verification is withdrawn for the caller''s own organisation only' USING ERRCODE = 'NM206';
  END IF;
  UPDATE nemura.letter_verification SET state = 'withdrawn', state_at = now() WHERE organisation_id = p_org AND letter_id = p_letter AND state = 'valid';
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n > 0;
END
$$;
-- +goose StatementEnd

-- The public look-up: by the hash alone, answering the letter's type, the day it was issued, the company and whether it is valid or withdrawn; a pending one is not found.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_verification_lookup(p_lookup bytea) RETURNS TABLE (organisation_id uuid, letter_id uuid, state text, type_key text, issued_on date, company text)
LANGUAGE sql SECURITY DEFINER STABLE
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT v.organisation_id, v.letter_id, v.state, v.type_key, v.issued_on, v.company FROM nemura.letter_verification v WHERE v.lookup_hash = p_lookup AND v.state IN ('valid', 'withdrawn')
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION nemura.letter_verification_register(uuid, uuid, text, bytea), nemura.letter_verification_issue(uuid, uuid, uuid, date, text),
  nemura.letter_verification_withdraw(uuid, uuid), nemura.letter_verification_lookup(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.letter_verification_register(uuid, uuid, text, bytea), nemura.letter_verification_issue(uuid, uuid, uuid, date, text),
  nemura.letter_verification_withdraw(uuid, uuid) TO nemura_api;
GRANT EXECUTE ON FUNCTION nemura.letter_verification_lookup(bytea) TO nemura_api;
