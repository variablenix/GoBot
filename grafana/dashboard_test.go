package grafana

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type dashboardResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Title     string                      `json:"title"`
		Elements  map[string]dashboardElement `json:"elements"`
		Variables []struct {
			Spec struct {
				Name string `json:"name"`
			} `json:"spec"`
		} `json:"variables"`
	} `json:"spec"`
}

type dashboardElement struct {
	Spec struct {
		Title string `json:"title"`
	} `json:"spec"`
}

func TestDashboardJSONUsesExpandedMetrics(t *testing.T) {
	raw, err := os.ReadFile("gobot-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var dashboard dashboardResource
	if err := json.Unmarshal(raw, &dashboard); err != nil {
		t.Fatalf("dashboard JSON is invalid: %v", err)
	}
	if dashboard.APIVersion == "" || dashboard.Kind != "Dashboard" || dashboard.Spec.Title != "GoBot Operations" {
		t.Fatalf("unexpected dashboard identity: %#v", dashboard)
	}

	panelTitles := make(map[string]bool)
	for _, element := range dashboard.Spec.Elements {
		panelTitles[element.Spec.Title] = true
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
	for _, variable := range dashboard.Spec.Variables {
		variables[variable.Spec.Name] = true
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
