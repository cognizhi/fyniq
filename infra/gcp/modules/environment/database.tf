# Cloud SQL for PostgreSQL 18 (ADR-D1): zonal for staging, regional HA for production (DR-01), private IP only,
# encrypted connections only, CMEK, daily backups and point-in-time recovery (NFR-DR-01: ≤ 5 minutes),
# pgAudit, and deletion protection. Hydra has its own database on the same instance (§12.3).

resource "google_sql_database_instance" "main" {
  project             = var.project
  name                = "fyniq-${local.spec.environment}"
  region              = local.region
  database_version    = "POSTGRES_18"
  encryption_key_name = google_kms_crypto_key.cmek["database"].id
  deletion_protection = true
  settings {
    tier                        = local.spec.database.tier
    edition                     = "ENTERPRISE"
    availability_type           = local.spec.database.availability
    disk_size                   = local.spec.database.disk_gb
    disk_autoresize             = true
    deletion_protection_enabled = true
    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.platform.id
      ssl_mode        = "ENCRYPTED_ONLY"
    }
    backup_configuration {
      enabled                        = true
      location                       = local.region # pinned to Singapore: the default is the Asia multi-region (§11.3, ADR-D1)
      start_time                     = "18:00"      # 02:00 in Singapore
      point_in_time_recovery_enabled = true
      transaction_log_retention_days = 7
      backup_retention_settings {
        retained_backups = 35 # daily backups kept 35 days (SEC-13, §11.9; rev 0.244) [live-verify]
      }
    }
    maintenance_window {
      day  = 7  # Sunday
      hour = 19 # 03:00 in Singapore
    }
    insights_config {
      query_insights_enabled = true
    }
    database_flags {
      name  = "cloudsql.enable_pgaudit"
      value = "on" # pgAudit (§12.2)
    }
    database_flags {
      name  = "pgaudit.log"
      value = "ddl,role" # schema and grant changes; never read or write statements, which could carry personal data (SEC-15)
    }
    database_flags {
      name  = "log_checkpoints"
      value = "on"
    }
    database_flags {
      name  = "log_connections"
      value = "on"
    }
    database_flags {
      name  = "log_disconnections"
      value = "on"
    }
    database_flags {
      name  = "log_hostname"
      value = "on"
    }
    database_flags {
      name  = "log_lock_waits"
      value = "on"
    }
    database_flags {
      name  = "log_duration"
      value = "on"
    }
    database_flags {
      name  = "log_statement"
      value = "ddl"
    }
    database_flags {
      name  = "log_min_error_statement"
      value = "error"
    }
    database_flags {
      name  = "log_min_messages"
      value = "error"
    }
    database_flags {
      name  = "log_min_duration_statement"
      value = "-1"
    }
    database_flags {
      name  = "log_temp_files"
      value = "0"
    }
  }
  lifecycle {
    precondition {
      condition     = local.spec.environment != "production" || local.spec.database.availability == "REGIONAL"
      error_message = "Production's database must be regional HA, a synchronous standby in a second zone (DR-01)."
    }
  }
  depends_on = [google_service_networking_connection.services, google_kms_crypto_key_iam_member.agent]
}

resource "google_sql_database" "databases" {
  for_each = toset(["fyniq", "hydra"])
  project  = var.project
  instance = google_sql_database_instance.main.name
  name     = each.value
}

# The monthly restore drill (DR-06, CTL-RES-02; p1-operations-dr design D2): the jobs' account may clone this instance at a
# point in time, and read, update and delete only its drill copies, <instance>-drill-<time>, which the adapter alone
# names and removes [verify the permissions Cloud SQL asks for a clone] [live-verify].
resource "google_project_iam_custom_role" "drill_clone" {
  project     = var.project
  role_id     = "fyniqRestoreDrillClone"
  title       = "Fyniq restore drill: clone"
  description = "Clone the production instance at a point in time for the monthly restore drill (DR-06)."
  permissions = ["cloudsql.instances.clone", "cloudsql.instances.get"]
}

resource "google_project_iam_custom_role" "drill_copies" {
  project     = var.project
  role_id     = "fyniqRestoreDrillCopies"
  title       = "Fyniq restore drill: its copies"
  description = "Read, unprotect and delete the restore drill's copies of the instance (DR-06)."
  permissions = ["cloudsql.instances.get", "cloudsql.instances.update", "cloudsql.instances.delete"]
}

resource "google_project_iam_member" "drill_clone" {
  project = var.project
  role    = google_project_iam_custom_role.drill_clone.id
  member  = local.member["jobs"]
  condition {
    title       = "fyniq-${local.spec.environment} only"
    description = "The production instance, never another."
    expression  = "resource.name == \"projects/${var.project}/instances/${google_sql_database_instance.main.name}\""
  }
}

resource "google_project_iam_member" "drill_copies" {
  project = var.project
  role    = google_project_iam_custom_role.drill_copies.id
  member  = local.member["jobs"]
  condition {
    title       = "the drill's copies only"
    description = "Instances named for the drill; never the instance itself."
    expression  = "resource.name.startsWith(\"projects/${var.project}/instances/${google_sql_database_instance.main.name}-drill-\")"
  }
}
