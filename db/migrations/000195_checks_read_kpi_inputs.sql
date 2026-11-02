-- The checks engine reads the KPIs' entered counts (PRD-001 FR-CTL-01, C.1 #36; p1-checks task 3.5): the runway check
-- works the KPIs out as the KPI page does, and the worker runs the checks. Reading only: the worker never enters counts.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

GRANT SELECT ON fyniq.kpi_input TO fyniq_worker;
