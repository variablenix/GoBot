package bot

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/variablenix/GoBot/storage"
)

type Stats struct {
	started                                         time.Time
	received, sent, commands, reconnects, connected atomic.Uint64
	db                                              *storage.DB
	persistDone                                     chan struct{}
	persistOnce                                     atomic.Bool
}

type persistedStats struct {
	Received   uint64 `json:"messages_received"`
	Sent       uint64 `json:"messages_sent"`
	Commands   uint64 `json:"commands_handled"`
	Reconnects uint64 `json:"reconnects"`
}

func NewStats(dbs ...*storage.DB) *Stats {
	s := &Stats{started: time.Now()}
	if len(dbs) > 0 && dbs[0] != nil {
		s.db = dbs[0]
		if raw, err := s.db.Get("stats", "global"); err == nil {
			var saved persistedStats
			if storage.Decode(raw, &saved) == nil {
				s.received.Store(saved.Received)
				s.sent.Store(saved.Sent)
				s.commands.Store(saved.Commands)
				s.reconnects.Store(saved.Reconnects)
			}
		}
		s.persistDone = make(chan struct{})
		go s.persistLoop()
	}
	return s
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
		Received: s.received.Load(), Sent: s.sent.Load(), Commands: s.commands.Load(), Reconnects: s.reconnects.Load(),
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
