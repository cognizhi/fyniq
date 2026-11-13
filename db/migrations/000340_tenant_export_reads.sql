-- The full tenant export reads the organisation whole (PRD-001 FR-MIG-04; p1-workbook-migration task 8.1, design D10).
-- tenant_tables lists every table of the schema that holds an organisation_id, partitions left out, with its columns
-- in order; tenant_rows answers a page of the organisation's rows of one of them, each as row_to_json writes it. Both
-- run as their caller (SECURITY INVOKER), so the caller's grants and row-level security decide what they read: the
-- worker, which prepares the export, reads every table the export's catalogue keeps. It already reads all but four,
-- which it is granted here.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.tenant_tables() RETURNS TABLE (table_name text, column_names text[])
LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT c.relname::text, array_agg(a.attname::text ORDER BY a.attnum)
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
   WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p') AND NOT c.relispartition
     AND EXISTS (SELECT 1 FROM pg_attribute o WHERE o.attrelid = c.oid AND o.attname = 'organisation_id' AND NOT o.attisdropped)
   GROUP BY c.relname
   ORDER BY c.relname
$$;
-- +goose StatementEnd

-- A page of the transaction's organisation's rows of p_table, after the row whose id is p_after (NULL: from the first),
-- in id order, at most p_limit; a table without a uuid id comes whole. Only a table tenant_tables lists is read.
-- +goose StatementBegin
CREATE FUNCTION fyniq.tenant_rows(p_table text, p_after uuid, p_limit integer) RETURNS SETOF text
LANGUAGE plpgsql STABLE SECURITY INVOKER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.current_org();
  has_id boolean;
BEGIN
  IF org IS NULL THEN
    RAISE EXCEPTION 'The tenant export reads one organisation: none is set' USING ERRCODE = '42501';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.tenant_tables() t WHERE t.table_name = p_table) THEN
    RAISE EXCEPTION 'The tenant export reads only the organisation''s tables: % is not one', p_table USING ERRCODE = '22023';
  END IF;
  IF p_limit IS NULL OR p_limit < 1 OR p_limit > 10000 THEN
    RAISE EXCEPTION 'A page of the tenant export holds 1 to 10000 rows, not %', p_limit USING ERRCODE = '22023';
  END IF;
  SELECT EXISTS (SELECT 1 FROM pg_attribute a
                  WHERE a.attrelid = format('fyniq.%I', p_table)::regclass AND a.attname = 'id' AND NOT a.attisdropped
                    AND a.atttypid = 'uuid'::regtype) INTO has_id;
  IF has_id THEN
    RETURN QUERY EXECUTE format(
      'SELECT row_to_json(t)::text FROM fyniq.%I t WHERE t.organisation_id = $1 AND ($2::uuid IS NULL OR t.id > $2) ORDER BY t.id LIMIT $3',
      p_table) USING org, p_after, p_limit;
  ELSE
    RETURN QUERY EXECUTE format('SELECT row_to_json(t)::text FROM fyniq.%I t WHERE t.organisation_id = $1', p_table) USING org;
  END IF;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION fyniq.tenant_tables() FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.tenant_rows(text, uuid, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.tenant_tables() TO fyniq_worker;
GRANT EXECUTE ON FUNCTION fyniq.tenant_rows(text, uuid, integer) TO fyniq_worker;

-- The organisation's accounting framework and policies, and its announcements and their dismissals, which the worker
-- did not read before: the export keeps them (design D10's catalogue).
GRANT SELECT ON fyniq.accounting_framework, fyniq.accounting_policy, fyniq.announcement, fyniq.announcement_dismissal
  TO fyniq_worker;
