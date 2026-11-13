-- Where a security event came from (PRD-001 SEC-31, rev 0.246; p1-security-hardening task 8.1, design D7): the country
-- and region the edge's load balancer adds to the request (Cloud Armor's geolocation; locally a stand-in header), kept
-- beside the chain with the IP address and user agent (AUD-06, 000285), fingerprinted with them in the event's body, and
-- erased with them after the security-log retention. An event the edge said nothing about keeps neither, and its
-- fingerprint is the same as before this migration. The impossible-travel alert reads them.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.audit_request_detail ADD COLUMN country text NOT NULL DEFAULT '';
ALTER TABLE fyniq.audit_request_detail ADD COLUMN region text NOT NULL DEFAULT '';
ALTER TABLE fyniq.audit_request_detail ADD CONSTRAINT audit_request_detail_country_code CHECK (country ~ '^([A-Z]{2})?$') NOT VALID;
ALTER TABLE fyniq.audit_request_detail ADD CONSTRAINT audit_request_detail_region_length CHECK (length(region) <= 16) NOT VALID;

-- Whoever writes a security event writes its place with it, as its IP address and user agent (000285).
GRANT INSERT (country, region) ON fyniq.audit_request_detail TO fyniq_api, fyniq_worker, fyniq_executor;
