---
key: reports.dashboards
type: task
title: Arrange your own dashboard
summary: Choosing, adding, ordering and resizing KPI tiles, which arrangement shows, what a tile shows, and the spend alerts
sources:
  - docs/user-guide/dashboards.md sha256:b6f13008425e326726b36ec32b153fbb0cbed7f51153add5718232168d7a3acd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-23
aliases:
  - dashboard
  - kpi tiles
  - arrange the dashboard
  - spend alert
  - anomaly alert
---

# Arrange your own dashboard

## Find it

- **Reports** has a **Dashboard**, which shows the KPIs the person watches as tiles.

(src: docs/user-guide/dashboards.md#find-it)

## Which arrangement you see

- A person sees their own arrangement if they saved one; if not, the arrangement an Owner or Administrator set for their role; if there is none, the default: cash, runway, net burn, revenue, gross margin and the year's net profit.

(src: docs/user-guide/dashboards.md#which-arrangement-you-see)

## Arrange it

- **Add a tile** from the list of KPIs; move a tile earlier or later, choose its width (a third, half or the whole row), or remove it. **Save my dashboard** keeps it as theirs; **Use the default** clears it, so the role's arrangement or the default shows again.
- Each tile is placed once, and a dashboard holds up to 24 tiles.

(src: docs/user-guide/dashboards.md#arrange-it)

## What a tile shows

- A tile shows the KPI as the server worked it out, the month before beside it, and when it was read; selecting it shows what is behind it. A tile shows only what the person's own roles read: a layout never gives anyone figures they could not already read.

(src: docs/user-guide/dashboards.md#what-a-tile-shows)

## Alerts about spend

- When a closed month's spend in an expense category is well above its trend, the people who approve budgets are told. The trend is the mean of the months before it (at least six); the spend must be more than three standard deviations above the mean and at least a tenth above it. Each category and month is alerted once.

(src: docs/user-guide/dashboards.md#alerts-about-spend)
