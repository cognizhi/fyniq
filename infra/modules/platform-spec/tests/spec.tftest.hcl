# req: NFR-PORT-05
# req: CTL-DP-07
# req: SEC-03

variables {
  environment = "staging"
  budget      = { amount_usd = 120, emails = ["founders@example.com"] }
  anchors     = { retention_days = 30, lock = false }
}

# The spec names every service and job of §12.3 with its own identity, and only the services that
# need the database have a role (the MCP server and the AI orchestrator have none).
run "the_platform_as_data" {
  command = plan
  assert {
    condition     = toset(keys(output.spec.services)) == toset(["api", "ai", "mcp", "worker", "executor", "render", "oauth", "oauth-admin"])
    error_message = "the services are not those of §12.3"
  }
  assert {
    condition     = output.spec.services.mcp.database_role == null && output.spec.services.ai.database_role == null
    error_message = "the MCP server and the AI orchestrator must have no database role (§12.3, SEC-03)"
  }
  assert {
    condition     = output.spec.classes.executor.service == "executor" && output.spec.classes.worker.service == "worker"
    error_message = "the executor must have its own class and service (§12.11)"
  }
  assert {
    condition     = output.spec.budget.thresholds == [0.5, 0.8, 1.0] && output.spec.tick_cron == "*/5 * * * *"
    error_message = "the budget alerts (50%, 80%, 100%) or the 5-minute tick changed"
  }
}

# CTL-DP-07, the local stand-in: nothing is planned outside Singapore. The organisation policy's own
# refusal is checked in staging.
run "a_region_outside_singapore_is_refused" {
  command = plan
  variables { region = "asia-southeast2" }
  expect_failures = [var.region]
}

run "a_tick_that_does_not_divide_the_hour_is_refused" {
  command = plan
  variables { tick_minutes = 7 }
  expect_failures = [var.tick_minutes]
}

run "the_dev_sandbox_has_no_platform" {
  command = plan
  variables { environment = "dev" }
  expect_failures = [var.environment]
}
