-- A holder record's NRIC guard matches a number in full only as a word of its own (PRD-001 FR-EQ-15, CMP-PD-07).
--
-- FY285 refused a holder record whose payload showed an NRIC or FIN in full anywhere, the holder's contact ID included:
-- a UUID such as ...-f1234567ab... holds a letter of S, T, F, G or M, seven digits and a letter, so about 2% of holders
-- were refused by chance. The guard now matches the number only as a word of its own, as the audit trail's mask and
-- the evidence scan do; a UUID's groups never form one. Found by p1-gst's gates, where TestHoldersAndShareClasses
-- failed once.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.equity_record_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  ok    boolean;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t = 'holder_record' THEN
    SELECT k INTO extra FROM jsonb_object_keys(p) k
     WHERE k NOT IN ('date', 'contact_id', 'kind', 'id_type', 'id_last4', 'registration_number', 'nationality', 'address',
                     'contact_people', 'note')
     LIMIT 1;
    IF extra IS NOT NULL THEN
      RAISE EXCEPTION 'A holder record keeps no %', extra USING ERRCODE = 'FY285',
        HINT = 'It holds the holder''s kind, identifier, nationality, address and contact people only.';
    END IF;
    IF p->>'kind' = 'individual' THEN
      IF p ? 'registration_number' OR NOT coalesce(p->>'id_type', 'nric') IN ('nric', 'fin', 'passport')
         OR NOT coalesce(p->>'id_last4', 'X') ~ '^[A-Za-z0-9]{1,4}$' THEN
        RAISE EXCEPTION 'An individual''s identifier is kept as its type and its last 4 characters only' USING ERRCODE = 'FY285',
          HINT = 'The full number stays in ACRA''s register and the identity document kept as evidence (FR-EQ-15).';
      END IF;
    ELSIF p->>'kind' = 'entity' THEN
      IF p ? 'id_last4' OR NOT coalesce(p->>'id_type', 'uen') IN ('uen', 'foreign_registration')
         OR length(coalesce(p->>'registration_number', '')) > 30 THEN
        RAISE EXCEPTION 'An entity is identified by its UEN or foreign registration number' USING ERRCODE = 'FY285';
      END IF;
    ELSE
      RAISE EXCEPTION 'A holder is an individual or an entity' USING ERRCODE = 'FY285';
    END IF;
    -- A number in full is a word of its own: inside a longer word, such as a contact's ID, the same letters and digits
    -- are no NRIC or FIN (\m and \M are the start and end of a word).
    IF p::text ~ '\m[STFGMstfgm][0-9]{7}[A-Za-z]\M' THEN
      RAISE EXCEPTION 'A holder record never shows an NRIC or FIN in full' USING ERRCODE = 'FY285',
        HINT = 'Keep the last 4 characters only (FR-EQ-15, CMP-PD-07).';
    END IF;
  ELSIF t = 'share_class' THEN
    SELECT EXISTS (SELECT 1 FROM fyniq.account a
                    WHERE a.organisation_id = NEW.organisation_id AND a.id::text = p->>'account_id'
                      AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group)
      INTO ok;
    IF NOT ok THEN
      RAISE EXCEPTION 'A share class belongs to an active share-capital account' USING ERRCODE = 'FY286',
        HINT = 'Choose 3010 for ordinary shares or 3020 for preference shares (FR-EQ-16).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;

-- +goose StatementEnd
