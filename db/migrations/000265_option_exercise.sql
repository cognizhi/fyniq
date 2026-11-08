-- An exercise of options (PRD-001 FR-EQ-19; p1-equity-register slice 5b-ii, task 6.1, design D9).
--
-- An exercise is an equity transaction of kind exercise: an issue to a recorded grant's holder, of its class, at its
-- exercise price, made effective from share application monies like a cash issue (000252), with the ACRA steps of an
-- allotment (000254). Its shares come from the grant: the options it exercises are no longer outstanding, and a reversal
-- makes them outstanding again.
--
-- The database is the backstop:
--   - (FY288) an issue's movement may be an exercise's, and an exercise takes an allotment's steps (FY290): the
--     functions of 000260 are replaced to say so;
--   - (FY293) an exercise's movement names a recorded grant of the same holder and class at its exercise price, and,
--     under the lock on the organisation's pool, the grant's effective exercises not reversed and its events never take
--     more options than it gave; option_event_guard (000263) is replaced to count the exercises too. Vesting, which
--     limits an exercise to what is vested, is computed and so checked by the engine only (design D9).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
-- (000252, 000260, 000265) A holding moves only with its transaction; an issue's movement is a cash issue's, a
-- conversion's or an exercise's (FY288).
CREATE OR REPLACE FUNCTION fyniq.holding_movement_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  d       record;
  s       fyniq.share_issue;
  r       fyniq.holding_movement;
  p       record;
  account text;
  status  text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Holding movements are never deleted' USING ERRCODE = 'FY288';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Holding movement % is never changed or deleted', OLD.id USING ERRCODE = 'FY288',
      HINT = 'Correct an equity transaction with a reversing one (FR-EQ-17).';
  END IF;
  IF NEW.shares = 'NaN' THEN -- as the check says, before the guard reads anything (§11.3)
    RAISE EXCEPTION 'A holding movement''s shares are a number' USING ERRCODE = 'check_violation';
  END IF;
  NEW.created_xact := pg_current_xact_id()::text::bigint;

  IF NEW.transaction_id IS NOT NULL THEN
    SELECT dd.status, v.payload INTO d
      FROM fyniq.document dd
      JOIN fyniq.document_version v ON v.organisation_id = dd.organisation_id AND v.document_id = dd.id AND v.version = dd.current_version
     WHERE dd.organisation_id = NEW.organisation_id AND dd.id = NEW.transaction_id AND dd.type = 'equity_transaction';
    IF NOT FOUND OR d.status <> 'posted'
       OR (NEW.kind = 'issue' AND d.payload->>'kind' NOT IN ('issue_cash', 'instrument_conversion', 'exercise'))
       OR (NEW.kind <> 'issue' AND d.payload->>'kind' <> NEW.kind) THEN
      RAISE EXCEPTION 'Holding movement % names no approved equity transaction of its kind', NEW.id USING ERRCODE = 'FY288',
        HINT = 'Make an approved equity transaction effective, which writes its movements (FR-EQ-17).';
    END IF;
    IF EXISTS (SELECT 1 FROM fyniq.holding_movement m
                WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id
                  AND m.created_xact <> NEW.created_xact) THEN
      RAISE EXCEPTION 'Equity transaction % is effective already, so it takes no more movements', NEW.transaction_id
        USING ERRCODE = 'FY288', HINT = 'Correct it with a reversing transaction (FR-EQ-17).';
    END IF;
  END IF;

  -- (000260) FR-EQ-18: a SAFE or note converts once: no other conversion of it is effective and not reversed.
  IF NEW.kind = 'issue' AND NEW.transaction_id IS NOT NULL THEN -- d is the transaction's version only then
   IF d.payload->>'kind' = 'instrument_conversion'
     AND EXISTS (SELECT 1 FROM fyniq.holding_movement m
                   JOIN fyniq.document o ON o.organisation_id = m.organisation_id AND o.id = m.transaction_id
                   JOIN fyniq.document_version ov ON ov.organisation_id = o.organisation_id AND ov.document_id = o.id
                                                 AND ov.version = o.current_version
                  WHERE m.organisation_id = NEW.organisation_id AND m.kind = 'issue' AND m.transaction_id <> NEW.transaction_id
                    AND ov.payload->>'kind' = 'instrument_conversion' AND ov.payload->>'instrument_id' = d.payload->>'instrument_id'
                    AND NOT EXISTS (SELECT 1 FROM fyniq.holding_movement back
                                     WHERE back.organisation_id = m.organisation_id AND back.reverses_id = m.id)) THEN
    RAISE EXCEPTION 'Instrument % has converted already', d.payload->>'instrument_id' USING ERRCODE = 'FY288',
      HINT = 'A SAFE or note converts once; reverse that conversion first (FR-EQ-18).';
   END IF;
  END IF;
  IF NEW.kind = 'issue' THEN
    SELECT * INTO s FROM fyniq.share_issue x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.share_issue_id;
    IF s.id IS NULL OR s.holder_id <> NEW.holder_id OR s.class_account_id <> NEW.class_account_id
       OR s.class_id IS DISTINCT FROM NEW.class_id OR s.shares <> NEW.shares OR s.effective_on <> NEW.effective_on THEN
      RAISE EXCEPTION 'Holding movement % does not equal the share issue it names', NEW.id USING ERRCODE = 'FY288',
        HINT = 'An issue''s movement is its holder, class, shares and effective date (FR-EQ-17).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.transaction_id IS NULL THEN
    RAISE EXCEPTION 'Holding movement % names no equity transaction', NEW.id USING ERRCODE = 'FY288',
      HINT = 'Only an issue is recorded outside an equity transaction (FR-EQ-17).';
  END IF;

  IF NEW.kind = 'reversal' THEN
    SELECT * INTO r FROM fyniq.holding_movement x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.reverses_id;
    IF r.id IS NULL OR r.kind = 'reversal' OR r.transaction_id IS NULL OR r.transaction_id::text IS DISTINCT FROM d.payload->>'reverses_id'
       OR r.holder_id <> NEW.holder_id OR r.class_account_id <> NEW.class_account_id OR r.class_id IS DISTINCT FROM NEW.class_id
       OR r.shares <> -NEW.shares OR NEW.effective_on < r.effective_on THEN
      RAISE EXCEPTION 'Holding movement % does not negate a movement of the transaction its reversal names', NEW.id
        USING ERRCODE = 'FY288', HINT = 'A reversal undoes every movement of one transaction, on or after its date (FR-EQ-17).';
    END IF;
  ELSE
    IF NOT EXISTS (SELECT 1 FROM fyniq.account a WHERE a.organisation_id = NEW.organisation_id AND a.id = NEW.class_account_id
                    AND a.role = 'share-capital' AND a.is_active AND NOT a.is_group) THEN
      RAISE EXCEPTION 'The class of holding movement % is not an active share-capital account', NEW.id USING ERRCODE = 'FY288',
        HINT = 'Move shares of 3010 Ordinary or 3020 Preference share capital (FR-EQ-17).';
    END IF;
    IF NEW.class_id IS NULL THEN
      IF fyniq.share_class_of(NEW.organisation_id, NEW.class_account_id, NULL) IS NOT NULL THEN
        RAISE EXCEPTION 'Holding movement % names no class, and its account has a recorded class', NEW.id USING ERRCODE = 'FY288',
          HINT = 'Name the class the shares move in (FR-EQ-16).';
      END IF;
    ELSE
      SELECT dd.status, v.payload->>'account_id' INTO status, account
        FROM fyniq.document dd
        JOIN fyniq.document_version v ON v.organisation_id = dd.organisation_id AND v.document_id = dd.id AND v.version = dd.current_version
       WHERE dd.organisation_id = NEW.organisation_id AND dd.id = NEW.class_id AND dd.type = 'share_class';
      IF status IS DISTINCT FROM 'posted' OR account IS DISTINCT FROM NEW.class_account_id::text THEN
        RAISE EXCEPTION 'The class of holding movement % is not a recorded class of its account', NEW.id USING ERRCODE = 'FY288';
      END IF;
    END IF;
  END IF;
  IF NEW.shares > 0 AND NEW.kind <> 'reversal'
     AND NOT EXISTS (SELECT 1 FROM fyniq.contact c WHERE c.organisation_id = NEW.organisation_id AND c.id = NEW.holder_id
                       AND (c.is_director OR c.is_shareholder)) THEN
    RAISE EXCEPTION 'The holder shares move to in holding movement % is not a director or shareholder', NEW.id USING ERRCODE = 'FY288',
      HINT = 'Mark the contact a shareholder first (FR-EQ-17).';
  END IF;
  SELECT x.status INTO p FROM fyniq.period x
   WHERE x.organisation_id = NEW.organisation_id AND daterange(x.starts_on, x.ends_on, '[]') @> NEW.effective_on;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'No period covers the effective date %', NEW.effective_on
      USING ERRCODE = 'FY030', HINT = 'Create the period first, or record the transaction on a date inside one.';
  END IF;
  IF p.status = 'closed' THEN
    RAISE EXCEPTION 'The period of % is closed, so no holding moves on it', NEW.effective_on
      USING ERRCODE = 'FY003', HINT = 'Ask an Owner to reopen the period; an effective date is never moved later (FR-EQ-17, INV-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- (000254, 000260, 000265) An exercise takes the steps of an allotment (FY290).
