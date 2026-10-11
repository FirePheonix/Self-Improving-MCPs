# observability

OpenTelemetry for the proxy hot path.

Exports `execute_tool` spans and call/latency/error metrics over OTLP when configured. Defaults to no-op so Cursor stays fast with zero collector setup.
