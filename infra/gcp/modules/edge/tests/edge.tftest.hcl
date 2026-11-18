# req: SEC-23
# req: SEC-31

mock_provider "google" {}

variables {
  project = "fyniq-staging"
  name    = "fyniq-staging"
  domain  = "staging.fyniq.example"
  services = {
    api   = { paths = ["/api/*"], strip = true }
    oauth = { paths = ["/oauth/*"], strip = true }
    ai    = { paths = ["/ai/*"], strip = true }
    mcp   = { paths = ["/mcp", "/mcp/*"], strip = false }
  }
}

# Every request passes Cloud Armor: the OWASP rules, a per-IP ban on sign-in and the public routes, adaptive protection;
# every service learns the client's address, country and region from the load balancer; only HTTPS on TLS 1.2 or later.
run "the_edge_screens_every_request" {
  command = plan
  assert {
    condition     = length([for r in google_compute_security_policy.edge.rule : r if startswith(r.match[0].expr[0].expression, "evaluatePreconfiguredWaf(") && r.preview]) == 9
    error_message = "the nine OWASP rule sets, in preview until their hits are reviewed"
  }
  assert {
    condition = length([for r in google_compute_security_policy.edge.rule : r if r.action == "rate_based_ban" &&
      r.rate_limit_options[0].enforce_on_key == "IP" && strcontains(r.match[0].expr[0].expression, "auth/") &&
    strcontains(r.match[0].expr[0].expression, "v1/(public/|setup")]) == 1
    error_message = "a per-IP ban on sign-in, the public documents, setup, joining and recovery"
  }
  assert {
    condition     = length([for r in google_compute_security_policy.edge.rule : r if r.match[0].expr[0].expression == "evaluatePreconfiguredExpr('cve-canary')" && !r.preview && r.action == "deny(403)"]) == 1
    error_message = "Log4Shell lookups are denied from the start"
  }
  assert {
    condition     = length([for r in google_compute_security_policy.edge.rule : r if r.match[0].expr[0].expression == "request.path.startsWith('/api/internal/')" && !r.preview && r.action == "deny(404)"]) == 1
    error_message = "the API's internal operations, the AI service's alone, are refused at the edge (PRD-001 §9.3 rev 0.277)"
  }
  assert {
    condition     = google_compute_security_policy.edge.adaptive_protection_config[0].layer_7_ddos_defense_config[0].enable
    error_message = "adaptive protection is on"
  }
  assert {
    condition = alltrue([for b in google_compute_backend_service.services :
    b.security_policy != "" && toset(b.custom_request_headers) == toset(["X-Fyniq-Client-IP: {client_ip_address}", "X-Fyniq-Client-Country: {client_region}", "X-Fyniq-Client-Region: {client_region_subdivision}"])])
    error_message = "every service is behind Cloud Armor and learns the client's address, country and region"
  }
  assert {
    condition     = google_compute_ssl_policy.edge.min_tls_version == "TLS_1_2" && google_compute_ssl_policy.edge.profile == "RESTRICTED"
    error_message = "TLS 1.2 or later, with the restricted cipher suites"
  }
  assert {
    condition     = google_compute_url_map.redirect.default_url_redirect[0].https_redirect && google_compute_global_forwarding_rule.http.port_range == "80"
    error_message = "plain HTTP only redirects to HTTPS"
  }
  assert {
    condition     = length([for p in google_compute_url_map.edge.path_matcher[0].path_rule : p if contains(p.paths, "/api/*") && length(p.route_action) == 1]) == 1 && length([for p in google_compute_url_map.edge.path_matcher[0].path_rule : p if contains(p.paths, "/mcp") && length(p.route_action) == 0]) == 1
    error_message = "the paths are the local stack's: /api/ without its prefix, /mcp as it is"
  }
  assert {
    condition     = [for h in google_compute_url_map.edge.host_rule : h.hosts] == [toset(["staging.fyniq.example"])]
    error_message = "only the public origin's host"
  }
  assert {
    condition     = google_compute_managed_ssl_certificate.edge.managed[0].domains == tolist(["staging.fyniq.example"])
    error_message = "the public origin's host has its managed certificate"
  }
}

run "the_owasp_rules_deny_once_reviewed" {
  command = plan
  variables {
    waf_preview = false
  }
  assert {
    condition     = length([for r in google_compute_security_policy.edge.rule : r if startswith(r.match[0].expr[0].expression, "evaluatePreconfiguredWaf(") && !r.preview && r.action == "deny(403)"]) == 9
    error_message = "out of preview, the OWASP rules deny"
  }
}
