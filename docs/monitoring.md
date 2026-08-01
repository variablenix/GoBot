# Monitoring

GoBot exposes two HTTP endpoints on the configured stats listener:

- /stats: human-readable JSON runtime and persistence details
- /metrics: Prometheus-compatible metrics

Typical checks:

~~~sh
curl http://127.0.0.1:8082/stats
curl http://127.0.0.1:8082/metrics
~~~

Use /metrics for Prometheus. The endpoints do not have built-in
authentication, so bind them to localhost or restrict the port with a
firewall.

## Metrics

- bot_connected: 1 while at least one IRC connection is active, otherwise 0
- bot_reconnects: cumulative reconnect count
- bot_messages_received: cumulative IRC messages received
- bot_messages_sent: cumulative messages sent by GoBot
- bot_commands_handled: cumulative commands handled
- bot_uptime_seconds: current process uptime
- bot_messages_dropped: messages discarded because the outbound queue was full

Cumulative counters are persisted in BoltDB and survive restarts. Connection
status and process uptime naturally reset or change when the process restarts.
The !stats command reports persistent per-channel message and user statistics.

## Prometheus scrape configuration

If Prometheus runs on another host, set GoBot's listener to a WireGuard or
private LAN address reachable only by the monitoring host:

~~~env
BOT_STATS_LISTEN_ADDRESS=<BOT_VPS_PRIVATE_IP>:8082
~~~

Allow only the Prometheus host through the VPS firewall. For UFW, where
192.0.2.50 is the Prometheus host:

~~~sh
sudo ufw allow from 192.0.2.50 to any port 8082 proto tcp
sudo systemctl restart gobot
~~~

Example scrape job:

~~~yaml
scrape_configs:
  - job_name: gobot
    scrape_interval: 30s
    metrics_path: /metrics
    static_configs:
      - targets: ["<BOT_VPS_PRIVATE_IP>:8082"]
        labels:
          role: irc-bot
          use: gobot
~~~

Test from the Prometheus host:

~~~sh
curl http://<BOT_VPS_PRIVATE_IP>:8082/metrics
~~~

## Grafana

Import [grafana/gobot-dashboard.json](../grafana/gobot-dashboard.json) into
Grafana and select your Prometheus data source. The dashboard variables use
Prometheus queries for the job and instance labels. A sample export is
included at [grafana/gobot-dashboard-example.png](../grafana/gobot-dashboard-example.png).