CREATE OR REPLACE FUNCTION fyniq.equity_step_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  k text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The steps of equity transactions are never deleted' USING ERRCODE = 'FY290';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'Step % of an equity transaction is never changed or deleted', OLD.id USING ERRCODE = 'FY290',
      HINT = 'Record the step again: the later one supersedes it, and both stay (FR-EQ-09).';
  END IF;
  SELECT v.payload->>'kind' INTO k
    FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.transaction_id AND d.type = 'equity_transaction' AND d.status = 'posted';
  IF k IS NULL OR EXISTS (SELECT 1 FROM fyniq.holding_movement m WHERE m.organisation_id = NEW.organisation_id AND m.transaction_id = NEW.transaction_id) THEN
    RAISE EXCEPTION 'Equity transaction % is not approved and waiting for ACRA', NEW.transaction_id USING ERRCODE = 'FY290',
      HINT = 'Record the ACRA steps of a transaction between its approval and its effective date (FR-EQ-09).';
  END IF;
  IF NOT ((k IN ('issue_cash', 'instrument_conversion', 'exercise') AND NEW.kind IN ('resolved', 'approval_lodged', 'agm_due', 'allotment_lodged'))
          OR (k = 'transfer' AND NEW.kind IN ('instrument_signed', 'transfer_lodged'))
          OR (k = 'class_conversion' AND NEW.kind IN ('resolved', 'conversion_lodged'))) THEN
    RAISE EXCEPTION 'A % has no step %', replace(k, '_', ' '), replace(NEW.kind, '_', ' ') USING ERRCODE = 'FY290',
      HINT = 'Record the steps its kind takes (FR-EQ-09, FR-EQ-17).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- An exercise's shares come from its grant (FY293): a recorded grant of the movement's holder and class, at the grant's
