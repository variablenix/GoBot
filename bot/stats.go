package bot

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/variablenix/GoBot/storage"
)

type Stats struct {
	started                                                  time.Time
	received, sent, commands, reconnects, dropped, connected atomic.Uint64
	connectedNetworks                                        atomic.Int64
	db                                                       *storage.DB
	persistDone                                              chan struct{}
	persistOnce                                              atomic.Bool
	networkMu                                                sync.RWMutex
	networks                                                 map[string]*networkStats
}

type networkStats struct {
	name               string
	configuredChannels int
	queue              *Queue
	channelMu          sync.RWMutex
	joinedChannels     map[string]string
	connected          atomic.Uint64
	received           atomic.Uint64
	sent               atomic.Uint64
	commands           atomic.Uint64
	reconnects         atomic.Uint64
	dropped            atomic.Uint64
	pluginCommands     sync.Map
	pluginPanics       sync.Map
}

type persistedStats struct {
	Received   uint64 `json:"messages_received"`
	Sent       uint64 `json:"messages_sent"`
	Commands   uint64 `json:"commands_handled"`
	Reconnects uint64 `json:"reconnects"`
	Dropped    uint64 `json:"messages_dropped"`
}

type metricLabel struct {
	name  string
	value string
}

type prometheusWriter struct {
	output    strings.Builder
	described map[string]struct{}
}

func NewStats(dbs ...*storage.DB) *Stats {
	s := &Stats{started: time.Now(), networks: make(map[string]*networkStats)}
	if len(dbs) > 0 && dbs[0] != nil {
		s.db = dbs[0]
		if raw, err := s.db.Get("stats", "global"); err == nil {
			var saved persistedStats
			if storage.Decode(raw, &saved) == nil {
				s.received.Store(saved.Received)
				s.sent.Store(saved.Sent)
				s.commands.Store(saved.Commands)
				s.reconnects.Store(saved.Reconnects)
				s.dropped.Store(saved.Dropped)
			}
		}
		s.persistDone = make(chan struct{})
		go s.persistLoop()
	}
	return s
}

func (s *Stats) registerNetwork(name string, configuredChannels int, queue *Queue) *networkStats {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}
	if configuredChannels < 0 {
		configuredChannels = 0
	}
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	if existing := s.networks[name]; existing != nil {
		return existing
	}
	network := &networkStats{name: name, configuredChannels: configuredChannels, queue: queue, joinedChannels: make(map[string]string)}
	s.networks[name] = network
	return network
}

func (network *networkStats) joinChannel(channel string) {
	if network == nil {
		return
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return
	}
	network.channelMu.Lock()
	network.joinedChannels[strings.ToLower(channel)] = channel
	network.channelMu.Unlock()
}

func (network *networkStats) leaveChannel(channel string) {
	if network == nil {
		return
	}
	network.channelMu.Lock()
	delete(network.joinedChannels, strings.ToLower(strings.TrimSpace(channel)))
	network.channelMu.Unlock()
}

func (network *networkStats) clearJoinedChannels() {
	if network == nil {
		return
	}
	network.channelMu.Lock()
	clear(network.joinedChannels)
	network.channelMu.Unlock()
}

func (network *networkStats) sortedJoinedChannels() []string {
	if network == nil {
		return nil
	}
	network.channelMu.RLock()
	channels := make([]string, 0, len(network.joinedChannels))
	for _, channel := range network.joinedChannels {
		channels = append(channels, channel)
	}
	network.channelMu.RUnlock()
	sort.Slice(channels, func(i, j int) bool { return strings.ToLower(channels[i]) < strings.ToLower(channels[j]) })
	return channels
}

func (s *Stats) setNetworkConnected(network *networkStats, value bool) {
	if network == nil {
		if value {
			s.connected.Store(1)
		} else {
			s.connected.Store(0)
		}
		return
	}
	if !value {
		network.clearJoinedChannels()
	}
	desired := uint64(0)
	delta := int64(-1)
	if value {
		desired = 1
		delta = 1
	}
	if network.connected.Swap(desired) == desired {
		return
	}
	active := s.connectedNetworks.Add(delta)
	if active > 0 {
		s.connected.Store(1)
		return
	}
	if active < 0 {
		s.connectedNetworks.Store(0)
	}
	s.connected.Store(0)
}

func (s *Stats) recordCommand(network *networkStats, plugin string) {
	s.commands.Add(1)
	if network == nil {
		return
	}
	network.commands.Add(1)
	atomicCounter(&network.pluginCommands, plugin).Add(1)
}

