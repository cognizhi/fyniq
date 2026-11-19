-- Nemura's side of the link to Fyniq (PRD-002 NM-ARC-04, NM-INT-01, 03, 10, 11; p1-nemura-fyniq-link tasks 1.1, 2.1, 2.3, 3.2). The link names the Fyniq organisation it is bound to, an
-- epoch every event carries, and Fyniq's public key, against which what Fyniq sends is verified. Nemura's own signing key is a secret of the link, readable by the worker alone (it signs at
-- send time); sealing it under the platform's key is [live-verify]. The inbox keeps each event received once, by its ID, with identities and a digest and never a payload; a run's posting
-- answer from Fyniq (the reference, or the code and the plain message of a refusal) is kept for the run to show. Payment states from Fyniq are not kept here: they go through
-- nemura.pay_payment_event by its one function, as the contract says.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.fyniq_link (
  organisation_id       uuid        NOT NULL DEFAULT nemura.current_org(),
  id                    uuid        NOT NULL DEFAULT uuidv7(),
  state                 text        NOT NULL,
  epoch                 integer     NOT NULL,
  fyniq_organisation_id uuid        NOT NULL,
  fyniq_key             bytea       NOT NULL,   -- Fyniq's Ed25519 public key: what Fyniq sends is verified against it
  link_public           bytea       NOT NULL,   -- Nemura's own public key, which Fyniq holds for what Nemura sends
  sequence              bigint      NOT NULL DEFAULT 0,   -- the last sequence number Nemura sent on this link
  version               integer     NOT NULL DEFAULT 1,
  created_at            timestamptz NOT NULL,
  changed_at            timestamptz NOT NULL,
  CONSTRAINT fyniq_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT fyniq_link_state_known CHECK (state IN ('active', 'suspended', 'unlinking', 'unlinked')),
  CONSTRAINT fyniq_link_keys CHECK (octet_length(fyniq_key) = 32 AND octet_length(link_public) = 32),
  CONSTRAINT fyniq_link_epoch_positive CHECK (epoch >= 1)
);
CREATE UNIQUE INDEX fyniq_link_one_live ON nemura.fyniq_link (organisation_id) WHERE state <> 'unlinked';
CREATE UNIQUE INDEX fyniq_link_epoch ON nemura.fyniq_link (organisation_id, epoch);

CREATE TABLE nemura.fyniq_link_secret (
  organisation_id uuid  NOT NULL DEFAULT nemura.current_org(),
  link_id         uuid  NOT NULL,
  link_private    bytea NOT NULL,   -- Nemura's Ed25519 private key for this link: signs at send time
  CONSTRAINT fyniq_link_secret_pkey PRIMARY KEY (organisation_id, link_id),
  CONSTRAINT fyniq_link_secret_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES nemura.fyniq_link (organisation_id, id),
  CONSTRAINT fyniq_link_secret_size CHECK (octet_length(link_private) = 64)
);

-- +goose StatementBegin
CREATE FUNCTION nemura.fyniq_link_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
DECLARE
  top integer;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A link is kept: unlink it instead' USING ERRCODE = 'NM190';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT coalesce(max(epoch), 0) INTO top FROM nemura.fyniq_link WHERE organisation_id = NEW.organisation_id;
    IF NEW.state <> 'active' OR NEW.version <> 1 OR NEW.epoch <> top + 1 THEN
      RAISE EXCEPTION 'A link starts active, at its first version, one epoch above the last' USING ERRCODE = 'NM190';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.id, NEW.epoch, NEW.fyniq_organisation_id, NEW.fyniq_key, NEW.link_public, NEW.created_at) IS DISTINCT FROM
     (OLD.id, OLD.epoch, OLD.fyniq_organisation_id, OLD.fyniq_key, OLD.link_public, OLD.created_at)
     OR NEW.sequence < OLD.sequence
     OR NEW.version <> OLD.version + 1
     OR (NEW.state <> OLD.state AND NOT ((OLD.state, NEW.state) IN (('active', 'suspended'), ('suspended', 'active'), ('active', 'unlinking'),
                                       ('suspended', 'unlinking'), ('unlinking', 'unlinked'), ('unlinking', 'active')))) THEN
    RAISE EXCEPTION 'A link only moves between its states, at its next version, and keeps its epoch and keys' USING ERRCODE = 'NM190';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER fyniq_link_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.fyniq_link FOR EACH ROW EXECUTE FUNCTION nemura.fyniq_link_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.fyniq_link DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('fyniq_link', 'id');

