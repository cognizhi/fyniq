-- Account recovery's cooling-off is the organisation's setting (PRD-001 SEC-35; rev 0.253, the owner's decision of 12
-- Nov 2026): security.recovery_cooling_off_hours, 24 to 72 hours, under dual control, 24 when the organisation has not
-- changed it. A person who belongs to several organisations is held for the longest of theirs.
--
-- recovery_cooling_off reads, in each organisation of the tenant directory where the person is an active member, the
-- value in force on that organisation's own date, as idle_timeout does (000233), and keeps the longest; a value outside
-- 24 to 72 hours, or none, counts as the default p_default. recover_account now holds the person for that long; its
-- signature is unchanged, its p_cooling the default.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION fyniq.recovery_cooling_off(p_user uuid, p_now timestamptz, p_default interval) RETURNS interval
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  previous text := current_setting('app.org_id', true);
  longest  interval := p_default;
  org      uuid;
  zone     text;
  hours    text;
BEGIN
  FOR org IN SELECT t.organisation_id FROM fyniq.tenant_directory t ORDER BY t.created_at, t.organisation_id LOOP
    PERFORM set_config('app.org_id', org::text, true);
    IF EXISTS (SELECT 1 FROM fyniq.user_membership m WHERE m.organisation_id = org AND m.user_id = p_user AND m.status = 'active') THEN
      SELECT o.time_zone INTO zone FROM fyniq.organisation o WHERE o.organisation_id = org;
      hours := NULL;
      SELECT s.value INTO hours FROM fyniq.organisation_setting s
       WHERE s.organisation_id = org AND s.key = 'security.recovery_cooling_off_hours'
         AND s.effective_from <= (p_now AT TIME ZONE coalesce(nullif(zone, ''), 'UTC'))::date
       ORDER BY s.effective_from DESC, s.version DESC LIMIT 1;
      IF hours ~ '^[0-9]{1,2}$' AND hours::integer BETWEEN 24 AND 72 THEN
        longest := greatest(longest, hours::integer * interval '1 hour');
      END IF;
    END IF;
  END LOOP;
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  RETURN longest;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.recovery_cooling_off(uuid, timestamptz, interval) FROM PUBLIC;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fyniq.recover_account(p_session bytea, p_code bytea, p_confirmation bytea, p_new_hashes bytea[], p_now timestamptz,
  p_cooling interval)
  RETURNS TABLE (recovery_id uuid, user_id uuid, confirmed_by uuid, confirming_organisation uuid, cooling_off_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  who  uuid := fyniq.live_session_user(p_session, p_now);
  code fyniq.user_recovery_code;
  conf fyniq.recovery_confirmation;
  rid  uuid;
  cool interval;
BEGIN
  SELECT * INTO conf FROM fyniq.recovery_confirmation c
   WHERE c.code_hash = p_confirmation AND c.user_id = who AND c.used_at IS NULL AND c.expires_at > p_now FOR UPDATE;
  IF who IS NOT NULL AND conf.id IS NOT NULL THEN
    SELECT * INTO code FROM fyniq.user_recovery_code c WHERE c.code_hash = p_code AND c.user_id = who AND c.used_at IS NULL FOR UPDATE;
  END IF;
  IF who IS NULL OR conf.id IS NULL OR code.id IS NULL THEN
    RAISE EXCEPTION 'The recovery is not valid, used or expired' USING ERRCODE = 'FY277',
      HINT = 'Check both codes, or ask the Owner or Administrator for a new confirmation.';
  END IF;
  IF cardinality(p_new_hashes) <> 10 THEN
    RAISE EXCEPTION 'A person keeps ten recovery codes, not %', cardinality(p_new_hashes) USING ERRCODE = 'FY277';
  END IF;
  cool := fyniq.recovery_cooling_off(who, p_now, p_cooling);
  UPDATE fyniq.recovery_confirmation c SET used_at = p_now WHERE c.id = conf.id;
  UPDATE fyniq.user_recovery_code c SET used_at = p_now WHERE c.user_id = who AND c.used_at IS NULL;
  INSERT INTO fyniq.user_recovery_code (user_id, code_hash, created_at) SELECT who, h, p_now FROM unnest(p_new_hashes) AS h;
  UPDATE fyniq.user_session s SET step_up_at = p_now, step_up_method = 'recovery' WHERE s.token_hash = p_session;
  UPDATE fyniq.user_session s SET ended_at = greatest(p_now, s.created_at)
   WHERE s.ended_at IS NULL AND s.token_hash <> p_session
     AND EXISTS (SELECT 1 FROM fyniq.user_identity i WHERE i.issuer = s.issuer AND i.subject = s.subject AND i.user_id = who);
  INSERT INTO fyniq.account_recovery (user_id, recovery_code_id, confirmation_id, recovered_at, cooling_off_until)
  VALUES (who, code.id, conf.id, p_now, p_now + cool) RETURNING id INTO rid;
  RETURN QUERY SELECT rid, who, conf.confirmed_by, conf.confirming_organisation, p_now + cool;
END
$$;
-- +goose StatementEnd
