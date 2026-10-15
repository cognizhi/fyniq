# The monthly budget as code (open question 9, rev 0.20; NFR-COST-01): alerts at 50%, 80% and 100% to both
# founders by email. Budgets alert and never cap: a cap would stop the service mid-month (design decision).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "billing_account" {
  description = "The billing account's ID (XXXXXX-XXXXXX-XXXXXX), from the founder."
  type        = string
  validation {
    condition     = can(regex("^[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$", var.billing_account))
    error_message = "billing_account must look like 012345-6789AB-CDEF01."
  }
}

variable "project" {
  description = "The project the budget covers, which also holds the alerts' email channels."
  type        = string
}

variable "name" {
  description = "The budget's display name, e.g. \"fyniq-production\"."
  type        = string
}

variable "amount" {
  description = "The monthly amount (production US$400, development and staging US$120: open question 9)."
  type        = number
}

variable "currency" {
  description = "The billing account's currency: a budget must use it. [verify the billing account's currency with the founder]"
  type        = string
  default     = "USD"
}

variable "thresholds" {
  description = "The alert thresholds, as fractions of the amount."
  type        = list(number)
  default     = [0.5, 0.8, 1.0]
}

variable "emails" {
  description = "Who the alerts go to: both founders."
  type        = list(string)
}

data "google_project" "covered" {
  project_id = var.project
}

resource "google_project_service" "monitoring" {
  project            = var.project
  service            = "monitoring.googleapis.com"
  disable_on_destroy = false
}

resource "google_monitoring_notification_channel" "email" {
  for_each     = toset(var.emails)
  depends_on   = [google_project_service.monitoring]
  project      = var.project
  display_name = "Budget alerts: ${each.value}"
  type         = "email"
  labels = {
    email_address = each.value
  }
}

resource "google_billing_budget" "monthly" {
  billing_account = var.billing_account
  display_name    = var.name
  budget_filter {
    projects        = ["projects/${data.google_project.covered.number}"]
    calendar_period = "MONTH"
  }
  amount {
    specified_amount {
      currency_code = var.currency
      units         = tostring(var.amount)
    }
  }
  dynamic "threshold_rules" {
    for_each = var.thresholds
    content {
      threshold_percent = threshold_rules.value
      spend_basis       = "CURRENT_SPEND"
    }
  }
  # Email only: no Pub/Sub topic, so nothing can act on an alert by switching billing or services off.
  all_updates_rule {
    monitoring_notification_channels = [for c in google_monitoring_notification_channel.email : c.id]
    disable_default_iam_recipients   = false
  }
}
