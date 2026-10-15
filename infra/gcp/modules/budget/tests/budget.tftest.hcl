# req: NFR-COST-01

mock_provider "google" {
  mock_data "google_project" {
    defaults = { number = "123456789012" }
  }
}

variables {
  billing_account = "012345-6789AB-CDEF01"
  project         = "fyniq-prod"
  name            = "fyniq-production"
  amount          = 400
  emails          = ["founder-one@example.com", "founder-two@example.com"]
}

# The amount is a variable, the alerts go to both founders at 50%, 80% and 100%, and nothing caps.
run "alerts_and_never_a_cap" {
  command = plan
  assert {
    condition     = google_billing_budget.monthly.amount[0].specified_amount[0].units == "400"
    error_message = "the budget is the variable's amount"
  }
  assert {
    condition     = [for r in google_billing_budget.monthly.threshold_rules : r.threshold_percent] == [0.5, 0.8, 1.0]
    error_message = "alerts at 50%, 80% and 100%"
  }
  assert {
    condition     = length(google_monitoring_notification_channel.email) == 2
    error_message = "both founders get the alerts"
  }
  assert {
    condition     = google_billing_budget.monthly.all_updates_rule[0].pubsub_topic == null
    error_message = "a Pub/Sub topic could cap spending; budgets alert only"
  }
  assert {
    condition     = google_billing_budget.monthly.budget_filter[0].projects == toset(["projects/123456789012"])
    error_message = "the budget covers its own project"
  }
}
