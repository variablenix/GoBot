# GoBot Grafana dashboard

[`gobot-dashboard.json`](gobot-dashboard.json) is the importable Grafana
dashboard for GoBot's `/metrics` endpoint. It covers:

- Prometheus scrape and IRC connection health
- process uptime and reliability events
- per-network incoming and outgoing message rates
- handled-command rates grouped by plugin
- outbound queue depth and capacity by network
- filtering by Prometheus job, environment, hostname, instance, and IRC network

The dashboard defaults match the production example below, but every filter
is selectable after import.

## 1. Deploy the expanded metrics

Pull the current GoBot `main` branch, rebuild the binary, and restart the
service. Confirm the endpoint from the Prometheus host:

~~~sh
curl http://10.69.0.22:8082/metrics
~~~

Along with the original `bot_*` metrics, the response should include
`bot_network_connected`, `bot_network_messages_received_total`,
`bot_plugin_commands_handled_total`, and `bot_outgoing_queue_depth`.

## 2. Configure Prometheus

Add this job under `scrape_configs` in `prometheus.yml`:

~~~yaml
  # GoBot IRC bot
  - job_name: gobot
    scrape_interval: 30s
    scrape_timeout: 10s
    metrics_path: /metrics
    static_configs:
      - targets: ["10.69.0.22:8082"]
        labels:
          role: irc-bot
          use: gobot
          hostname: nexus-node
          environment: production
~~~

Check the Prometheus configuration and reload it. For a package installation,
the usual commands are:

~~~sh
promtool check config /etc/prometheus/prometheus.yml
sudo systemctl reload prometheus
~~~

In Prometheus, run `up{job="gobot"}`. A value of `1` confirms that scraping is
working. If it is `0` or absent, check routing/firewall access from the
Prometheus host to `10.69.0.22:8082` before importing the dashboard.

## 3. Import into Grafana

1. Open **Dashboards → New → Import** in Grafana.
2. Upload `grafana/gobot-dashboard.json` from this repository, or paste the
   file contents.
3. Choose the Prometheus data source that contains the `gobot` job when
   Grafana asks for a data source.
4. Keep the dashboard name **GoBot Operations** and click **Import**.
5. At the top of the dashboard, verify these filters:
   - **Job:** `gobot`
   - **Environment:** `production`
   - **Host:** `nexus-node`
   - **Instance:** `10.69.0.22:8082`
   - **IRC network:** `All`, or one configured GoBot network

The dashboard refreshes every 30 seconds, matching the Prometheus scrape
interval. A newly started bot may need two scrapes (about one minute) before
rate panels have enough samples to draw a line.

## Security note

GoBot's `/stats` and `/metrics` endpoints do not provide authentication. Bind
the listener to a private address and allow access only from the Prometheus
host through the firewall.
