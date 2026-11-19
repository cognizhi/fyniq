---
key: projects.costs
title: Put costs on a project and bill them on
mode: expert
roles: [owner, controller, accountant, approver, employee]
requirements: [FR-PRJ-08, FR-DIM-02]
status: draft            # provisional until the owner signs off (PRD-001 rev 0.297)
---

# Put costs on a project and bill them on

## What it is

A cost belongs to a project when its line carries the project as a dimension value: on a bill, an expense claim receipt or trip, a card or
bank line, or a journal. The project's profit then counts it. This page covers the two extras for bills and claims: billing a cost on to
the customer, and a project on a purchase order.

## Bill a cost on to the customer

On a bill line or a claim line that carries a project, tick **Bill this cost to the customer** and, if you add a margin, type the
**Markup (%)**, from 0 to 999.99. Once the bill or claim is posted, the project's unbilled list shows the cost plus the markup, rounded to
the cent. A line that is billable must carry a project; Fyniq says so, naming the line, if it does not. A markup belongs to a billable line.

## A purchase order for a project

Choose the **Project** on a purchase order. Every line of a bill made from the order then carries the project, so what you buy for the
project is its cost without choosing it again on each bill.
