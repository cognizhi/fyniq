# A private network: Cloud SQL has a private IP only (`sql.restrictPublicIp`), Cloud Run reaches it and
# Hydra's admin port through Direct VPC egress, and outbound calls to providers leave through Cloud NAT.

resource "google_compute_network" "platform" {
  project                 = var.project
  name                    = "platform"
  auto_create_subnetworks = false
  depends_on              = [google_project_service.apis]
}

# Nothing comes in from outside: Cloud Run's egress and the database's private IP need no ingress rule.
resource "google_compute_firewall" "deny_ingress" {
  project   = var.project
  name      = "deny-all-ingress"
  network   = google_compute_network.platform.id
  direction = "INGRESS"
  priority  = 65534
  deny {
    protocol = "all"
  }
  source_ranges = ["0.0.0.0/0"]
  log_config {
    metadata = "INCLUDE_ALL_METADATA"
  }
}

resource "google_compute_subnetwork" "run" {
  project                  = var.project
  name                     = "run"
  region                   = local.region
  network                  = google_compute_network.platform.id
  ip_cidr_range            = "10.10.0.0/24"
  private_ip_google_access = true
  log_config {
    aggregation_interval = "INTERVAL_10_MIN"
    flow_sampling        = 0.5
    metadata             = "INCLUDE_ALL_METADATA"
  }
}

resource "google_compute_global_address" "services" {
  project       = var.project
  name          = "google-managed-services"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 20
  network       = google_compute_network.platform.id
}

resource "google_service_networking_connection" "services" {
  network                 = google_compute_network.platform.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.services.name]
}

resource "google_compute_router" "egress" {
  project = var.project
  name    = "egress"
  region  = local.region
  network = google_compute_network.platform.id
}

resource "google_compute_router_nat" "egress" {
  project                            = var.project
  name                               = "egress"
  router                             = google_compute_router.egress.name
  region                             = local.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}