-- exercise price, and never more options than the grant gave, with its other exercises not reversed and its events.
CREATE FUNCTION fyniq.option_exercise_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     jsonb;
  g     jsonb;
  taken numeric;
BEGIN
  SELECT v.payload INTO t FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.transaction_id AND d.type = 'equity_transaction';
  IF t->>'kind' IS DISTINCT FROM 'exercise' THEN
    RETURN NEW;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.option_pool:' || NEW.organisation_id::text, 0));
  SELECT v.payload INTO g FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id::text = t->>'grant_id' AND d.type = 'option_grant' AND d.status = 'posted';
  IF g IS NULL OR g->>'holder_id' IS DISTINCT FROM NEW.holder_id::text OR g->>'class_account_id' IS DISTINCT FROM NEW.class_account_id::text
     OR (g->>'exercise_price')::numeric IS DISTINCT FROM (t->>'price')::numeric THEN
    RAISE EXCEPTION 'An exercise names a recorded grant of its holder and class, at the grant''s exercise price' USING ERRCODE = 'FY293',
      HINT = 'Exercise a recorded grant''s options, to its holder, at its price (FR-EQ-19).';
  END IF;
  SELECT coalesce(sum(m.shares), 0) INTO taken FROM fyniq.holding_movement m
    JOIN fyniq.document o ON o.organisation_id = m.organisation_id AND o.id = m.transaction_id
    JOIN fyniq.document_version ov ON ov.organisation_id = o.organisation_id AND ov.document_id = o.id AND ov.version = o.current_version
   WHERE m.organisation_id = NEW.organisation_id AND m.kind = 'issue' AND ov.payload->>'kind' = 'exercise'
     AND ov.payload->>'grant_id' = t->>'grant_id'
     AND NOT EXISTS (SELECT 1 FROM fyniq.holding_movement back WHERE back.organisation_id = m.organisation_id AND back.reverses_id = m.id);
  taken := taken + NEW.shares;
  SELECT taken + coalesce(sum((v.payload->>'number')::numeric), 0) INTO taken FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_event' AND d.status = 'posted' AND v.payload->>'grant_id' = t->>'grant_id';
  IF taken > (g->>'number')::numeric THEN
    RAISE EXCEPTION 'The grant''s exercises and events take % options, more than the % it gave', taken, g->>'number' USING ERRCODE = 'FY293',
      HINT = 'Exercise at most what the grant has left (FR-EQ-19).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