func (s *Stats) recordPluginPanic(network *networkStats, plugin, handler string) {
	if network == nil {
		return
	}
	atomicCounter(&network.pluginPanics, plugin+"\x00"+handler).Add(1)
}

func atomicCounter(counters *sync.Map, key string) *atomic.Uint64 {
	value, _ := counters.LoadOrStore(key, &atomic.Uint64{})
	return value.(*atomic.Uint64)
}

func (s *Stats) persistLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Persist()
		case <-s.persistDone:
			return
		}
	}
}

func (s *Stats) Persist() {
	if s.db == nil {
		return
	}
	_ = s.db.Set("stats", "global", persistedStats{
		Received: s.received.Load(), Sent: s.sent.Load(), Commands: s.commands.Load(), Reconnects: s.reconnects.Load(), Dropped: s.dropped.Load(),
	})
}

func (s *Stats) Close() {
	if s.db == nil || !s.persistOnce.CompareAndSwap(false, true) {
		return
	}
	close(s.persistDone)
	s.Persist()
}

func (s *Stats) Snapshot() map[string]interface{} {
	return map[string]interface{}{
		"uptime":            time.Since(s.started).Round(time.Second).String(),
		"connected":         s.connected.Load() == 1,
		"reconnects":        s.reconnects.Load(),
		"messages_received": s.received.Load(),
		"messages_sent":     s.sent.Load(),
		"messages_dropped":  s.dropped.Load(),
		"commands_handled":  s.commands.Load(),
		"networks":          s.networkSnapshot(),
	}
}

func (s *Stats) MetricsSnapshot() map[string]interface{} {
	snapshot := s.Snapshot()
	delete(snapshot, "uptime")
	delete(snapshot, "networks")
	snapshot["uptime_seconds"] = time.Since(s.started).Seconds()
	return snapshot
}

func (s *Stats) networkSnapshot() map[string]interface{} {
	networks := make(map[string]interface{})
	for _, network := range s.sortedNetworks() {
		depth, capacity := 0, 0
		joinedChannels := network.sortedJoinedChannels()
		if network.queue != nil {
			depth = network.queue.Depth()
			capacity = network.queue.Capacity()
		}
		networks[network.name] = map[string]interface{}{
			"connected":            network.connected.Load() == 1,
			"reconnects":           network.reconnects.Load(),
			"messages_received":    network.received.Load(),
			"messages_sent":        network.sent.Load(),
			"messages_dropped":     network.dropped.Load(),
			"commands_handled":     network.commands.Load(),
			"configured_channels":  network.configuredChannels,
			"joined_channel_count": len(joinedChannels),
			"joined_channels":      joinedChannels,
			"queue_depth":          depth,
			"queue_capacity":       capacity,
		}
	}
	return networks
}

func (s *Stats) sortedNetworks() []*networkStats {
	s.networkMu.RLock()
	networks := make([]*networkStats, 0, len(s.networks))
	for _, network := range s.networks {
		networks = append(networks, network)
	}
	s.networkMu.RUnlock()
	sort.Slice(networks, func(i, j int) bool { return networks[i].name < networks[j].name })
	return networks
}

