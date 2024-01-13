provider "vault" {}
provider "nomad" {}

resource "nomad_acl_policy" "logs_reader" {
  name = "logs-reader"

  rules_hcl = <<EOT
agent {
  policy = "read"
}

node {
  policy = "read"
}

namespace "*" {
  capabilities = ["read-logs", "read-job"]
}
EOT
}

resource "vault_nomad_secret_role" "logs" {
  backend  = "nomad"
  role     = "service-logs"
  type     = "client"
  policies = [nomad_acl_policy.logs_reader.name]
}

resource "vault_policy" "nomad_logs" {
  name = "logs"

  policy = <<EOT
# Get Nomad creds
path "nomad/creds/${vault_nomad_secret_role.logs.role}" {
  capabilities = ["read"]
}
EOT
}

resource "vault_jwt_auth_backend_role" "nomad_logs" {
  backend   = "jwt-nomad"
  role_name = "nomtail"
  role_type = "jwt"

  bound_audiences = ["vault.io"]

  user_claim              = "/nomad_job_id"
  user_claim_json_pointer = true

  claim_mappings = {
    nomad_namespace = "nomad_namespace"
    nomad_job_id    = "nomad_job_id"
    nomad_group     = "nomad_group"
    nomad_task      = "nomad_task"
  }

  token_type     = "service"
  token_policies = [vault_policy.nomad_logs.name]

  token_period = 3600

  token_explicit_max_ttl = 0
}

resource "nomad_job" "logs" {
  jobspec = file("./logs.nomad")
}
