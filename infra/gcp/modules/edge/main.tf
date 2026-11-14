# The edge (SEC-23, SEC-31; p1-security-hardening design D4): a global external Application Load Balancer in front of
# the public Cloud Run services, on the public origin's host, with a Google-managed certificate and TLS 1.2 or later.
# Its paths are the local stack's (deploy/local/Caddyfile): /api/, /oauth/ and /ai/ go to their services with the
# prefix taken off, /mcp as it is. Cloud Armor screens every request: the preconfigured OWASP rules (in preview until
# the dev cloud has watched their hits), a per-IP rate-based ban on sign-in and the routes that take no session, and
# adaptive protection. The load balancer tells the services the client's address, country and region in headers,
# which the per-IP limits and the impossible-travel alert read. The services take traffic from the load balancer and
# inside Google Cloud only (environment's edge ingress). [live-verify]

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

variable "region" {
  description = "Where the services run: Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "name" {
  description = "The edge's name: fyniq-<environment>."
  type        = string
}

variable "domain" {
  description = "The public origin's host, such as fyniq.example.com: the certificate's and the only host served."
  type        = string
  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$", var.domain))
    error_message = "domain is a host name, such as fyniq.example.com."
  }
}

variable "services" {
  description = "Each public Cloud Run service behind the edge: its paths, and whether the matched prefix is taken off (as Caddy's handle_path)."
  type = map(object({
    paths = list(string)
    strip = bool
  }))
}

variable "default_service" {
  description = "The service of every other path. The web app's bucket takes its place with cloud-first-deployment."
  type        = string
  default     = "api"
}

variable "limited_paths" {
  description = "The paths of the per-IP rate-based ban: sign-in and the routes that take no session (a CEL regular expression on request.path)."
  type        = string
  default     = "^/api/(auth/|v1/(public/|setup|members$|super-admins$|sign-in-providers|session/(join|recovery)))"
}

variable "rate" {
  description = "The rate-based ban: requests a minute per client address on the limited paths, and how long an address over it is banned."
  type = object({
    per_minute  = number
    ban_seconds = number
  })
  default = { per_minute = 120, ban_seconds = 600 }
}

variable "waf_preview" {
  description = "Whether the OWASP rules only log (preview) or deny. Preview until their hits in the dev cloud are reviewed [live-verify]."
  type        = bool
  default     = true
}

locals {
  # The preconfigured OWASP Core Rule Set rules Cloud Armor evaluates, at sensitivity 1 (fewest false positives).
  waf = ["sqli-v33-stable", "xss-v33-stable", "lfi-v33-stable", "rfi-v33-stable", "rce-v33-stable", "methodenforcement-v33-stable",
  "scannerdetection-v33-stable", "protocolattack-v33-stable", "sessionfixation-v33-stable"]
  # What the load balancer tells the services about the client (internal/api's per-IP limits, internal/domain/security).
  client_headers = ["X-Fyniq-Client-IP: {client_ip_address}", "X-Fyniq-Client-Country: {client_region}",
  "X-Fyniq-Client-Region: {client_region_subdivision}"]
}

resource "google_project_service" "compute" {
  project            = var.project
  service            = "compute.googleapis.com"
  disable_on_destroy = false
}

resource "google_compute_region_network_endpoint_group" "services" {
  for_each              = var.services
  project               = var.project
  name                  = "${var.name}-${each.key}"
  region                = var.region
  network_endpoint_type = "SERVERLESS"
  cloud_run {
    service = each.key
  }
  depends_on = [google_project_service.compute]
}