func (s *Stats) PrometheusSnapshot() string {
	writer := prometheusWriter{described: make(map[string]struct{})}
	writer.metric("bot_connected", "Whether at least one IRC network is connected.", "gauge", nil, s.connected.Load())
	writer.metric("bot_reconnects", "Persistent cumulative IRC reconnect count.", "untyped", nil, s.reconnects.Load())
	writer.metric("bot_messages_received", "Persistent cumulative IRC messages received.", "untyped", nil, s.received.Load())
	writer.metric("bot_messages_sent", "Persistent cumulative IRC messages sent.", "untyped", nil, s.sent.Load())
	writer.metric("bot_commands_handled", "Persistent cumulative commands handled.", "untyped", nil, s.commands.Load())
	writer.metric("bot_uptime_seconds", "Current GoBot process uptime in seconds.", "gauge", nil, time.Since(s.started).Seconds())
	writer.metric("bot_messages_dropped", "Persistent cumulative messages dropped because an outbound queue was full.", "untyped", nil, s.dropped.Load())
	writer.metric("bot_process_start_time_seconds", "Unix timestamp when the current GoBot process started.", "gauge", nil, float64(s.started.Unix()))
	networks := s.sortedNetworks()
	writer.metric("bot_networks_configured", "Number of configured IRC networks.", "gauge", nil, len(networks))
	writer.metric("bot_networks_connected", "Number of currently connected IRC networks.", "gauge", nil, s.connectedNetworks.Load())

	for _, network := range networks {
		labels := []metricLabel{{name: "network", value: network.name}}
		writer.metric("bot_network_connected", "Whether the IRC network is connected.", "gauge", labels, network.connected.Load())
		writer.metric("bot_network_reconnects_total", "IRC reconnects during the current process lifetime.", "counter", labels, network.reconnects.Load())
		writer.metric("bot_network_messages_received_total", "IRC messages received during the current process lifetime.", "counter", labels, network.received.Load())
		writer.metric("bot_network_messages_sent_total", "IRC messages sent during the current process lifetime.", "counter", labels, network.sent.Load())
		writer.metric("bot_network_commands_handled_total", "Commands handled during the current process lifetime.", "counter", labels, network.commands.Load())
		writer.metric("bot_network_messages_dropped_total", "Messages dropped because the network outbound queue was full.", "counter", labels, network.dropped.Load())
		writer.metric("bot_network_configured_channels", "Configured IRC channels for the network.", "gauge", labels, network.configuredChannels)
		joinedChannels := network.sortedJoinedChannels()
		writer.metric("bot_network_joined_channels", "IRC channels the bot is currently joined to on the network.", "gauge", labels, len(joinedChannels))
		for _, channel := range joinedChannels {
			channelLabels := append(append([]metricLabel{}, labels...), metricLabel{name: "channel", value: channel})
			writer.metric("bot_network_channel_joined", "Current IRC channel membership for the bot.", "gauge", channelLabels, 1)
		}
		depth, capacity := 0, 0
		if network.queue != nil {
			depth = network.queue.Depth()
			capacity = network.queue.Capacity()
		}
		writer.metric("bot_outgoing_queue_depth", "Messages currently waiting in the network outbound queue.", "gauge", labels, depth)
		writer.metric("bot_outgoing_queue_capacity", "Maximum messages supported by the network outbound queue.", "gauge", labels, capacity)
		writer.syncMapCounters("bot_plugin_commands_handled_total", "Commands handled by each plugin during the current process lifetime.", network.name, &network.pluginCommands, false)
		writer.syncMapCounters("bot_plugin_panics_total", "Recovered plugin panics during the current process lifetime.", network.name, &network.pluginPanics, true)
	}
	return writer.output.String()
}

func (w *prometheusWriter) syncMapCounters(name, help, network string, counters *sync.Map, includeHandler bool) {
	values := make(map[string]uint64)
	keys := make([]string, 0)
	counters.Range(func(key, value interface{}) bool {
		text, ok := key.(string)
		counter, counterOK := value.(*atomic.Uint64)
		if ok && counterOK {
			keys = append(keys, text)
			values[text] = counter.Load()
		}
		return true
	})
	sort.Strings(keys)
	for _, key := range keys {
		plugin, handler := key, ""
		if includeHandler {
			plugin, handler, _ = strings.Cut(key, "\x00")
		}
		labels := []metricLabel{{name: "network", value: network}, {name: "plugin", value: plugin}}
		if includeHandler {
			labels = append(labels, metricLabel{name: "handler", value: handler})
		}
		w.metric(name, help, "counter", labels, values[key])
	}
}

func (w *prometheusWriter) metric(name, help, metricType string, labels []metricLabel, value interface{}) {
	if _, ok := w.described[name]; !ok {
		fmt.Fprintf(&w.output, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
		w.described[name] = struct{}{}
	}
	w.output.WriteString(name)
	if len(labels) > 0 {
		w.output.WriteByte('{')
		for i, label := range labels {
			if i > 0 {
				w.output.WriteByte(',')
			}
			fmt.Fprintf(&w.output, `%s="%s"`, label.name, escapePrometheusLabel(label.value))
		}
		w.output.WriteByte('}')
	}
	fmt.Fprintf(&w.output, " %v\n", value)
}

func escapePrometheusLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func (s *Stats) Serve(address string, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(s.PrometheusSnapshot()))
	})
	if address == "" {
		address = "127.0.0.1"
	}
	server := &http.Server{
		Addr:              statsListenAddress(address, port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = server.ListenAndServe() }()
}

func statsListenAddress(address string, port int) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, strconv.Itoa(port))
}
