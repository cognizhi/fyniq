# The organisation policies of PRD-001 §12.4: resources only in Singapore (CTL-DP-07), no service-account
# keys (CTL-IAM-04, SEC-04), uniform bucket access, no public IP on Cloud SQL; and CMEK required for the
# database and storage where cmek_parents says (CTL-DP-01, `gcp.restrictNonCmekServices`).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "parent" {
  description = "Where the policies apply: organizations/<id> (the founder's organisation)."
  type        = string
  validation {
    condition     = can(regex("^(organizations|folders)/[0-9]+$", var.parent))
    error_message = "parent must be organizations/<id> or folders/<id>."
  }
}

variable "cmek_parents" {
  description = "The projects (projects/<id>) where Cloud SQL and Cloud Storage must use a customer-managed key."
  type        = list(string)
  default     = []
}

locals {
  enforced = toset([
    "iam.disableServiceAccountKeyCreation", # CTL-IAM-04: no keys; CI uses Workload Identity Federation
    "iam.disableServiceAccountKeyUpload",
    "storage.uniformBucketLevelAccess",
    "sql.restrictPublicIp", # the database has a private IP only (§12.3)
  ])
}

resource "google_org_policy_policy" "locations" {
  name   = "${var.parent}/policies/gcp.resourceLocations"
  parent = var.parent
  spec {
    rules {
      values {
        allowed_values = ["in:asia-southeast1-locations"]
      }
    }
  }
}

resource "google_org_policy_policy" "enforced" {
  for_each = local.enforced
  name     = "${var.parent}/policies/${each.value}"
  parent   = var.parent
  spec {
    rules {
      enforce = "TRUE"
    }
  }
}

resource "google_org_policy_policy" "cmek" {
  for_each = toset(var.cmek_parents)
  name     = "${each.value}/policies/gcp.restrictNonCmekServices"
  parent   = each.value
  spec {
    rules {
      values {
        denied_values = ["sqladmin.googleapis.com", "storage.googleapis.com"]
      }
    }
  }
}
