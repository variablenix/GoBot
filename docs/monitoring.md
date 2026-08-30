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

The original global metrics remain available for backward compatibility:

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

GoBot also exposes operational metrics for richer dashboards:

| Metric | Type | Meaning |
| --- | --- | --- |
| `bot_process_start_time_seconds` | gauge | Current process start time as a Unix timestamp |
| `bot_networks_configured` | gauge | Number of configured IRC networks |
| `bot_networks_connected` | gauge | Number of currently connected IRC networks |
| `bot_network_connected{network}` | gauge | Per-network connection state |
| `bot_network_reconnects_total{network}` | counter | Per-network reconnects since process start |
| `bot_network_messages_received_total{network}` | counter | Per-network messages received since process start |
| `bot_network_messages_sent_total{network}` | counter | Per-network messages sent since process start |
| `bot_network_commands_handled_total{network}` | counter | Per-network handled commands since process start |
| `bot_network_messages_dropped_total{network}` | counter | Per-network outbound messages dropped since process start |
| `bot_network_configured_channels{network}` | gauge | Configured channels per network |
| `bot_outgoing_queue_depth{network}` | gauge | Messages currently waiting to be sent |
| `bot_outgoing_queue_capacity{network}` | gauge | Maximum outbound queue size |
| `bot_plugin_commands_handled_total{network,plugin}` | counter | Handled commands grouped by plugin |
| `bot_plugin_panics_total{network,plugin,handler}` | counter | Recovered message/event handler panics |

Per-network and per-plugin counters reset when the GoBot process restarts;
Prometheus `rate()` and `increase()` account for counter resets. Labels are
limited to configured network names, built-in plugin names, and the bounded
handler type. Channel names, nicknames, accounts, and message contents are not
exported.

The `/stats` JSON response includes a `networks` object with connection,
traffic, command, reconnect, queue, and configured-channel details for each
network.

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
Prometheus queries for job, environment, hostname, instance, and IRC network
labels. Follow the complete step-by-step instructions in
[grafana/README.md](../grafana/README.md).
