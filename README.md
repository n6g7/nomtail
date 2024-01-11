# Nomtail

Nomtail streams Nomad task logs to Promtail/Loki.

```
Nomad agent <--(list local allocs, stream logs)-- Nomtail --(push to loki_push_api)--> Promtail --(send to loki)--> Loki
```

Nomtail connects to a Nomad client and streams the logs (stdout and stderr) of all of its local tasks to a [Loki push API](https://grafana.com/docs/loki/latest/reference/api/#push-log-entries-to-loki) endpoint. That endpoint can be either [Loki](https://grafana.com/docs/loki/latest/) or [Promtail](https://grafana.com/docs/loki/latest/send-data/promtail/). It's recommended sending logs to Promtail first to benefit from its pipelines.

Nomtail is meant to run alongside each Nomad client as **it only streams the logs from tasks running on the Nomad client it connects to**. Don't forget to run it in each of your nodes.

## Usage

Nomtail is packaged as a Docker image: `n6g7/nomtail`. It is configured exclusively with environment variables:

| Variable | Description |
| -------- | ----------- |
| NOMAD_ADDR | URL of the local Nomad client. |
| NOMAD_TOKEN | Nomad token to use to list allocations and stream logs. |
| PROMTAIL_ADDR | Loki push API endpoint to send logs to. Can be either Promtail of Loki. |

## Examples

With Nomad:
> TBD

With Docker Compose:
```yaml
version: "3"

services:
  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"
    command: -config.file=/etc/loki/local-config.yaml

  promtail:
    image: grafana/promtail:latest
    volumes:
      - ./promtail.yml:/etc/promtail/config.yml
    command: -config.file=/etc/promtail/config.yml

  nomtail:
    image: n6g7/nomtail:latest
    environment:
      NOMAD_ADDR: http://nomad-client:4646
      NOMAD_TOKEN: <token>
      PROMTAIL_ADDR: http://promtail:1234/loki/api/v1/push
```
