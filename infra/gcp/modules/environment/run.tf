# The Cloud Run services and jobs (§12.3, ADR-T7): min instances 0, each as its own service account, all
# egress through the private network. Public services take traffic from anywhere and authenticate it
# themselves; the others accept only internal traffic and their invokers.

locals {
  # Plain settings per service: the environment, the wiring, Hydra's admin port, and the root's own.
  env = { for name in keys(local.runners) : name => merge(
    { FYNIQ_ENV = local.spec.environment },
    contains(local.wired, name) ? { FYNIQ_GCP = local.gcp_config } : {},
    # The project the service's traces go to, and its log lines link to (internal/app, internal/logging).
    contains(local.traced, name) ? { FYNIQ_TRACE_PROJECT = var.project } : {},
    contains(local.oauth_admin, name) ? { FYNIQ_OAUTH_ADMIN_URL = local.url["oauth-admin"] } : {},
    lookup(local.default_env, name, {}),
    # Which secret holds each installation connector secret (the names, never the values; rev 0.139).
    contains(local.connector_readers, name) ? { FYNIQ_CONNECTOR_SECRETS = jsonencode(local.connector_secret_names) } : {},
    # Where the organisations' connector secrets are, and the key the api creates them under (org_secrets.tf).
    contains(local.org_secret_readers, name) ? { FYNIQ_ORG_SECRETS_PROJECT = var.secrets_project, FYNIQ_ORG_SECRETS_KEY = google_kms_crypto_key.cmek["secrets"].id } : {},
    lookup(var.service_env, name, {}),
  ) }
  health = { for name in keys(local.spec.services) : name => contains(["oauth", "oauth-admin"], name) ? "/health/ready" : "/healthz" }
  # The scanner service's sidecar (PRD-001 rev 0.247): clamd, which speaks raw TCP on loopback to the HTTP front beside
  # it, keeps its signatures current in the background (so its CPU is always allocated) and holds them in memory, about
  # 1.2 GB and twice that while it reloads them [live-verify the sizes]. The front reports ready only once clamd answers
  # (/readyz), so no file reaches an instance before its signatures are loaded.
  sidecars = { scanner = { name = "clamd", memory = "3Gi" } }
}

resource "google_cloud_run_v2_service" "services" {
  for_each = local.spec.services
  project  = var.project
  name     = each.key
  location = local.region
  # A public service takes traffic from the edge's load balancer and from inside Google Cloud only (SEC-23, the edge module).
  ingress             = each.value.public ? "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER" : "INGRESS_TRAFFIC_INTERNAL_ONLY"
  deletion_protection = local.spec.environment == "production"
  # Binary Authorization's project policy admits only images the release workflow attested (SEC-22, the binauthz
  # module in the shared root). No breakglass justification, ever.
  binary_authorization {
    use_default = true
  }
  template {
    service_account                  = local.email[each.key]
    timeout                          = contains(["worker", "executor"], each.key) ? "1800s" : "300s" # Cloud Tasks waits up to 30 minutes
    max_instance_request_concurrency = 40
    scaling {
      # One scanner kept warm in production: clamd takes about a minute to load its signatures (rev 0.247).
      min_instance_count = each.key == "scanner" && local.spec.environment == "production" ? 1 : 0
      max_instance_count = each.value.max_instances
    }
    vpc_access {
      network_interfaces {
        network    = google_compute_network.platform.id
        subnetwork = google_compute_subnetwork.run.id
      }
      egress = "ALL_TRAFFIC"
    }
    containers {
      name  = each.key
      image = var.placeholder_image
      args  = lookup(local.args, each.key, null)
      # A service with a sidecar starts after it.
      depends_on = contains(keys(local.sidecars), each.key) ? [local.sidecars[each.key].name] : null
      ports {
        container_port = each.value.port
      }
      resources {
        limits   = { cpu = "1", memory = "512Mi" }
        cpu_idle = true
      }
      dynamic "env" {
        for_each = local.env[each.key]
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = local.secret_env[each.key]
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.all[env.value].secret_id
              version = "latest"
            }
          }
        }
      }
      startup_probe {
        # The scanner waits for clamd's signatures: its readiness asks clamd, for up to 5 minutes.
        period_seconds    = contains(keys(local.sidecars), each.key) ? 10 : null
        failure_threshold = contains(keys(local.sidecars), each.key) ? 30 : null
        http_get {
          path = contains(keys(local.sidecars), each.key) ? "/readyz" : local.health[each.key]
        }
      }
      liveness_probe {
        http_get {
          path = local.health[each.key]
        }
      }
    }
    dynamic "containers" {
      for_each = contains(keys(local.sidecars), each.key) ? [local.sidecars[each.key]] : []
      content {
        name  = containers.value.name
        image = var.placeholder_image
        resources {
          limits   = { cpu = "1", memory = containers.value.memory }
          cpu_idle = false
        }
      }
    }
  }
  lifecycle {
    ignore_changes = [template[0].containers[0].image, template[0].containers[1].image, client, client_version] # deployed by digest by the release workflow
  }
  depends_on = [google_service_account.all, google_secret_manager_secret_iam_member.access]
}

