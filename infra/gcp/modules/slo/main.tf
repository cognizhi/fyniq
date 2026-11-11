# Service level objectives (NFR-AVAIL-01, NFR-PERF-01, NFR-OBS-01, CTL-RES-03; p1-operations-dr design D5): for each
# public service, 99.9% of its requests answered without a server error each month, and 95% of them within 300 ms over 28
# days, from Cloud Run's own request metrics; alerts when the error budget burns fast (14.4 times over an hour) or slowly
# (6 times over six hours), to the founders; and a dashboard of both [live-verify].

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

variable "services" {
  description = "The public Cloud Run services the SLOs cover, by name."
  type        = list(string)
}

variable "channels" {
  description = "The notification channels the burn-rate alerts go to: the founders'."
  type        = list(string)
}

variable "availability" {
  description = "The share of requests answered without a server error each month (NFR-AVAIL-01: 99.9% at P1)."
  type        = number
  default     = 0.999
}

variable "latency_ms" {
  description = "The answer time 95% of requests meet (NFR-PERF-01: 300 ms at p95)."
  type        = number
  default     = 300
}

locals {
  run = { for s in var.services : s => "resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"${s}\"" }
  # The burn-rate alerts (the Google SRE workbook's): fast pages within the hour, slow catches a steady leak.
  burns = {
    fast = { window = "3600s", rate = 14.4, severity = "CRITICAL" }
    slow = { window = "21600s", rate = 6, severity = "WARNING" }
  }
  alerts = merge([for s in var.services : { for k, b in local.burns : "${s}/${k}" => merge(b, { service = s }) }]...)
}

resource "google_monitoring_custom_service" "run" {
  for_each     = toset(var.services)
  project      = var.project
  service_id   = "fyniq-${var.environment}-${each.key}"
  display_name = "Fyniq ${var.environment}: ${each.key}"
}

resource "google_monitoring_slo" "availability" {
  for_each        = toset(var.services)
  project         = var.project
  service         = google_monitoring_custom_service.run[each.key].service_id
  slo_id          = "availability"
  display_name    = "${each.key}: ${var.availability * 100}% of requests without a server error, each month"
  goal            = var.availability
  calendar_period = "MONTH"
  request_based_sli {
    good_total_ratio {
      bad_service_filter   = "metric.type=\"run.googleapis.com/request_count\" AND ${local.run[each.key]} AND metric.labels.response_code_class=\"5xx\""
      total_service_filter = "metric.type=\"run.googleapis.com/request_count\" AND ${local.run[each.key]}"
    }
  }
}

resource "google_monitoring_slo" "latency" {
  for_each            = toset(var.services)
  project             = var.project
  service             = google_monitoring_custom_service.run[each.key].service_id
  slo_id              = "latency"
  display_name        = "${each.key}: 95% of requests within ${var.latency_ms} ms, over 28 days"
  goal                = 0.95
  rolling_period_days = 28
  request_based_sli {
    distribution_cut {
      distribution_filter = "metric.type=\"run.googleapis.com/request_latencies\" AND ${local.run[each.key]}"
      range {
        max = var.latency_ms
      }
    }
  }
}

resource "google_monitoring_alert_policy" "burn" {
  for_each              = local.alerts
  project               = var.project
  display_name          = "Fyniq ${var.environment}: ${each.value.service}'s error budget burning ${each.value.rate}x over ${trimsuffix(each.value.window, "s")} s"
  combiner              = "OR"
  severity              = each.value.severity
  notification_channels = var.channels
  conditions {
    display_name = "${each.value.service} burns its availability budget at more than ${each.value.rate} times"
    condition_threshold {
      filter          = "select_slo_burn_rate(\"${google_monitoring_slo.availability[each.value.service].id}\", \"${each.value.window}\")"
      comparison      = "COMPARISON_GT"
      threshold_value = each.value.rate
      duration        = "0s"
    }
  }
  documentation {
    content   = "The service answers more server errors than its 99.9% monthly objective allows (NFR-AVAIL-01). Read its traces of failed requests in Cloud Trace, every one of which is kept, and its logs; open an incident if users are affected (docs/runbooks/incident-response.md)."
    mime_type = "text/markdown"
  }
}

resource "google_monitoring_dashboard" "slo" {
  project = var.project
  dashboard_json = jsonencode({
    displayName = "Fyniq ${var.environment}: service levels"
    mosaicLayout = {
      columns = 12
      tiles = flatten([for i, s in var.services : [
        {
          xPos = 0, yPos = i * 4, width = 6, height = 4
          widget = {
            title = "${s}: requests by response class"
            xyChart = { dataSets = [{ timeSeriesQuery = { timeSeriesFilter = {
              filter      = "metric.type=\"run.googleapis.com/request_count\" AND ${local.run[s]}"
              aggregation = { alignmentPeriod = "60s", perSeriesAligner = "ALIGN_RATE", crossSeriesReducer = "REDUCE_SUM", groupByFields = ["metric.label.response_code_class"] }
            } }, plotType = "STACKED_AREA" }] }
          }
        },
        {
          xPos = 6, yPos = i * 4, width = 6, height = 4
          widget = {
            title = "${s}: answer time, 95th percentile"
            xyChart = { dataSets = [{ timeSeriesQuery = { timeSeriesFilter = {
              filter      = "metric.type=\"run.googleapis.com/request_latencies\" AND ${local.run[s]}"
              aggregation = { alignmentPeriod = "60s", perSeriesAligner = "ALIGN_PERCENTILE_95", crossSeriesReducer = "REDUCE_MAX" }
            } }, plotType = "LINE" }] }
          }
        },
      ]])
    }
  })
}

output "slos" {
  description = "Each service's availability and latency objectives."
  value       = { for s in var.services : s => { availability = google_monitoring_slo.availability[s].id, latency = google_monitoring_slo.latency[s].id } }
}
