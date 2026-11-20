-- The structured information rights of an investor (PRD-001 FR-EQ-28; p2-reporting-planning task 3.4, design D8).
--
-- An investor_right version may now carry information_rights: a list of {kind, days}, each a thing the holder is owed and the days after
-- the end of its period it is due, such as quarterly accounts within 45 days. The calendar reads them to raise one task for each period
-- (internal/domain/deadlines). The key is additive: a version without it is as it was, and the free text in `information` stays beside it.
-- The guard is replaced to keep its rules and to hold this key to its own shape, whoever writes it (FY295).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.investor_right_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  t     text;
  p     jsonb := NEW.payload;
  extra text;
  r     jsonb;
BEGIN
  SELECT d.type INTO t FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.document_id;
  IF t IS DISTINCT FROM 'investor_right' THEN
    RETURN NEW;
  END IF;
  SELECT k INTO extra FROM jsonb_object_keys(p) k
   WHERE k NOT IN ('date', 'holder_id', 'from', 'board_seats', 'observer', 'board_majority', 'information', 'information_rights', 'pro_rata', 'consents',
                   'agreements', 'note') LIMIT 1;
  IF extra IS NOT NULL OR jsonb_typeof(p->'board_seats') IS DISTINCT FROM 'number' OR jsonb_typeof(p->'consents') IS DISTINCT FROM 'array'
     OR jsonb_typeof(p->'observer') IS DISTINCT FROM 'boolean' OR jsonb_typeof(p->'board_majority') IS DISTINCT FROM 'boolean'
     OR jsonb_typeof(p->'pro_rata') IS DISTINCT FROM 'boolean' THEN
    RAISE EXCEPTION 'Investor rights keep their own keys: board seats, an observer, a board majority, pro-rata and consent matters'
      USING ERRCODE = 'FY295', HINT = 'Record the rights the agreements give (FR-EQ-22).';
  END IF;
  IF (p->>'board_seats')::numeric NOT BETWEEN 0 AND 20 OR (p->>'board_seats')::numeric <> trunc((p->>'board_seats')::numeric) THEN
    RAISE EXCEPTION 'Investor rights appoint from 0 to 20 directors' USING ERRCODE = 'FY295',
      HINT = 'Enter the directors the holder may appoint (FR-EQ-22).';
  END IF;
  IF p ? 'information_rights' THEN
    IF jsonb_typeof(p->'information_rights') IS DISTINCT FROM 'array' OR jsonb_array_length(p->'information_rights') > 8 THEN
      RAISE EXCEPTION 'Investor rights hold at most 8 information rights, as a list' USING ERRCODE = 'FY295',
        HINT = 'Name what is owed and the days after its period it is due (FR-EQ-28).';
    END IF;
    FOR r IN SELECT x FROM jsonb_array_elements(p->'information_rights') x LOOP
      IF jsonb_typeof(r) IS DISTINCT FROM 'object' OR EXISTS (SELECT 1 FROM jsonb_object_keys(r) k WHERE k NOT IN ('kind', 'days'))
         OR r->>'kind' NOT IN ('monthly_accounts', 'quarterly_accounts', 'annual_accounts', 'board_pack')
         OR jsonb_typeof(r->'days') IS DISTINCT FROM 'number' OR (r->>'days')::numeric NOT BETWEEN 1 AND 365
         OR (r->>'days')::numeric <> trunc((r->>'days')::numeric) THEN
        RAISE EXCEPTION 'An information right is monthly, quarterly or annual accounts or the board pack, due in 1 to 365 whole days' USING ERRCODE = 'FY295',
          HINT = 'Choose what is owed and the days after its period it is due (FR-EQ-28).';
      END IF;
    END LOOP;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.contact c
                  WHERE c.organisation_id = NEW.organisation_id AND c.id::text = p->>'holder_id'
                    AND (c.is_shareholder OR c.is_investor)) THEN
    RAISE EXCEPTION 'Investor rights are of a contact marked shareholder or investor' USING ERRCODE = 'FY295',
      HINT = 'Mark the contact an investor in Contacts first (FR-EQ-15, FR-EQ-22).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
