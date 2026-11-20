-- One sign-in across the apps (PRD-002 NM-INT-12; PRD-001 FR-PPL-18; p1-nemura-fyniq-link 4.2): when a person is signed out of, or removed from, Fyniq, Fyniq tells Nemura (identity.session.revoked, over the
-- link's one event scope) and Nemura ends that person's sessions here. The identity directory stays unreadable to the API: this one function ends the sessions of the members of the transaction's own
-- organisation whose sign-in identity (issuer and subject) is the one named, and returns the IDs of the sessions it ended, so the caller audits each (every session change has its audit event). It changes nothing else and reads no email.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- +goose StatementBegin
CREATE FUNCTION nemura.revoke_sessions_of_identity(p_issuer text, p_subject text, p_at timestamptz) RETURNS SETOF uuid
LANGUAGE sql SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  UPDATE nemura.session s SET revoked_at = p_at
   WHERE s.organisation_id = nemura.require_org() AND s.revoked_at IS NULL AND s.expires_at > p_at
     AND s.member_id IN (SELECT d.member_id FROM nemura.identity_directory d
                          WHERE d.organisation_id = nemura.require_org() AND d.issuer = p_issuer AND d.subject = p_subject)
  RETURNING s.id
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.revoke_sessions_of_identity(text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.revoke_sessions_of_identity(text, text, timestamptz) TO nemura_api;
