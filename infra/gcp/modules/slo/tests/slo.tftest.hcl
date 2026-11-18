# req: NFR-AVAIL-01
# req: NFR-PERF-01
# req: NFR-OBS-01
# req: CTL-RES-03
# req: NFR-COST-02

mock_provider "google" {
  mock_resource "google_monitoring_slo" {
    defaults = { id = "projects/fyniq-prod/services/fyniq-production-api/serviceLevelObjectives/availability" }
  }
}

variables {
  project     = "fyniq-prod"
  environment = "production"
  services    = ["api", "mcp", "oauth"]
  channels    = ["projects/fyniq-prod/notificationChannels/1", "projects/fyniq-prod/notificationChannels/2"]
}

# Each public service has its two objectives from Cloud Run's metrics, and its availability budget pages the founders when
# it burns fast or slowly.
run "the_slos_page_the_founders" {
  command = plan
  assert {
    condition = (length(google_monitoring_slo.availability) == 3 && alltrue([for s in google_monitoring_slo.availability :
      s.goal == 0.999 && s.calendar_period == "MONTH" &&
    strcontains(one(one(s.request_based_sli).good_total_ratio).bad_service_filter, "response_code_class=\"5xx\"")]))
    error_message = "99.9% of each service's requests without a server error, each month (NFR-AVAIL-01)"
  }
  assert {
    condition = (alltrue([for s in google_monitoring_slo.latency : s.goal == 0.95 && s.rolling_period_days == 28 &&
    one(one(one(s.request_based_sli).distribution_cut).range).max == 300]))
    error_message = "95% of each service's requests within 300 ms (NFR-PERF-01)"
  }
  assert {
    condition = (length(google_monitoring_alert_policy.burn) == 6 &&
      google_monitoring_alert_policy.burn["api/fast"].conditions[0].condition_threshold[0].threshold_value == 14.4 &&
      strcontains(google_monitoring_alert_policy.burn["api/fast"].conditions[0].condition_threshold[0].filter, "\"3600s\"") &&
      google_monitoring_alert_policy.burn["mcp/slow"].conditions[0].condition_threshold[0].threshold_value == 6 &&
      strcontains(google_monitoring_alert_policy.burn["mcp/slow"].conditions[0].condition_threshold[0].filter, "\"21600s\"") &&
    alltrue([for p in google_monitoring_alert_policy.burn : length(p.notification_channels) == 2]))
    error_message = "fast (14.4x over an hour) and slow (6x over six hours) burn alerts for each service, to the founders"
  }
  assert {
    condition     = strcontains(google_monitoring_dashboard.slo.dashboard_json, "run.googleapis.com/request_latencies")
    error_message = "a dashboard of the requests and their answer times"
  }
}

# Idle cost: every service but the warm scanner is watched for compute billed in an hour with no request, and the
# founders are told; with no services to watch there is no policy (NFR-COST-02).
run "compute_billed_while_idle_alerts_the_founders" {
  command = plan
  variables {
    idle_services = ["api", "worker", "render", "scanner"]
    warm_services = ["scanner"]
  }
  assert {
    condition = (length(google_monitoring_alert_policy.idle) == 1 &&
      strcontains(google_monitoring_alert_policy.idle[0].conditions[0].condition_prometheus_query_language[0].query, "service_name=~\"api|worker|render\"") &&
      strcontains(google_monitoring_alert_policy.idle[0].conditions[0].condition_prometheus_query_language[0].query, "unless") &&
    length(google_monitoring_alert_policy.idle[0].notification_channels) == 2)
    error_message = "the services but the warm scanner are watched for compute billed with no request, to the founders"
  }
}

run "nothing_watched_no_policy" {
  command = plan
  assert {
    condition     = length(google_monitoring_alert_policy.idle) == 0
    error_message = "no idle policy without services to watch"
  }
}
