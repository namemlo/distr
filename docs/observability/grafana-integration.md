# Grafana Integration

Grafana integration is static in the current observability suite. Distr builds links and publishes dashboard JSON templates, but it does not call Grafana APIs or provision dashboards.

## Configuration

Set the base URL used for generated links:

```text
OBSERVABILITY_GRAFANA_BASE_URL=https://grafana.example.com
```

The value should be the user-facing Grafana origin, optionally including a path prefix:

```text
OBSERVABILITY_GRAFANA_BASE_URL=https://observability.example.com/grafana
```

If the value is empty or invalid, the link builders return empty link fields. The dashboard catalog still returns the static dashboard definitions when `observability_dashboards` is enabled.

## Dashboard Templates

Use `GET /api/v1/observability/dashboards` to retrieve dashboard templates. Import the `template` JSON into Grafana through your normal Grafana administration workflow.

Current dashboard IDs:

- `http-overview`
- `task-execution-overview`
- `service-health-overview`

## Link Construction

Trace links use Grafana Explore and the Tempo datasource name expected by the correlation builder.

Metrics links use Grafana Explore and the Prometheus datasource name expected by the correlation builder.

Dashboard links use `/d/{dashboardID}` with time range and `var-*` dashboard variables.

Example dashboard link:

```text
https://grafana.example.com/d/task-execution-overview?from=now-1h&to=now&var-environment=prod&var-service=hub
```

## Operational Notes

Use stable, low-cardinality labels such as `service`, `environment`, and `version`. Avoid secret values and arbitrary variable values in labels.

Keep Grafana datasource names aligned with the assumptions in the correlation builders. If a deployment uses different datasource names, add that customization in a future code slice rather than editing documentation to imply runtime support that does not exist.

## Boundaries

This guide does not add:

- Grafana provisioning,
- dashboard synchronization,
- datasource management,
- alert rule creation,
- live validation of links.
