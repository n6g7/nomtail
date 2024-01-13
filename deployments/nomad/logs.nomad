job "logs" {
  type = "system"

  group "log-shipping" {
    network {
      mode = "bridge"
      port "promtail_http" {}
      port "promtail_grpc" {}
      port "promtail_lokipush_http" {}
      port "promtail_lokipush_grpc" {}
    }

    service {
      name = "promtail"
      port = "promtail_http"
      check {
        type     = "http"
        path     = "/ready"
        interval = "10s"
        timeout  = "2s"
      }

      connect {
        sidecar_service {
          proxy {
            upstreams {
              destination_name = "loki"
              local_bind_port  = 8081
            }
          }
        }
      }
    }

    task "promtail" {
      driver = "docker"

      config {
        image = "grafana/promtail:latest"
        args = [
          "-config.file",
          "local/config.yaml",
        ]
        ports = ["promtail_http", "promtail_grpc", "promtail_lokipush_http", "promtail_lokipush_grpc"]
      }

      template {
        destination = "local/config.yaml"
        data = <<EOH
server:
  http_listen_port: {{ env "NOMAD_PORT_promtail_http" }}
  grpc_listen_port: {{ env "NOMAD_PORT_promtail_grpc" }}

positions:
  filename: {{ env "NOMAD_ALLOC_DIR" }}/positions.yaml

clients:
  - url: http://{{ env "NOMAD_UPSTREAM_ADDR_loki" }}/loki/api/v1/push

scrape_configs:
  - job_name: nomtail
    loki_push_api:
      server:
        http_listen_port: {{ env "NOMAD_PORT_promtail_lokipush_http" }}
        grpc_listen_port: {{ env "NOMAD_PORT_promtail_lokipush_grpc" }}
      labels:
        job: nomtail
        host: "{{env "node.unique.name"}}"
      use_incoming_timestamp: true
EOH
      }
    }

    task "nomtail" {
      vault {
        role = "nomtail"
      }

      driver = "docker"

      config {
        image = "n6g7/nomtail:latest"
      }

      template {
        destination = "secrets/nomtail.env"
        env = true

        data = <<EOH
NOMAD_ADDR=https://{{ env "node.unique.name" }}.local:4646
{{ with secret "nomad/creds/nomtail" -}}
NOMAD_TOKEN={{ .Data.secret_id }}
{{- end }}
PROMTAIL_ADDR=http://localhost:{{ env "NOMAD_PORT_promtail_lokipush_http" }}/loki/api/v1/push
EOH
      }
    }
  }
}
