-- One sign-in across the apps (PRD-002 Appendix E, NM-INT-12; PRD-001 FR-PPL-18; p1-nemura-fyniq-link 4.2; owner decision of 21 Nov 2026): the link's client may also hold identity.sessions.write, the scope
-- of the one operation by which Nemura tells Fyniq that a person was signed out of, or removed from, Nemura, so Fyniq ends that person's sessions here when they are mapped to a person of the link. It is
-- granted only by a link made after this migration (the client is registered with the scopes then in force); a link made before keeps the six it has, and Nemura's call is refused with its scope code until
-- the link is approved again. No ordinary client, personal token or agent can hold it: the check below only widens what the link's own kind may hold, as the table's other check still refuses the rest.
-- (Widened NOT VALID, validated in 000917.)

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.api_client DROP CONSTRAINT api_client_scopes_format;
ALTER TABLE fyniq.api_client ADD CONSTRAINT api_client_scopes_format CHECK (cardinality(scopes) BETWEEN 1 AND 16 AND (
    (kind <> 'link' AND array_to_string(scopes, ' ') ~ '^fyniq\.(read|write)\.[a-z]+( fyniq\.(read|write)\.[a-z]+)*$')
    OR (kind = 'link' AND array_to_string(scopes, ' ') ~
        '^(people\.projection\.write|people\.availability\.write|people\.holds\.write|people\.holds\.read|payroll\.journal\.submit|payroll\.payments\.submit|identity\.sessions\.write)( (people\.projection\.write|people\.availability\.write|people\.holds\.write|people\.holds\.read|payroll\.journal\.submit|payroll\.payments\.submit|identity\.sessions\.write))*$'))) NOT VALID;
