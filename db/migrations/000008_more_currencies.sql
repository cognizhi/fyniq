-- Three more ISO 4217 currencies, from the ISO 4217 list maintained by SIX
-- (https://www.six-group.com/en/products-services/financial-information/data-standards.html): the
-- differential tests exercise 3- and 4-decimal currencies, and CLF reaches NUMERIC(20,4)'s scale.
-- internal/domain/money keeps the same list (a test compares them).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

INSERT INTO fyniq.currency (code, minor_units, name) VALUES
  ('CLF', 4, 'Unidad de Fomento'),
  ('OMR', 3, 'Rial Omani'),
  ('TND', 3, 'Tunisian dinar');
