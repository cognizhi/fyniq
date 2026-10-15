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
        retained_backups = 14
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
  depends_on = [google_service_networking_connection.services, google_kms_crypto_key_iam_member.agent]
}

resource "google_sql_database" "databases" {
  for_each = toset(["fyniq", "hydra"])
  project  = var.project
  instance = google_sql_database_instance.main.name
  name     = each.value
}
