package bot

import "testing"

func TestShouldDelayCertFPJoinOnlyForUnconfirmedExternalAuth(t *testing.T) {
	server := ServerConfig{ClientCert: "/etc/gobot/oftc.pem"}

	tests := []struct {
		name       string
		clientCert bool
		mechanism  string
		state      *capNegotiation
		want       bool
	}{
		{name: "OFTC CertFP without SASL confirmation", clientCert: true, mechanism: "EXTERNAL", state: &capNegotiation{}, want: true},
		{name: "successful SASL EXTERNAL", clientCert: true, mechanism: "EXTERNAL", state: &capNegotiation{saslSucceeded: true}, want: false},
		{name: "password SASL", clientCert: true, mechanism: "PLAIN", state: &capNegotiation{}, want: false},
		{name: "no client certificate", mechanism: "EXTERNAL", state: &capNegotiation{}, want: false},
		{name: "missing negotiation state", clientCert: true, mechanism: "EXTERNAL", state: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := ServerConfig{}
			if test.clientCert {
				candidate = server
			}
			if got := shouldDelayCertFPJoin(candidate, test.mechanism, test.state); got != test.want {
				t.Fatalf("shouldDelayCertFPJoin() = %v, want %v", got, test.want)
			}
		})
	}
}
