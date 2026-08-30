package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

func TestStatsListenAddress(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{address: "127.0.0.1", want: "127.0.0.1:8080"},
		{address: "127.0.0.1:9090", want: "127.0.0.1:9090"},
		{address: "0.0.0.0", want: "0.0.0.0:8080"},
	}
	for _, tt := range tests {
		if got := statsListenAddress(tt.address, 8080); got != tt.want {
			t.Errorf("address %q: got %q, want %q", tt.address, got, tt.want)
		}
	}
}

func TestMetricsSnapshotIncludesUptimeAndDroppedMessages(t *testing.T) {
	stats := NewStats()
	stats.dropped.Store(3)

	snapshot := stats.MetricsSnapshot()
	if _, ok := snapshot["uptime_seconds"]; !ok {
		t.Fatal("metrics snapshot does not include uptime_seconds")
	}
	if got := snapshot["messages_dropped"]; got != uint64(3) {
		t.Fatalf("messages_dropped: got %v, want 3", got)
	}
	if _, ok := snapshot["uptime"]; ok {
		t.Fatal("metrics snapshot should not include human-readable uptime")
	}
}

func TestExpandedPrometheusMetricsRemainBackwardCompatible(t *testing.T) {
	stats := NewStats()
	queue := NewQueue(0.01, 2, func(Outgoing) {})
	defer queue.Drain(context.Background())
	network := stats.registerNetwork("libera", 3, queue)
	stats.setNetworkConnected(network, true)
	stats.received.Store(12)
	stats.sent.Store(8)
	network.received.Store(7)
	network.sent.Store(5)
	network.reconnects.Store(2)
	stats.recordCommand(network, "help")
	stats.recordPluginPanic(network, "weather", "message")
	if !queue.Enqueue(Outgoing{Target: "#test", Text: "queued"}) {
		t.Fatal("failed to enqueue test message")
	}

	metrics := stats.PrometheusSnapshot()
	for _, want := range []string{
		"bot_connected 1\n",
		"bot_messages_received 12\n",
		"bot_messages_sent 8\n",
		"bot_commands_handled 1\n",
		"bot_networks_configured 1\n",
		"bot_networks_connected 1\n",
		`bot_network_connected{network="libera"} 1`,
		`bot_network_reconnects_total{network="libera"} 2`,
		`bot_network_messages_received_total{network="libera"} 7`,
		`bot_network_messages_sent_total{network="libera"} 5`,
		`bot_network_configured_channels{network="libera"} 3`,
		`bot_outgoing_queue_depth{network="libera"} 1`,
		`bot_outgoing_queue_capacity{network="libera"} 40`,
		`bot_plugin_commands_handled_total{network="libera",plugin="help"} 1`,
		`bot_plugin_panics_total{network="libera",plugin="weather",handler="message"} 1`,
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("PrometheusSnapshot() missing %q\n%s", want, metrics)
		}
	}
	networks, ok := stats.Snapshot()["networks"].(map[string]interface{})
	if !ok {
		t.Fatal("Snapshot() networks has an unexpected type")
	}
	libera, ok := networks["libera"].(map[string]interface{})
	if !ok || libera["configured_channels"] != 3 || libera["queue_depth"] != 1 {
		t.Fatalf("Snapshot() network details = %#v", networks["libera"])
	}
}

type panicMetricsPlugin struct{}

func (*panicMetricsPlugin) Name() string                         { return "panic-test" }
func (*panicMetricsPlugin) Commands() []string                   { return []string{"panic-test"} }
func (*panicMetricsPlugin) Help() string                         { return "panic-test" }
func (*panicMetricsPlugin) Init(PluginConfig, *storage.DB) error { return nil }
func (*panicMetricsPlugin) Handle(*Bot, Message) bool            { panic("test panic") }

type commandMetricsPlugin struct{}

func (*commandMetricsPlugin) Name() string                         { return "choose" }
func (*commandMetricsPlugin) Commands() []string                   { return []string{"choose"} }
func (*commandMetricsPlugin) Help() string                         { return "choose" }
func (*commandMetricsPlugin) Init(PluginConfig, *storage.DB) error { return nil }
func (*commandMetricsPlugin) Handle(*Bot, Message) bool            { return true }

func TestPluginPanicsAreRecoveredAndCounted(t *testing.T) {
	plugin := &panicMetricsPlugin{}
	config := Config{NetworkName: "test", CommandPrefix: "!"}
	stats := NewStats()
	instance := NewWithStats(config, nil, []Plugin{plugin}, zap.NewNop(), stats)
	defer instance.Queue.Drain(context.Background())

	instance.dispatch(Message{Command: "PRIVMSG", Target: "#test", Text: "!panic-test", IsChannel: true})
	metrics := stats.PrometheusSnapshot()
	if !strings.Contains(metrics, `bot_plugin_panics_total{network="test",plugin="panic-test",handler="message"} 1`) {
		t.Fatalf("plugin panic metric missing:\n%s", metrics)
	}
}

func TestConnectionGaugeAggregatesMultipleNetworks(t *testing.T) {
	stats := NewStats()
	first := stats.registerNetwork("first", 1, nil)
	second := stats.registerNetwork("second", 1, nil)

	stats.setNetworkConnected(first, true)
	stats.setNetworkConnected(second, true)
	stats.setNetworkConnected(first, false)
	if stats.connected.Load() != 1 || stats.connectedNetworks.Load() != 1 {
		t.Fatalf("one connected network reported global=%d active=%d", stats.connected.Load(), stats.connectedNetworks.Load())
	}
	stats.setNetworkConnected(second, false)
	if stats.connected.Load() != 0 || stats.connectedNetworks.Load() != 0 {
		t.Fatalf("zero connected networks reported global=%d active=%d", stats.connected.Load(), stats.connectedNetworks.Load())
	}
}

func TestPrometheusLabelsAreEscaped(t *testing.T) {
	stats := NewStats()
	stats.registerNetwork("bad\\\"\nname", 0, nil).reconnects.Store(1)
	metrics := stats.PrometheusSnapshot()
	if !strings.Contains(metrics, `network="bad\\\"\nname"`) {
		t.Fatalf("network label was not escaped safely:\n%s", metrics)
	}
}

func TestFirstPluginHandledCommandIsCounted(t *testing.T) {
	plugin := &commandMetricsPlugin{}
	config := Config{NetworkName: "test", CommandPrefix: "!"}
	stats := NewStats()
	instance := NewWithStats(config, nil, []Plugin{plugin}, zap.NewNop(), stats)
	defer instance.Queue.Drain(context.Background())

	instance.dispatch(Message{Command: "PRIVMSG", Target: "#test", Text: "!choose a | b", IsChannel: true})
	if got := stats.commands.Load(); got != 1 {
		t.Fatalf("commands handled = %d, want 1", got)
	}
	metrics := stats.PrometheusSnapshot()
	if !strings.Contains(metrics, `bot_plugin_commands_handled_total{network="test",plugin="choose"} 1`) {
		t.Fatalf("plugin command metric missing:\n%s", metrics)
	}
}
