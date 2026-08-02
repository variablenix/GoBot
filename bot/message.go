package bot

import (
	"gopkg.in/irc.v3"
	"strings"
	"time"
)

type Message struct {
	Raw, Nick, User, Host, Account, Command, Target, Text string
	IsChannel                                             bool
	Timestamp                                             time.Time
}

func ParseMessage(m *irc.Message) Message {
	target, text := "", ""
	if len(m.Params) > 0 {
		target = m.Params[0]
	}
	if len(m.Params) > 1 {
		text = strings.Join(m.Params[1:], " ")
	}
	account, _ := m.GetTag("account")
	timestamp := time.Now()
	if rawTimestamp, ok := m.GetTag("time"); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTimestamp); err == nil {
			timestamp = parsed
		}
	}
	return Message{Raw: m.String(), Nick: m.Name, User: m.User, Host: m.Host, Account: account, Command: m.Command, Target: target, Text: text, IsChannel: strings.HasPrefix(strings.ToLower(target), "#") || strings.HasPrefix(strings.ToLower(target), "&"), Timestamp: timestamp}
}
func (m Message) ReplyTarget() string {
	if m.IsChannel {
		return m.Target
	}
	return m.Nick
}
