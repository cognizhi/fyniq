# Security monitoring (SEC-30, SEC-31; p1-security-hardening task 8.2, design D7): log-based metrics and alert policies in
# Cloud Monitoring, emailed to the founders. The API and the worker log each security alert as a structured entry
# (internal/domain/security: "event":"fyniq.security_alert", with its rule); Cloud Audit Logs give IAM policy changes and
# Secret Manager reads (data access logs, task 9.1). Each policy fires on the first entry. [live-verify]

locals {
  security_metrics = {
    "security-alerts" = {
      description = "SEC-31's alerts the platform raised: unusual sign-ins, exports and refusals, vendor bank changes, AI pattern hits"
      filter      = "jsonPayload.event=\"fyniq.security_alert\""
    }
    "audit-chain-failures" = {
      description = "An audit trail failed its nightly verification (AUD-03)"
      filter      = "jsonPayload.event=\"fyniq.security_alert\" AND jsonPayload.rule=\"audit_chain\""
    }
    "iam-policy-changes" = {
      description = "Someone changed an IAM policy of the project"
      filter      = "logName:\"cloudaudit.googleapis.com%2Factivity\" AND protoPayload.methodName=\"SetIamPolicy\""
    }
    "secret-reads-outside-the-services" = {
      description = "A secret was read by an identity that is not one of the platform's own service accounts"
      filter = join(" AND ", ["protoPayload.serviceName=\"secretmanager.googleapis.com\"", "protoPayload.methodName:\"AccessSecretVersion\"",
      "NOT protoPayload.authenticationInfo.principalEmail:\"@${var.project}.iam.gserviceaccount.com\""])
    }
  }
}

resource "google_monitoring_notification_channel" "security" {
  for_each     = toset(local.spec.budget.emails)
  project      = var.project
  display_name = "Fyniq ${local.spec.environment} security: ${each.value}"
  type         = "email"
  labels       = { email_address = each.value }
  depends_on   = [google_project_service.apis]
}

resource "google_logging_metric" "security" {
  for_each    = local.security_metrics
  project     = var.project
  name        = "fyniq/${each.key}"
  description = each.value.description
  filter      = each.value.filter
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

resource "google_monitoring_alert_policy" "security" {
  for_each              = local.security_metrics
  project               = var.project
  display_name          = "Fyniq ${local.spec.environment}: ${each.value.description}"
  combiner              = "OR"
  severity              = "CRITICAL"
  notification_channels = [for c in google_monitoring_notification_channel.security : c.id]
  conditions {
    display_name = each.value.description
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.security[each.key].name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "0s"
      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_SUM"
      }
    }
  }
  alert_strategy {
    auto_close = "86400s"
  }
  documentation {
    content   = "See docs/runbooks/incident-response.md: triage, contain, preserve, assess, notify, recover, learn (SEC-32)."
    mime_type = "text/markdown"
  }
}
