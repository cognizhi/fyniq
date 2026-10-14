# deploy

| Directory | What |
|---|---|
| `local/` | The Compose stack (`make deps-up`, `make up`) and the Caddyfile for one local origin (ADR-T4) |
| `docker/` | The Dockerfiles: one for every Go service with a release or local flavour, and the web image (§12.9) |

Infrastructure for the cloud environments is in `infra/` (OpenTofu, p0-infra-opentofu).
