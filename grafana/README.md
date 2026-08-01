# GoBot Grafana dashboard

`gobot-dashboard.json` is an importable Grafana dashboard for the metrics exposed by GoBot at `/metrics`.

## Import

1. In Grafana, open **Dashboards → New → Import**.
2. Upload `gobot-dashboard.json` or paste its contents.
3. Select the Prometheus datasource that scrapes GoBot.
4. Click **Import**.

The dashboard expects the `gobot` Prometheus job and uses these metrics:

- `bot_connected`
- `bot_reconnects`
- `bot_messages_received`
- `bot_messages_sent`
- `bot_commands_handled`
- `bot_uptime_seconds`
- `bot_messages_dropped`

The Prometheus job and dashboard are intentionally kept separate: the Ansible monitoring role deploys the scrape configuration, while this repository carries the dashboard definition alongside the application.
