# GoBot Grafana dashboard

[`gobot-dashboard.json`](gobot-dashboard.json) is a portable Classic JSON
dashboard for GoBot's `/metrics` endpoint. Grafana maps its Prometheus data
source during import instead of retaining an instance-specific data-source
identifier. It covers:

- Prometheus scrape and IRC connection health
- process uptime and reliability events
- per-network incoming and outgoing message rates
- handled-command rates grouped by plugin
- the ten most-used plugins across the persisted GoBot command history
- current networks and channels GoBot has actually joined
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
`bot_plugin_commands_handled_total`, `bot_network_channel_joined`, and
`bot_outgoing_queue_depth`.

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

The JSON intentionally uses Grafana's portable Classic import model. It does
not contain API-managed resource fields such as `generation`,
`resourceVersion`, or `creationTimestamp`, so it can be imported through the
normal UI and used to overwrite an earlier copy safely.

The dashboard refreshes every 30 seconds, matching the Prometheus scrape
interval. A newly started bot may need two scrapes (about one minute) before
rate panels have enough samples to draw a line.

The **Most-used plugins** panel uses persistent per-network command totals from
BoltDB, so its ranking survives service restarts. A new database has no plugin
series until the first command is handled. The **Joined networks and channels**
panel reflects live JOIN/PART/KICK state and clears a network's channels when
its IRC connection ends.

## Security note

GoBot's `/stats` and `/metrics` endpoints do not provide authentication. Bind
the listener to a private address and allow access only from the Prometheus
host through the firewall. The membership metric includes joined channel
names as labels; it does not expose users, accounts, or message contents.
