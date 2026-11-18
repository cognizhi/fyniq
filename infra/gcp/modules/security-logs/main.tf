# The central log store (SEC-30; p1-security-hardening task 9.1, design D8) [live-verify]: data-access audit logs for the
# sensitive services, and a sink that sends Cloud Audit Logs, the database's pgAudit entries (CTL-AUD-04) and the platform's
# security alerts to a Cloud Logging
# bucket in the audit project, kept 400 days (the security events' retention, AUD-06) and locked in production. The
# alert policies on them are the environment module's (alerts.tf).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The environment's own project, whose logs are kept."
  type        = string
}

variable "audit_project" {
  description = "Where the log bucket lives: fyniq-audit for production (§12.4), the environment's own project for staging."
  type        = string
}

variable "environment" {
  description = "staging or production: part of the bucket's name."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "retention_days" {
  description = "How long the security logs are kept: the security events' 400 days (AUD-06)."
  type        = number
  default     = 400
}

variable "lock" {
  description = "Lock the bucket's retention. IRREVERSIBLE: no entry can be deleted before it ends. True only in production."
  type        = bool
}

locals {
  # Cloud SQL, Secret Manager, KMS and IAM; Cloud Storage too, unless the anchors module already logs it in this project
  # (staging keeps its anchors in its own project).
  services = concat(["sqladmin.googleapis.com", "secretmanager.googleapis.com", "cloudkms.googleapis.com", "iam.googleapis.com"],
  var.project == var.audit_project ? [] : ["storage.googleapis.com"])
  bucket = "fyniq-${var.environment}-security"
  # Cloud Audit Logs (admin activity, data access, policy denials), the security alerts the platform logs
  # (internal/domain/security: "event":"fyniq.security_alert"), and the database's pgAudit entries: schema and grant
  # changes (pgaudit.log=ddl,role, environment/database.tf), which Cloud SQL writes to its postgres.log with "AUDIT:"
  # (CTL-AUD-04, §11.5's database and platform logs; p1-operations-dr design D4) [live-verify the log name].
  filter = join(" OR ", [
    "logName:\"cloudaudit.googleapis.com\"",
    "jsonPayload.event=\"fyniq.security_alert\"",
    "(resource.type=\"cloudsql_database\" AND logName:\"postgres.log\" AND textPayload:\"AUDIT:\")",
  ])
}

resource "google_project_iam_audit_config" "sensitive" {
  for_each = toset(local.services)
  project  = var.project
  service  = each.value
  audit_log_config {
    log_type = "ADMIN_READ"
  }
  audit_log_config {
    log_type = "DATA_READ"
  }
  audit_log_config {
    log_type = "DATA_WRITE"
  }
}

resource "google_logging_project_bucket_config" "security" {
  project        = var.audit_project
  location       = var.region
  bucket_id      = local.bucket
  retention_days = var.retention_days
  locked         = var.lock
  description    = "Fyniq ${var.environment}: Cloud Audit Logs and the security alerts, kept ${var.retention_days} days (SEC-30, AUD-06)"
}

resource "google_logging_project_sink" "security" {
  project                = var.project
  name                   = "fyniq-security"
  destination            = "logging.googleapis.com/${google_logging_project_bucket_config.security.id}"
  filter                 = local.filter
  unique_writer_identity = true
}

# The sink writes into the bucket in another project only; a sink into a bucket of its own project needs no grant.
resource "google_project_iam_member" "sink_writer" {
  count   = var.project == var.audit_project ? 0 : 1
  project = var.audit_project
  role    = "roles/logging.bucketWriter"
  member  = google_logging_project_sink.security.writer_identity
  condition {
    title      = "fyniq-security-bucket"
    expression = "resource.name.endsWith(\"locations/${var.region}/buckets/${local.bucket}\")"
  }
}

output "bucket" {
  description = "The security log bucket."
  value       = google_logging_project_bucket_config.security.id
}
