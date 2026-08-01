package bot

import (
	"context"
	"testing"

	"github.com/variablenix/GoBot/storage"
	"go.uber.org/zap"
)

type eventProbe struct {
	events  int
	handles int
}

func (p *eventProbe) Name() string                         { return "event-probe" }
func (p *eventProbe) Commands() []string                   { return nil }
func (p *eventProbe) Help() string                         { return "test event probe" }
func (p *eventProbe) Init(PluginConfig, *storage.DB) error { return nil }
func (p *eventProbe) Handle(*Bot, Message) bool {
	p.handles++
	return false
}
func (p *eventProbe) HandleEvent(*Bot, Message) bool {
	p.events++
	return true
}

func TestDispatchEventOnlyCallsEventHandlers(t *testing.T) {
	probe := &eventProbe{}
	b := New(Config{}, nil, []Plugin{probe}, zap.NewNop())
	b.dispatchEvent(Message{Command: "JOIN", Target: "#test", IsChannel: true, Nick: "Alice"})
	if probe.events != 1 {
		t.Fatalf("event handler calls = %d, want 1", probe.events)
	}
	if probe.handles != 0 {
		t.Fatalf("message handler calls = %d, want 0", probe.handles)
	}
	b.Queue.Drain(context.Background())
}
