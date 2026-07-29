package bot

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type Stats struct {
	started                                         time.Time
	received, sent, commands, reconnects, connected atomic.Uint64
}

func NewStats() *Stats { return &Stats{started: time.Now()} }
func (s *Stats) Snapshot() map[string]interface{} {
	return map[string]interface{}{"uptime": time.Since(s.started).Round(time.Second).String(), "connected": s.connected.Load() == 1, "reconnects": s.reconnects.Load(), "messages_received": s.received.Load(), "messages_sent": s.sent.Load(), "commands_handled": s.commands.Load()}
}
func (s *Stats) Serve(address string, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Snapshot())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for k, v := range s.Snapshot() {
			if k == "uptime" {
				continue
			}
			if k == "connected" {
				if v == true {
					v = 1
				} else {
					v = 0
				}
			}
			fmt.Fprintf(w, "bot_%s %v\n", k, v)
		}
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
	go server.ListenAndServe()
}

func statsListenAddress(address string, port int) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, strconv.Itoa(port))
}
