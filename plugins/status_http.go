package plugins

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/idna"
)

var (
	errStatusTargetBlocked = errors.New("non-public website target")
	errStatusRedirectLimit = errors.New("website redirect limit reached")
	// Shared across networks; user cooldowns also apply in the command router.
	statusCheckSlots = make(chan struct{}, 4)
)

func parseStatusTarget(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, errStatusTargetBlocked
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return nil, errStatusTargetBlocked
	}
	port := u.Port()
	if (port != "" && port != "80" && port != "443") || strings.HasSuffix(u.Host, ":") {
		return nil, errStatusTargetBlocked
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicInternetIP(ip) {
			return nil, errStatusTargetBlocked
		}
		if ip.Is6() {
			host = "[" + host + "]"
		}
	} else {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil || len(host) > 253 || !strings.Contains(host, ".") {
			return nil, errStatusTargetBlocked
		}
		for _, suffix := range []string{".localhost", ".local", ".internal", ".home.arpa"} {
			if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
				return nil, errStatusTargetBlocked
			}
		}
		labels := strings.Split(host, ".")
		for _, label := range labels {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' || strings.ContainsFunc(label, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
			}) {
				return nil, errStatusTargetBlocked
			}
		}
		// Reject legacy shortened/octal IPv4 spellings, not just dotted quads.
		if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
			return nil, errStatusTargetBlocked
		}
	}
	u.Host = host
	if port != "" {
		u.Host += ":" + port
	}
	u.Fragment, u.RawFragment = "", ""
	return u, nil
}

func newStatusHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Never let an environment proxy bypass IP validation.
	transport.DisableKeepAlives = true
	transport.DisableCompression = true
	transport.TLSHandshakeTimeout = 3 * time.Second
	transport.ResponseHeaderTimeout = 4 * time.Second
	transport.MaxResponseHeaderBytes = 64 << 10
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialStatusPublic(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 2 * time.Second}).DialContext)
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if _, err := parseStatusTarget(req.URL.String()); err != nil {
			return err
		}
		if len(via) >= 5 {
			return errStatusRedirectLimit
		}
		return nil
	}}
}

func dialStatusPublic(ctx context.Context, network, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") {
		return nil, errStatusTargetBlocked
	}
	var ips []netip.Addr
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{IsNotFound: true}
	}
	// Validate the entire answer before dialing even one address. Pin each
	// connection to a validated IP; never resolve the hostname a second time.
	for _, ip := range ips {
		if !publicInternetIP(ip) {
			return nil, errStatusTargetBlocked
		}
	}
	for _, ip := range ips {
		conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}

type statusSiteResult struct {
	code     int
	elapsed  time.Duration
	finalURL *url.URL
}

func checkStatusSite(ctx context.Context, client *http.Client, target *url.URL) (statusSiteResult, error) {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return statusSiteResult{}, err
	}
	req.Header.Set("User-Agent", "GoBot/1.0 (public website availability check)")
	res, err := client.Do(req)
	result := statusSiteResult{elapsed: time.Since(started), finalURL: target}
	if err != nil {
		return result, err
	}
	defer res.Body.Close() // Only response headers are needed, never a page body.
	result.code = res.StatusCode
	if res.Request != nil {
		result.finalURL = res.Request.URL
	}
	return result, nil
}

func statusTargetLabel(target *url.URL, maxBytes int) string {
	// Omit queries/fragments so tokens or other URL parameters are not echoed.
	return truncateUTF8Bytes(cleanExternalText(target.Scheme+"://"+target.Host+target.EscapedPath()), maxBytes)
}

func formatStatusSite(target *url.URL, result statusSiteResult, err error) string {
	label := "[Status] " + statusTargetLabel(target, 180)
	if err != nil {
		return label + " | " + ircColor(ircYellow, statusSiteError(err))
	}
	color := ircGreen
	if result.code >= 400 {
		color = ircYellow
	}
	if result.code >= 500 {
		color = ircRed
	}
	text := fmt.Sprintf("%s | reachable | %s | HTTP response %d ms", label, ircColor(color, fmt.Sprintf("HTTP %d %s", result.code, http.StatusText(result.code))), result.elapsed.Milliseconds())
	if result.finalURL != nil && result.finalURL.String() != target.String() {
		text += " | redirected to " + statusTargetLabel(result.finalURL, 100)
	}
	return text
}

func statusSiteError(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var netErr net.Error
	switch {
	case errors.Is(err, errStatusTargetBlocked):
		return "blocked non-public target or redirect"
	case errors.Is(err, errStatusRedirectLimit):
		return "too many redirects"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout():
		return "website check timed out"
	case errors.As(err, &dnsErr):
		return "DNS lookup failed"
	case errors.As(err, &certErr) || errors.As(err, &unknownCA) || errors.As(err, &hostnameErr):
		return "TLS certificate verification failed"
	default:
		return "HTTP connection failed"
	}
}
