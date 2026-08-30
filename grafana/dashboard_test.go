package grafana

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type dashboardModel struct {
	SchemaVersion int    `json:"schemaVersion"`
	Title         string `json:"title"`
	UID           string `json:"uid"`
	Inputs        []struct {
		Name     string `json:"name"`
		PluginID string `json:"pluginId"`
	} `json:"__inputs"`
	Panels []struct {
		Title string `json:"title"`
	} `json:"panels"`
	Templating struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	} `json:"templating"`
}

func TestDashboardJSONUsesExpandedMetrics(t *testing.T) {
	raw, err := os.ReadFile("gobot-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var dashboard dashboardModel
	if err := json.Unmarshal(raw, &dashboard); err != nil {
		t.Fatalf("dashboard JSON is invalid: %v", err)
	}
	if dashboard.SchemaVersion < 39 || dashboard.Title != "GoBot Operations" || dashboard.UID != "gobot-operations" {
		t.Fatalf("unexpected dashboard identity: %#v", dashboard)
	}
	if len(dashboard.Inputs) != 1 || dashboard.Inputs[0].Name != "DS_PROMETHEUS" || dashboard.Inputs[0].PluginID != "prometheus" {
		t.Fatalf("dashboard Prometheus input is not portable: %#v", dashboard.Inputs)
	}

	panelTitles := make(map[string]bool)
	for _, panel := range dashboard.Panels {
		panelTitles[panel.Title] = true
	}
	for _, title := range []string{
		"IRC connection",
		"Prometheus scrape",
		"Reliability events",
		"Message throughput by network",
		"Command throughput by plugin",
		"Outbound queue pressure",
		"Uptime",
		"Most-used plugins",
		"Joined networks and channels",
	} {
		if !panelTitles[title] {
			t.Errorf("dashboard is missing panel %q", title)
		}
	}

	variables := make(map[string]bool)
	for _, variable := range dashboard.Templating.List {
		variables[variable.Name] = true
	}
	for _, name := range []string{"job", "environment", "hostname", "instance", "network"} {
		if !variables[name] {
			t.Errorf("dashboard is missing variable %q", name)
		}
	}

	text := string(raw)
	for _, metric := range []string{
		"bot_network_connected",
		"bot_network_reconnects_total",
		"bot_network_messages_received_total",
		"bot_network_messages_sent_total",
		"bot_network_messages_dropped_total",
		"bot_plugin_commands_handled_total",
		"bot_plugin_panics_total",
		"bot_outgoing_queue_depth",
		"bot_outgoing_queue_capacity",
		"bot_network_channel_joined",
	} {
		if !strings.Contains(text, metric) {
			t.Errorf("dashboard does not query %s", metric)
		}
	}
}