resource "google_cloud_run_v2_job" "jobs" {
  for_each            = local.spec.jobs
  project             = var.project
  name                = each.key
  location            = local.region
  deletion_protection = local.spec.environment == "production"
  binary_authorization {
    use_default = true # as the services (SEC-22)
  }
  template {
    task_count = 1
    template {
      service_account = local.email[each.key]
      max_retries     = 1
      timeout         = each.key == "jobs" ? "86400s" : "3600s"
      vpc_access {
        network_interfaces {
          network    = google_compute_network.platform.id
          subnetwork = google_compute_subnetwork.run.id
        }
        egress = "ALL_TRAFFIC"
      }
      containers {
        image = var.placeholder_image
        args  = lookup(local.args, each.key, null)
        resources {
          limits = { cpu = "1", memory = "512Mi" }
        }
        dynamic "env" {
          for_each = local.env[each.key]
          content {
            name  = env.key
            value = env.value
          }
        }
        dynamic "env" {
          for_each = local.secret_env[each.key]
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = google_secret_manager_secret.all[env.value].secret_id
                version = "latest"
              }
            }
          }
        }
      }
    }
  }
  lifecycle {
    ignore_changes = [template[0].template[0].containers[0].image, client, client_version]
  }
  depends_on = [google_service_account.all, google_secret_manager_secret_iam_member.access]
}

# The public services authenticate every request themselves (sessions, OAuth tokens, the tick's OIDC
# token); the edge's load balancer, with Cloud Armor, is the only way in from outside (SEC-23). Hydra's admin
# port takes internal traffic only and has no further check, as in the local stack: the network is its
# boundary [verify in staging that Direct VPC egress counts as internal].
resource "google_cloud_run_v2_service_iam_member" "public" {
  for_each = { for name, s in local.spec.services : name => s if s.public || s.open_inside }
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services[each.key].name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Cloud Run pulls the release images from the CI project's Artifact Registry.
resource "google_artifact_registry_repository_iam_member" "pull" {
  count      = var.artifact_registry == null ? 0 : 1
  project    = var.artifact_registry.project
  location   = var.artifact_registry.location
  repository = var.artifact_registry.repository
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:service-${local.number}@serverless-robot-prod.iam.gserviceaccount.com"
}

# The private render service (§12.3, rev 0.96) is invoked by the api and the worker only, as their own accounts.
resource "google_cloud_run_v2_service_iam_member" "render_invoker" {
  for_each = toset(["api", "worker"])
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services["render"].name
  role     = "roles/run.invoker"
  member   = local.member[each.key]
}

# The private scanner service (PRD-001 rev 0.247) is invoked by the api and the worker only, as their own accounts.
resource "google_cloud_run_v2_service_iam_member" "scanner_invoker" {
  for_each = toset(["api", "worker"])
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services["scanner"].name
  role     = "roles/run.invoker"
  member   = local.member[each.key]
}