CREATE TABLE nemura.link_inbox (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  event_id        uuid        NOT NULL,
  link_id         uuid        NOT NULL,
  epoch           integer     NOT NULL,
  sequence        bigint      NOT NULL,
  type            text        NOT NULL,
  object_type     text        NOT NULL,
  object_id       text        NOT NULL,
  object_version  integer     NOT NULL,
  payload_sha256  text        NOT NULL,
  outcome         text        NOT NULL,
  code            text,
  received_at     timestamptz NOT NULL,
  CONSTRAINT link_inbox_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT link_inbox_event_key UNIQUE (organisation_id, event_id),
  CONSTRAINT link_inbox_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES nemura.fyniq_link (organisation_id, id),
  CONSTRAINT link_inbox_outcome_known CHECK (outcome IN ('applied', 'ignored_older', 'refused')),
  CONSTRAINT link_inbox_code_with_refusal CHECK ((outcome = 'refused') = (code IS NOT NULL)),
  CONSTRAINT link_inbox_digest CHECK (payload_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE INDEX link_inbox_object ON nemura.link_inbox (organisation_id, object_type, object_id, object_version);

-- What Fyniq answered for a run's version: posted with its reference, or refused with its code and a plain message; the run shows it and waits (NM-INT-03).
CREATE TABLE nemura.link_run_posting (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  run_id          uuid        NOT NULL,
  version         integer     NOT NULL,
  status          text        NOT NULL,
  posting_ref     text,
  code            text,
  message         text,
  received_at     timestamptz NOT NULL,
  CONSTRAINT link_run_posting_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT link_run_posting_once UNIQUE (organisation_id, run_id, version, status),
  CONSTRAINT link_run_posting_status CHECK (status IN ('posted', 'refused')),
  CONSTRAINT link_run_posting_shape CHECK ((status = 'posted') = (posting_ref IS NOT NULL) AND (status = 'refused') = (code IS NOT NULL))
);

-- +goose StatementBegin
CREATE FUNCTION nemura.link_append_only() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'What the link received is kept as it came: a change is a new event' USING ERRCODE = 'NM191';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER link_inbox_guard BEFORE UPDATE OR DELETE ON nemura.link_inbox FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();
CREATE TRIGGER link_run_posting_guard BEFORE UPDATE OR DELETE ON nemura.link_run_posting FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();

ALTER TABLE nemura.fyniq_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.fyniq_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.fyniq_link USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.fyniq_link_secret ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.fyniq_link_secret FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.fyniq_link_secret USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_inbox USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.link_run_posting ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_run_posting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_run_posting USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

REVOKE ALL ON nemura.fyniq_link, nemura.fyniq_link_secret, nemura.link_inbox, nemura.link_run_posting FROM PUBLIC;
GRANT SELECT ON nemura.fyniq_link, nemura.link_inbox, nemura.link_run_posting TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, state, epoch, fyniq_organisation_id, fyniq_key, link_public, created_at, changed_at) ON nemura.fyniq_link TO nemura_api;
GRANT UPDATE (state, sequence, version, changed_at) ON nemura.fyniq_link TO nemura_api, nemura_worker;
GRANT INSERT (link_id, link_private) ON nemura.fyniq_link_secret TO nemura_api;
GRANT SELECT ON nemura.fyniq_link_secret TO nemura_worker;
GRANT INSERT (event_id, link_id, epoch, sequence, type, object_type, object_id, object_version, payload_sha256, outcome, code, received_at) ON nemura.link_inbox TO nemura_api;
GRANT INSERT (run_id, version, status, posting_ref, code, message, received_at) ON nemura.link_run_posting TO nemura_api;
