-- The bank's code and name beside the sealed salary account (PRD-001 FR-PPL-12; p1-nemura-fyniq-link design D26, D28; owner's decisions of 21 Nov 2026; needs a human review: it changes what an approval of a
-- bank account change binds). Fyniq derives the BIC from the bank code, so Nemura keeps the bank's Singapore code (four digits, or four and a three-digit branch) and its name. They are not secret (a
-- bank, not an account) but they are part of what is approved: the approval's digest binds the code with the account's fingerprint, and a proposed change never changes it.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE nemura.person_secret
  ADD COLUMN bank_code text,
  ADD COLUMN bank_name text,
  ADD CONSTRAINT person_secret_bank_code CHECK (bank_code IS NULL OR bank_code ~ '^[0-9]{4}([0-9]{3})?$'),
  ADD CONSTRAINT person_secret_bank_name CHECK (bank_name IS NULL OR char_length(bank_name) BETWEEN 1 AND 140);

ALTER TABLE nemura.bank_account_change
  ADD COLUMN new_bank_code text,
  ADD COLUMN new_bank_name text,
  ADD CONSTRAINT bank_account_change_bank_code CHECK (new_bank_code IS NULL OR new_bank_code ~ '^[0-9]{4}([0-9]{3})?$'),
  ADD CONSTRAINT bank_account_change_bank_name CHECK (new_bank_name IS NULL OR char_length(new_bank_name) BETWEEN 1 AND 140);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION nemura.bank_account_change_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.person_id IS DISTINCT FROM OLD.person_id OR NEW.new_sealed IS DISTINCT FROM OLD.new_sealed OR NEW.new_masked IS DISTINCT FROM OLD.new_masked
     OR NEW.new_bank_code IS DISTINCT FROM OLD.new_bank_code OR NEW.new_bank_name IS DISTINCT FROM OLD.new_bank_name
     OR NEW.proposed_by IS DISTINCT FROM OLD.proposed_by OR NEW.approval_id IS DISTINCT FROM OLD.approval_id
     OR NOT ((OLD.state = 'proposed' AND NEW.state IN ('approved', 'refused')) OR (OLD.state = 'approved' AND NEW.state = 'confirmed')) THEN
    RAISE EXCEPTION 'A bank account change moves forward only: proposed, approved or refused, then confirmed' USING ERRCODE = 'NM126';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

GRANT INSERT (new_bank_code, new_bank_name) ON nemura.bank_account_change TO nemura_api;
GRANT INSERT (bank_code, bank_name), UPDATE (bank_code, bank_name) ON nemura.person_secret TO nemura_api;
