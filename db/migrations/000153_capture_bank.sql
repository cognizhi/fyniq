-- The bank-ingest address (PRD-001 FR-INT-22, FR-BNK-17; p1-bank-import-reconcile task 7.1, design D10): a third kind
-- of email-in address, one per organisation, that only the banks' registered senders reach. The documents addresses
-- (the organisation's and each member's) keep their kind, `documents`; a `bank` address belongs to the organisation,
-- never to a member. Decisions about senders are kept per kind, so a sender accepted at one address is not accepted at
-- the other.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.capture_address ADD COLUMN kind text NOT NULL DEFAULT 'documents';
ALTER TABLE fyniq.capture_address ADD CONSTRAINT capture_address_kind_known CHECK (kind IN ('documents', 'bank')) NOT VALID;
ALTER TABLE fyniq.capture_address ADD CONSTRAINT capture_address_bank_organisation CHECK (kind <> 'bank' OR user_id IS NULL) NOT VALID;

ALTER TABLE fyniq.capture_sender ADD COLUMN kind text NOT NULL DEFAULT 'documents';
ALTER TABLE fyniq.capture_sender ADD CONSTRAINT capture_sender_kind_known CHECK (kind IN ('documents', 'bank')) NOT VALID;
ALTER TABLE fyniq.capture_sender ADD CONSTRAINT capture_sender_bank_organisation CHECK (kind <> 'bank' OR user_id IS NULL) NOT VALID;

-- One live address of each kind for the organisation, and one for each member. capture_address holds a few rows per
-- organisation, so building the index in this transaction holds it for moments (squawk: the concurrent forms cannot run
-- in a transaction).
-- squawk-ignore require-concurrent-index-deletion
DROP INDEX fyniq.capture_address_live_owner;
-- squawk-ignore require-concurrent-index-creation
CREATE UNIQUE INDEX capture_address_live_owner ON fyniq.capture_address
  (organisation_id, kind, coalesce(user_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE retired_at IS NULL;

-- An address's kind never changes, as its name and member do not (FY182).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.capture_address_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  owner uuid;
BEGIN
  IF TG_OP = 'INSERT' THEN
    INSERT INTO fyniq.capture_name (local_part, owner, address_id, user_id)
    VALUES (NEW.local_part, NEW.organisation_id, NEW.id, NEW.user_id)
    ON CONFLICT (local_part) DO UPDATE SET address_id = EXCLUDED.address_id, user_id = EXCLUDED.user_id
      WHERE capture_name.owner = EXCLUDED.owner AND capture_name.address_id IS NULL
    RETURNING capture_name.owner INTO owner;
    IF owner IS NULL THEN
      RAISE EXCEPTION 'The capture address name % is taken', NEW.local_part USING ERRCODE = 'FY183',
        HINT = 'Choose another name: a name belongs to one organisation for good (FR-INT-22).';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' OR OLD.retired_at IS NOT NULL OR NEW.retired_at IS NULL
     OR (NEW.local_part, NEW.user_id, NEW.kind, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM (OLD.local_part, OLD.user_id, OLD.kind, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A capture address is only ever retired, once' USING ERRCODE = 'FY182',
      HINT = 'Retire it and make a new one (FR-INT-22).';
  END IF;
  UPDATE fyniq.capture_name SET address_id = NULL, user_id = NULL WHERE local_part = OLD.local_part AND address_id = OLD.id;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (kind) ON fyniq.capture_address, fyniq.capture_sender TO fyniq_api;
