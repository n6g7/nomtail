# Nomtail

Nomtail streams Nomad task logs to Promtail/Loki.

```
Nomad agent <--(list local allocs, stream logs)-- Nomtail --(push to loki_push_api)--> Promtail --(send to loki)--> Loki
```

Nomtail connects to a Nomad client and streams the logs (stdout and stderr) of all of its local allocations to a [Loki push API](https://grafana.com/docs/loki/latest/reference/api/#push-log-entries-to-loki) endpoint. That endpoint can be either [Loki](https://grafana.com/docs/loki/latest/) or [Promtail](https://grafana.com/docs/loki/latest/send-data/promtail/). It's recommended sending logs to Promtail first to benefit from its pipelines.

Nomtail is meant to run alongside each Nomad client as **it only streams the logs from tasks running on the Nomad client it connects to**. Don't forget to run it in each of your nodes.

## Usage

Nomtail is packaged as a Docker image: `n6g7/nomtail`. Available in [Docker Hub](https://hub.docker.com/r/n6g7/nomtail) and [GHCR](https://github.com/n6g7/nomtail/pkgs/container/nomtail).

It is configured with environment variables:
- all of the [Nomad CLI environment variables](https://developer.hashicorp.com/nomad/docs/commands#environment-variables) are used to configure the Nomad client: `NOMAD_ADDR`, `NOMAD_REGION`, `NOMAD_TOKEN`, etc.
- `PROMTAIL_ADDR` is the Loki push API endpoint to send logs to. Can be either Promtail or Loki.

## Examples

- [Nomad + Vault + Terraform](./deployments/nomad/)
- [Docker Compose](./deployments/docker/)
