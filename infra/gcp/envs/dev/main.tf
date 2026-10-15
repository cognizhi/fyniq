# The dev project (§12.4: a sandbox). It runs no platform: the release build refuses FYNIQ_ENV=dev
# (RUNTIME-FLAVOUR-01) and local adapters never leave a laptop or CI (DEV-07). It has its own budget.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "= 8.5.0" }
  }
  backend "gcs" {
    prefix = "envs/dev"
  }
}

variable "project" {
  description = "The dev project."
  type        = string
  default     = "fyniq-dev"
}

variable "billing_account" {
  description = "The billing account (from the founder)."
  type        = string
}

variable "budget_emails" {
  description = "Who gets the budget alerts: both founders."
  type        = list(string)
}

variable "budget_usd" {
  description = "Development and staging: US$120 a month (open question 9)."
  type        = number
  default     = 120
}

provider "google" {
  project        = var.project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", environment = "dev" }
}

module "budget" {
  source          = "../../modules/budget"
  billing_account = var.billing_account
  project         = var.project
  name            = "fyniq-dev"
  amount          = var.budget_usd
  emails          = var.budget_emails
}
