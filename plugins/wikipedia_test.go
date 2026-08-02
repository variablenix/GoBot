package plugins

import (
	"context"
	"testing"
)

func TestWikipediaRequestSetsDescriptiveUserAgent(t *testing.T) {
	req, err := wikipediaRequest(context.Background(), "https://en.wikipedia.org/api/rest_v1/page/summary/Linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("User-Agent"); got == "" || got == "Go-http-client/1.1" {
		t.Fatalf("unexpected User-Agent %q", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("got Accept %q", got)
	}
}

func TestWikipediaSearchTermFocusesConversationalQuestions(t *testing.T) {
	tests := map[string]string{
		"what is Linux exactly? Do you know?":      "linux",
		"What is Ubuntu?":                          "ubuntu",
		"is UFO disclosure happening more now?":    "ufo disclosure",
		"How does TLS protect web traffic?":        "tls protect web traffic",
		"tell me about the history of video games": "history video games",
	}
	for input, want := range tests {
		if got := wikipediaSearchTerm(input); got != want {
			t.Errorf("wikipediaSearchTerm(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWikipediaTitleMatchesRejectsIncidentalResults(t *testing.T) {
	tests := []struct {
		topic string
		title string
		want  bool
	}{
		{topic: "Linux", title: "Linux", want: true},
		{topic: "Ubuntu", title: "Ubuntu", want: true},
		{topic: "UFO disclosure", title: "Disclosure Day (soundtrack)", want: false},
		{topic: "TLS protect", title: "Transport Layer Security", want: false},
		{topic: "TLS protect", title: "TLS", want: true},
	}
	for _, test := range tests {
		if got := wikipediaTitleMatches(test.topic, test.title); got != test.want {
			t.Errorf("wikipediaTitleMatches(%q, %q) = %v, want %v", test.topic, test.title, got, test.want)
		}
	}
}