resource "google_compute_security_policy" "edge" {
  project     = var.project
  name        = var.name
  description = "Cloud Armor for ${var.domain} (SEC-23): OWASP rules, a per-IP rate-based ban on sign-in and public routes, adaptive protection"
  type        = "CLOUD_ARMOR"

  adaptive_protection_config {
    layer_7_ddos_defense_config {
      enable = true
    }
  }

  dynamic "rule" {
    for_each = { for i, r in local.waf : r => i }
    content {
      priority    = 1000 + rule.value
      action      = "deny(403)"
      preview     = var.waf_preview
      description = "OWASP CRS ${rule.key}, sensitivity 1"
      match {
        expr {
          expression = "evaluatePreconfiguredWaf('${rule.key}', {'sensitivity': 1})"
        }
      }
    }
  }

  # The API's internal operations are the AI service's alone, through Cloud Run's internal ingress: never through the
  # edge (PRD-001 §9.3 rev 0.277; Caddy refuses them locally).
  rule {
    priority    = 998
    action      = "deny(404)"
    description = "The API's internal operations, for the AI service only"
    match {
      expr {
        expression = "request.path.startsWith('/api/internal/')"
      }
    }
  }

  # Log4Shell (CVE-2021-44228) lookups are denied from the start: a known exploit, no false positives to review.
  rule {
    priority    = 999
    action      = "deny(403)"
    description = "CVE-2021-44228 (Log4j) lookups"
    match {
      expr {
        expression = "evaluatePreconfiguredExpr('cve-canary')"
      }
    }
  }

  rule {
    priority    = 2000
    action      = "rate_based_ban"
    description = "Per client address on sign-in and the routes that take no session"
    match {
      expr {
        expression = "request.path.matches('${var.limited_paths}')"
      }
    }
    rate_limit_options {
      conform_action   = "allow"
      exceed_action    = "deny(429)"
      enforce_on_key   = "IP"
      ban_duration_sec = var.rate.ban_seconds
      rate_limit_threshold {
        count        = var.rate.per_minute
        interval_sec = 60
      }
    }
  }

  rule {
    priority    = 2147483647
    action      = "allow"
    description = "Everything else, which the services authenticate themselves"
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["*"]
      }
    }
  }
}

resource "google_compute_backend_service" "services" {
  for_each                        = var.services
  project                         = var.project
  name                            = "${var.name}-${each.key}"
  protocol                        = "HTTPS"
  load_balancing_scheme           = "EXTERNAL_MANAGED"
  security_policy                 = google_compute_security_policy.edge.id
  custom_request_headers          = local.client_headers
  custom_response_headers         = ["Strict-Transport-Security: max-age=63072000; includeSubDomains; preload"]
  connection_draining_timeout_sec = 30
  backend {
    group = google_compute_region_network_endpoint_group.services[each.key].id
  }
  log_config {
    enable      = true
    sample_rate = 1
  }
}

resource "google_compute_url_map" "edge" {
  project         = var.project
  name            = var.name
  default_service = google_compute_backend_service.services[var.default_service].id
  host_rule {
    hosts        = [var.domain]
    path_matcher = "fyniq"
  }
  path_matcher {
    name            = "fyniq"
    default_service = google_compute_backend_service.services[var.default_service].id
    dynamic "path_rule" {
      for_each = var.services
      content {
        paths   = path_rule.value.paths
        service = google_compute_backend_service.services[path_rule.key].id
        dynamic "route_action" {
          for_each = path_rule.value.strip ? [1] : []
          content {
            url_rewrite {
              path_prefix_rewrite = "/" # the matched prefix off, as Caddy's handle_path [live-verify]
            }
          }
        }
      }
    }
  }
}

resource "google_compute_managed_ssl_certificate" "edge" {
  project = var.project
  name    = var.name
  managed {
    domains = [var.domain]
  }
}

resource "google_compute_ssl_policy" "edge" {
  project         = var.project
  name            = var.name
  profile         = "RESTRICTED"
  min_tls_version = "TLS_1_2"
}

resource "google_compute_target_https_proxy" "edge" {
  project          = var.project
  name             = var.name
  url_map          = google_compute_url_map.edge.id
  ssl_certificates = [google_compute_managed_ssl_certificate.edge.id]
  ssl_policy       = google_compute_ssl_policy.edge.id
}

resource "google_compute_global_address" "edge" {
  project    = var.project
  name       = var.name
  depends_on = [google_project_service.compute]
}

resource "google_compute_global_forwarding_rule" "https" {
  project               = var.project
  name                  = "${var.name}-https"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  ip_address            = google_compute_global_address.edge.id
  port_range            = "443"
  target                = google_compute_target_https_proxy.edge.id
}

# Plain HTTP only redirects to HTTPS.
resource "google_compute_url_map" "redirect" {
  project = var.project
  name    = "${var.name}-redirect"
  default_url_redirect {
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = false
  }
}

resource "google_compute_target_http_proxy" "redirect" {
  project = var.project
  name    = "${var.name}-redirect"
  url_map = google_compute_url_map.redirect.id
}

resource "google_compute_global_forwarding_rule" "http" {
  project               = var.project
  name                  = "${var.name}-http"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  ip_address            = google_compute_global_address.edge.id
  port_range            = "80"
  target                = google_compute_target_http_proxy.redirect.id
}

output "address" {
  description = "The edge's address: the public origin's DNS A record."
  value       = google_compute_global_address.edge.address
}

output "security_policy" {
  description = "The Cloud Armor policy."
  value       = google_compute_security_policy.edge.id
}
