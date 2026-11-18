-- The prices a provider's own model list gives (PRD-001 AI-82 (a); p1-ai-providers task 4.2, design D4).
--
-- A list such as OpenRouter's gives each model's price: each read keeps it on the model as the provider gave it, as
-- decimal text in a JSON object, never as a number the database could round, and the daily price read prefers it to the
-- other sources. A read that no longer gives one clears it. The check is added NOT VALID and 000355 validates it
-- (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.ai_model ADD COLUMN listed_price jsonb;
ALTER TABLE fyniq.ai_model ADD CONSTRAINT ai_model_listed_price_object
  CHECK (jsonb_typeof(listed_price) = 'object' AND length(listed_price::text) <= 4000) NOT VALID;
GRANT INSERT (listed_price), UPDATE (listed_price) ON fyniq.ai_model TO fyniq_api, fyniq_worker;
