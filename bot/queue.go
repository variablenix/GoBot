package bot

import (
	"context"
	"math"
	"sync"
	"time"
)

type Outgoing struct{ Target, Text string }
type Queue struct {
	ch       chan Outgoing
	interval time.Duration
	burst    int
	send     func(Outgoing)
	done     chan struct{}
	once     sync.Once
}

func NewQueue(rate float64, burst int, send func(Outgoing)) *Queue {
	if rate <= 0 || math.IsNaN(rate) {
		rate = 1
	}
	if math.IsInf(rate, 1) || rate > 1000 {
		rate = 1000
	}
	if burst < 1 {
		burst = 1
	}
	interval := time.Duration(float64(time.Second) / rate)
	if interval < time.Nanosecond {
		interval = time.Nanosecond
	}
	q := &Queue{ch: make(chan Outgoing, burst*20), interval: interval, burst: burst, send: send, done: make(chan struct{})}
	go q.loop()
	return q
}
func (q *Queue) Enqueue(o Outgoing) bool {
	select {
	case q.ch <- o:
		return true
	default:
		return false
	}
}
func (q *Queue) loop() {
	ticker := time.NewTicker(q.interval)
	defer ticker.Stop()
	tokens := q.burst
	for {
		select {
		case <-q.done:
			return
		case <-ticker.C:
			if tokens < q.burst {
				tokens++
			}
			if tokens > 0 {
				select {
				case o := <-q.ch:
					q.send(o)
					tokens--
				default:
				}
			}
		}
	}
}
func (q *Queue) Drain(ctx context.Context) {
	q.once.Do(func() { close(q.done) })
	for {
		select {
		case o := <-q.ch:
			q.send(o)
		case <-ctx.Done():
			return
		default:
			return
		}
	}
}
