package bot

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestQueueOrdering(t *testing.T) {
	var mu sync.Mutex
	var got []string
	q := NewQueue(100, 2, func(o Outgoing) { mu.Lock(); got = append(got, o.Text); mu.Unlock() })
	defer q.Drain(context.Background())
	q.Enqueue(Outgoing{"#x", "one"})
	q.Enqueue(Outgoing{"#x", "two"})
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %v", got)
	}
}