-- (000263, 000265) A grant's recorded events and its exercises never take more options than it gave (FY293).
CREATE OR REPLACE FUNCTION fyniq.option_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  e     jsonb;
  given numeric;
  taken numeric;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.option_pool:' || NEW.organisation_id::text, 0));
  SELECT v.payload INTO e FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  SELECT (v.payload->>'number')::numeric INTO given FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.id::text = e->>'grant_id' AND d.type = 'option_grant' AND d.status = 'posted';
  SELECT coalesce(sum((v.payload->>'number')::numeric), 0) INTO taken FROM fyniq.document d
    JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
   WHERE d.organisation_id = NEW.organisation_id AND d.type = 'option_event' AND d.status = 'posted'
     AND v.payload->>'grant_id' = e->>'grant_id';
  -- (000265) and its effective exercises not reversed
  SELECT taken + coalesce(sum(m.shares), 0) INTO taken FROM fyniq.holding_movement m
    JOIN fyniq.document o ON o.organisation_id = m.organisation_id AND o.id = m.transaction_id
    JOIN fyniq.document_version ov ON ov.organisation_id = o.organisation_id AND ov.document_id = o.id AND ov.version = o.current_version
   WHERE m.organisation_id = NEW.organisation_id AND m.kind = 'issue' AND ov.payload->>'kind' = 'exercise'
     AND ov.payload->>'grant_id' = e->>'grant_id'
     AND NOT EXISTS (SELECT 1 FROM fyniq.holding_movement back WHERE back.organisation_id = m.organisation_id AND back.reverses_id = m.id);
  IF given IS NULL OR taken > given THEN
    RAISE EXCEPTION 'The grant''s events and exercises take % options, more than the % it gave', taken, coalesce(given, 0)
      USING ERRCODE = 'FY293', HINT = 'Forfeit or let lapse at most what the grant leaves (FR-EQ-19).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER option_exercise_guard BEFORE INSERT ON fyniq.holding_movement
  FOR EACH ROW WHEN (NEW.kind = 'issue' AND NEW.transaction_id IS NOT NULL)
  EXECUTE FUNCTION fyniq.option_exercise_guard();
