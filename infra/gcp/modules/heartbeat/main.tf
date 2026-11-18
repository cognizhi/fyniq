# The heartbeat's alerts (NFR-OBS-02, CTL-RES-05; p1-operations-dr design D6) [live-verify]. The tick and every platform
# job log "fyniq.heartbeat" with their job once their beat is recorded (internal/outbox), and Cloud Monitoring alerts on
# its absence: independent of the API, the database and the Scheduler, which a dead tick may mean.
#   - The tick: absent 30 minutes, to the named platform owner; absent 60 minutes, to the Owner as well (the escalation:
#     Cloud Monitoring has no acknowledgement to escalate on, so a longer window does).
#   - The hourly jobs: absent 3 hours, to the platform owner.
#   - The daily and longer jobs: an absence alert waits 24 hours at most [verify the alerting limits], too short for a
#     daily job's 26; the hourly job heartbeats.check logs "fyniq.heartbeat_missing" for each one late, and its first
#     line alerts the platform owner.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The environment's own project."
  type        = string
}

variable "environment" {
  description = "staging or production."
  type        = string
}

variable "platform_owner_emails" {
  description = "The named platform owner, told first; empty: the Owners are told from the first alert."
  type        = list(string)
  default     = []
}

variable "owner_channels" {
  description = "The Owners' notification channels (the founders'), told by escalation."
  type        = list(string)
}

locals {
  platform_owner = length(var.platform_owner_emails) > 0 ? [for c in google_monitoring_notification_channel.platform_owner : c.id] : var.owner_channels
  heartbeat      = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.heartbeat.name}\""
  # Each absence alert: the heartbeat, how long it may be absent, and who is told.
  absences = {
    "tick"             = { job = "tick", window = 1800, escalate = false }
    "tick-escalated"   = { job = "tick", window = 3600, escalate = true }
    "audit.anchor"     = { job = "audit.anchor", window = 10800, escalate = false }
    "heartbeats.check" = { job = "heartbeats.check", window = 10800, escalate = false }
  }
}

resource "google_monitoring_notification_channel" "platform_owner" {
  for_each     = toset(var.platform_owner_emails)
  project      = var.project
  display_name = "Fyniq ${var.environment} platform owner: ${each.value}"
  type         = "email"
  labels       = { email_address = each.value }
}

resource "google_logging_metric" "heartbeat" {
  project     = var.project
  name        = "fyniq/heartbeat"
  description = "A heartbeat of the tick or a platform job, recorded (NFR-OBS-02)"
  filter      = "jsonPayload.event=\"fyniq.heartbeat\""
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
    labels {
      key         = "job"
      value_type  = "STRING"
      description = "The tick, or the platform job"
    }
  }
  label_extractors = { job = "EXTRACT(jsonPayload.job)" }
}

resource "google_logging_metric" "missing" {
  project     = var.project
  name        = "fyniq/heartbeat-missing"
  description = "A heartbeat missing or late, as heartbeats.check found it (NFR-OBS-02)"
  filter      = "jsonPayload.event=\"fyniq.heartbeat_missing\""
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

resource "google_monitoring_alert_policy" "absent" {
  for_each              = local.absences
  project               = var.project
  display_name          = "Fyniq ${var.environment}: heartbeat ${each.value.job} absent ${each.value.window / 60} minutes"
  combiner              = "OR"
  severity              = "CRITICAL"
  notification_channels = each.value.escalate ? distinct(concat(local.platform_owner, var.owner_channels)) : local.platform_owner
  conditions {
    display_name = "No heartbeat from ${each.value.job} for ${each.value.window / 60} minutes"
    condition_absent {
      filter   = "${local.heartbeat} AND metric.label.job=\"${each.value.job}\""
      duration = "${each.value.window}s"
      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_SUM"
        cross_series_reducer = "REDUCE_SUM"
      }
    }
  }
  documentation {
    content   = "The ${each.value.job} has not beaten for ${each.value.window / 60} minutes: the Scheduler, the API or the database may be down, so no platform job runs and nothing is reconciled. Check the Scheduler job and the api's logs; open an incident if it lasts (docs/runbooks/incident-response.md)."
    mime_type = "text/markdown"
  }
}

resource "google_monitoring_alert_policy" "missing" {
  project               = var.project
  display_name          = "Fyniq ${var.environment}: a platform job's heartbeat is late"
  combiner              = "OR"
  severity              = "ERROR"
  notification_channels = local.platform_owner
  conditions {
    display_name = "heartbeats.check found a late heartbeat"
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.missing.name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "0s"
      aggregations {
        alignment_period   = "3600s"
        per_series_aligner = "ALIGN_SUM"
      }
    }
  }
  documentation {
    content   = "A daily or longer platform job missed its heartbeat (jobs heartbeat-check names it). Read its latest slot in Compliance › Controls (the job's check) and its logs, and run it again with jobs run-slot."
    mime_type = "text/markdown"
  }
}

output "policies" {
  description = "The heartbeat's alert policies, which scheduler.configured checks are enabled with a channel."
  value       = concat([for p in google_monitoring_alert_policy.absent : p.display_name], [google_monitoring_alert_policy.missing.display_name])
}
