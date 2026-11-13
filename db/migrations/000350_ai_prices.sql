-- The prices of the AI connections' models (PRD-001 AI-82, AI-85; p1-ai-providers task 4.2, design D4).
--
-- ai_price_catalogue is each download of a public price catalogue (LiteLLM's): a global table, the same for every
-- organisation, with no organisation_id and no grant. Services read it through the view ai_price_catalogues, and only
-- the worker's daily read writes it, through record_ai_price_catalogue. Each download keeps its SHA-256, size, source
-- URL and retrieval time, its licence check and the licence's digest, and whether it was valid, or why it was refused;
-- the document itself is kept in the file store under its digest. A download is never changed or removed.
--
-- ai_price is each version of a model's price in an organisation: its source (the provider's list, Google's billing
-- catalogue, LiteLLM's catalogue, or an Owner with a second approver), the catalogue and entry it came from, how it
-- matched (exact, normalised, or entered), its currency and the source's rates it does not use. One version per model
-- is in force (active), and at most one waits for an Owner's review (pending): a normalised match always does, and so
-- does a rate that fell by more than half. A version that becomes active or pending supersedes the older ones it
-- replaces; an Owner's price is never superseded by a read (FY333). ai_price_rate is a version's rates by kind and by
-- the prompt length above which each applies, in its unit, as exact decimals above zero: written with its version, in
-- its transaction, and never changed (FY333).
--
-- ai_model gains when its price was last looked for and, when that left it without a new price, why: a model nothing
-- prices stays visibly unpriced, never priced by a guess (AI-82). The checks on ai_model are added NOT VALID and 000351
-- validates them (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ the catalogue downloads (global)
CREATE TABLE fyniq.ai_price_catalogue (
  id             uuid        NOT NULL DEFAULT uuidv7(),
  source         text        NOT NULL,
  url            text        NOT NULL,
  fetched_at     timestamptz NOT NULL,
  sha256         text        NOT NULL,
  size           bigint      NOT NULL,
  licence        text,                  -- what the licence check found; NULL when it failed
  licence_url    text        NOT NULL,
  licence_sha256 text        NOT NULL,
  outcome        text        NOT NULL,
  failure        text,
  entries        integer     NOT NULL,  -- the chat models it prices
  document       text        NOT NULL,  -- the file store's key of the document as downloaded
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ai_price_catalogue_pkey PRIMARY KEY (id),
  CONSTRAINT ai_price_catalogue_source_known CHECK (source IN ('litellm')),
  CONSTRAINT ai_price_catalogue_urls CHECK (url ~ '^(https|file)://[!-~]+$' AND length(url) <= 500
    AND licence_url ~ '^(https|file)://[!-~]+$' AND length(licence_url) <= 500),
  CONSTRAINT ai_price_catalogue_digests CHECK (sha256 ~ '^[0-9a-f]{64}$' AND licence_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT ai_price_catalogue_outcome_known CHECK (outcome IN ('valid', 'rejected')),
  CONSTRAINT ai_price_catalogue_failure CHECK ((outcome = 'valid') = (failure IS NULL) AND length(failure) <= 500),
  CONSTRAINT ai_price_catalogue_valid_licensed CHECK (outcome <> 'valid' OR licence IS NOT NULL),
  CONSTRAINT ai_price_catalogue_licence_length CHECK (length(licence) <= 200),
  CONSTRAINT ai_price_catalogue_counts CHECK (size >= 0 AND entries >= 0),
  CONSTRAINT ai_price_catalogue_document_key CHECK (document = 'ai/price-catalogues/' || source || '/' || sha256 || '.json')
);
CREATE INDEX ai_price_catalogue_latest ON fyniq.ai_price_catalogue (source, fetched_at DESC);
REVOKE ALL ON fyniq.ai_price_catalogue FROM PUBLIC;

CREATE VIEW fyniq.ai_price_catalogues AS
  SELECT id, source, url, fetched_at, sha256, size, licence, licence_url, licence_sha256, outcome, failure, entries, document, recorded_at
    FROM fyniq.ai_price_catalogue;
GRANT SELECT ON fyniq.ai_price_catalogues TO fyniq_api, fyniq_worker, fyniq_readonly;

-- record_ai_price_catalogue keeps one download of a catalogue and returns its id.
-- +goose StatementBegin
CREATE FUNCTION fyniq.record_ai_price_catalogue(p_source text, p_url text, p_fetched_at timestamptz, p_sha256 text, p_size bigint,
  p_licence text, p_licence_url text, p_licence_sha256 text, p_outcome text, p_failure text, p_entries integer, p_document text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  out_id uuid;
BEGIN
  INSERT INTO fyniq.ai_price_catalogue (source, url, fetched_at, sha256, size, licence, licence_url, licence_sha256, outcome, failure, entries, document)
  VALUES (p_source, p_url, p_fetched_at, p_sha256, p_size, p_licence, p_licence_url, p_licence_sha256, p_outcome, p_failure, p_entries, p_document)
  RETURNING id INTO out_id;
  RETURN out_id;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.record_ai_price_catalogue(text, text, timestamptz, text, bigint, text, text, text, text, text, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.record_ai_price_catalogue(text, text, timestamptz, text, bigint, text, text, text, text, text, integer, text) TO fyniq_worker;

-- ------------------------------------------------------------------ an organisation's prices
CREATE TABLE fyniq.ai_price (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connection_id   uuid        NOT NULL,
  model           text        NOT NULL,
  number          integer     NOT NULL,  -- the version of this model's price, from 1
  source          text        NOT NULL,
  catalogue_id    uuid,                  -- the catalogue download it came from (AI-85)
  catalogue_key   text,                  -- the source's entry it matched
  match           text        NOT NULL,
  currency        text        NOT NULL,
  ignored         text[]      NOT NULL DEFAULT '{}', -- the source's rates Fyniq does not price with
  status          text        NOT NULL,
  review          text,                  -- why it waited for an Owner's review: normalised or drop
  review_detail   text,
  recorded_by     text        NOT NULL,
  recorded_at     timestamptz NOT NULL,
  created_xact    bigint      NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  approval_id     uuid,                  -- an Owner's price: the approval of its second approver
  decided_by      text,                  -- the Owner who confirmed or declined it on review
  decided_at      timestamptz,
  CONSTRAINT ai_price_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_price_model_fkey FOREIGN KEY (organisation_id, connection_id, model) REFERENCES fyniq.ai_model (organisation_id, connection_id, model),
  CONSTRAINT ai_price_catalogue_fkey FOREIGN KEY (catalogue_id) REFERENCES fyniq.ai_price_catalogue (id),
  CONSTRAINT ai_price_currency_fkey FOREIGN KEY (currency) REFERENCES fyniq.currency (code),
  CONSTRAINT ai_price_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT ai_price_number UNIQUE (organisation_id, connection_id, model, number),
  CONSTRAINT ai_price_number_positive CHECK (number > 0),
  CONSTRAINT ai_price_source_known CHECK (source IN ('provider_list', 'google_billing', 'litellm', 'owner')),
  CONSTRAINT ai_price_match_known CHECK (match IN ('exact', 'normalised', 'entered')),
  -- An Owner's price is entered, with its second approver's approval, and names no catalogue (AI-82).
  CONSTRAINT ai_price_owner_entered CHECK ((source = 'owner') = (match = 'entered') AND (source = 'owner') = (approval_id IS NOT NULL)
    AND (source = 'owner') = (catalogue_key IS NULL)),
  -- A price from LiteLLM's catalogue names the download it came from, which every past estimate is reproduced with (AI-85).
  CONSTRAINT ai_price_catalogued CHECK ((source = 'litellm') = (catalogue_id IS NOT NULL)),
  CONSTRAINT ai_price_key_format CHECK (catalogue_key ~ '^[!-~]+$' AND length(catalogue_key) <= 300),
  CONSTRAINT ai_price_status_known CHECK (status IN ('active', 'pending', 'superseded', 'declined')),
  CONSTRAINT ai_price_review_known CHECK (review IN ('normalised', 'drop') AND length(review_detail) <= 500),
  CONSTRAINT ai_price_pending_says_why CHECK (status <> 'pending' OR review IS NOT NULL),
  -- A normalised match waits for an Owner, and one that waited is in force only by an Owner's decision (AI-82, AI-85).
  CONSTRAINT ai_price_normalised_reviewed CHECK (match <> 'normalised' OR coalesce(review, '') = 'normalised'),
  CONSTRAINT ai_price_reviewed_in_force CHECK (review IS NULL OR status <> 'active' OR decided_by IS NOT NULL),
  CONSTRAINT ai_price_decided CHECK ((decided_by IS NULL) = (decided_at IS NULL)),
  CONSTRAINT ai_price_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$' AND decided_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT ai_price_ignored_count CHECK (cardinality(ignored) <= 100)
);
CREATE UNIQUE INDEX ai_price_in_force ON fyniq.ai_price (organisation_id, connection_id, model) WHERE status = 'active';
CREATE UNIQUE INDEX ai_price_waiting ON fyniq.ai_price (organisation_id, connection_id, model) WHERE status = 'pending';
CREATE INDEX ai_price_by_catalogue ON fyniq.ai_price (organisation_id, catalogue_id);
CREATE INDEX ai_price_by_approval ON fyniq.ai_price (organisation_id, approval_id);

CREATE TABLE fyniq.ai_price_rate (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  price_id        uuid           NOT NULL,
  kind            text           NOT NULL,
  above           bigint         NOT NULL DEFAULT 0, -- the prompt length in tokens above which it applies; 0 for the base rate
  unit            text           NOT NULL,
  amount          numeric(24,10) NOT NULL,
  CONSTRAINT ai_price_rate_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_price_rate_price_fkey FOREIGN KEY (organisation_id, price_id) REFERENCES fyniq.ai_price (organisation_id, id),
  CONSTRAINT ai_price_rate_one UNIQUE (organisation_id, price_id, kind, above),
  CONSTRAINT ai_price_rate_kind_known CHECK (kind IN ('input', 'output', 'cache_read', 'cache_write', 'cache_write_1h', 'reasoning', 'input_image', 'request')),
  CONSTRAINT ai_price_rate_unit CHECK (unit = CASE kind WHEN 'input_image' THEN 'per_image' WHEN 'request' THEN 'per_request' ELSE 'per_million_tokens' END),
  CONSTRAINT ai_price_rate_tier CHECK (above >= 0 AND (above = 0 OR unit = 'per_million_tokens')),
  -- Exact decimals above zero, never NaN (golden rule 1, QA-ENG-05).
  CONSTRAINT ai_price_rate_amount CHECK (amount <> 'NaN' AND amount > 0)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_price_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  in_force record;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A price and its rates are kept, so every past estimate can be reproduced' USING ERRCODE = 'FY333';
  END IF;
  IF TG_TABLE_NAME = 'ai_price_rate' THEN
    IF TG_OP <> 'INSERT' OR NOT EXISTS (SELECT 1 FROM fyniq.ai_price p WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.price_id
                                          AND p.created_xact = pg_current_xact_id()::text::bigint) THEN
      RAISE EXCEPTION 'A price''s rates are written with it, in its transaction, and never changed' USING ERRCODE = 'FY333';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status NOT IN ('active', 'pending') OR NEW.decided_by IS NOT NULL OR NEW.created_xact <> pg_current_xact_id()::text::bigint THEN
      RAISE EXCEPTION 'A price starts in force or waiting for a review, undecided' USING ERRCODE = 'FY333';
    END IF;
  ELSE
    IF (NEW.organisation_id, NEW.id, NEW.connection_id, NEW.model, NEW.number, NEW.source, NEW.catalogue_id, NEW.catalogue_key, NEW.match,
        NEW.currency, NEW.ignored, NEW.review, NEW.review_detail, NEW.recorded_by, NEW.recorded_at, NEW.created_xact, NEW.approval_id)
       IS DISTINCT FROM
       (OLD.organisation_id, OLD.id, OLD.connection_id, OLD.model, OLD.number, OLD.source, OLD.catalogue_id, OLD.catalogue_key, OLD.match,
        OLD.currency, OLD.ignored, OLD.review, OLD.review_detail, OLD.recorded_by, OLD.recorded_at, OLD.created_xact, OLD.approval_id) THEN
      RAISE EXCEPTION 'A price is kept as it was recorded: only its state moves' USING ERRCODE = 'FY333';
    END IF;
    IF NEW.status = OLD.status AND (NEW.decided_by, NEW.decided_at) IS NOT DISTINCT FROM (OLD.decided_by, OLD.decided_at) THEN
      RETURN NEW;
    END IF;
    IF NOT ((OLD.status = 'pending' AND NEW.status IN ('active', 'declined') AND NEW.decided_by IS NOT NULL AND OLD.decided_by IS NULL)
         OR (OLD.status IN ('active', 'pending') AND NEW.status = 'superseded'
             AND (NEW.decided_by, NEW.decided_at) IS NOT DISTINCT FROM (OLD.decided_by, OLD.decided_at))) THEN
      RAISE EXCEPTION 'A price moves only from waiting to in force or declined by an Owner''s decision, or to superseded'
        USING ERRCODE = 'FY333';
    END IF;
    IF NEW.status <> 'active' THEN
      RETURN NEW;
    END IF;
  END IF;
  -- A version that becomes active replaces the one in force and every older one waiting; one that starts waiting
  -- replaces the older one waiting. An Owner's price is replaced only by an Owner's (AI-82).
  IF NEW.status = 'active' THEN
    SELECT source, number INTO in_force FROM fyniq.ai_price
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND status = 'active' AND id <> NEW.id;
    IF FOUND AND in_force.source = 'owner' AND NEW.source <> 'owner' THEN
      RAISE EXCEPTION 'An Owner''s price of % is never overwritten by a read', NEW.model USING ERRCODE = 'FY333',
        HINT = 'Withdraw the Owner''s price first, if the source''s price should apply.';
    END IF;
    UPDATE fyniq.ai_price SET status = 'superseded'
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND id <> NEW.id
       AND (status = 'active' OR (status = 'pending' AND number < NEW.number));
  ELSE
    UPDATE fyniq.ai_price SET status = 'superseded'
     WHERE organisation_id = NEW.organisation_id AND connection_id = NEW.connection_id AND model = NEW.model AND id <> NEW.id
       AND status = 'pending';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_price_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_price
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_price_guard();
CREATE TRIGGER ai_price_rate_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_price_rate
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_price_guard();
CREATE TRIGGER ai_price_no_truncate BEFORE TRUNCATE ON fyniq.ai_price
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_price_rate_no_truncate BEFORE TRUNCATE ON fyniq.ai_price_rate
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_price
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_price', 'id');

ALTER TABLE fyniq.ai_price ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_price FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_price
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_price_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_price_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_price_rate
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_price, fyniq.ai_price_rate FROM PUBLIC;
GRANT SELECT ON fyniq.ai_price, fyniq.ai_price_rate TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The worker's daily read records prices; the API records an Owner's review of one waiting.
GRANT INSERT (connection_id, model, number, source, catalogue_id, catalogue_key, match, currency, ignored, status, review, review_detail,
              recorded_by, recorded_at) ON fyniq.ai_price TO fyniq_worker;
GRANT UPDATE (status) ON fyniq.ai_price TO fyniq_worker;
GRANT UPDATE (status, decided_by, decided_at) ON fyniq.ai_price TO fyniq_api;
GRANT INSERT (price_id, kind, above, unit, amount) ON fyniq.ai_price_rate TO fyniq_worker;

-- ------------------------------------------------------------------ when each model's price was last looked for
ALTER TABLE fyniq.ai_model ADD COLUMN price_checked_at timestamptz;
ALTER TABLE fyniq.ai_model ADD COLUMN unpriced text; -- why the last look found no new price for it
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_unpriced_checked CHECK (unpriced IS NULL OR price_checked_at IS NOT NULL) NOT VALID;
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_unpriced_length CHECK (length(unpriced) <= 500) NOT VALID;
GRANT UPDATE (price_checked_at, unpriced) ON fyniq.ai_model TO fyniq_worker;
