package anizone

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

var errNotH2 = errors.New("server did not negotiate h2")

// firefoxRoundTripper uses uTLS (Firefox TLS fingerprint) with HTTP/2 and
// falls back to HTTP/1.1 when the server does not negotiate h2 or when an
// HTTP proxy is configured.
type chromeRoundTripper struct {
	h2        *http2.Transport
	h1        *http.Transport
	proxyFunc func(*http.Request) (*url.URL, error)
}

func newChromeRoundTripper(proxyFunc func(*http.Request) (*url.URL, error)) http.RoundTripper {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	dialUTLS := func(ctx context.Context, network, addr string) (*utls.UConn, error) {
		rawConn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}

		cfg := &utls.Config{
			ServerName: host,
		}

		uConn := utls.UClient(rawConn, cfg, utls.HelloFirefox_Auto)
		if err := uConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		return uConn, nil
	}

	h2 := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			uConn, err := dialUTLS(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if uConn.ConnectionState().NegotiatedProtocol != "h2" {
				_ = uConn.Close()
				return nil, errNotH2
			}
			return uConn, nil
		},
	}

	h1 := &http.Transport{
		Proxy: proxyFunc,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialUTLS(ctx, network, addr)
		},
	}

	return &chromeRoundTripper{
		h2:        h2,
		h1:        h1,
		proxyFunc: proxyFunc,
	}
}

func (rt *chromeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL != nil && strings.EqualFold(req.URL.Scheme, "https") {
		useProxy := false
		if rt.proxyFunc != nil {
			if proxyURL, err := rt.proxyFunc(req); err == nil && proxyURL != nil {
				useProxy = true
			}
		}

		if !useProxy {
			resp, err := rt.h2.RoundTrip(req)
			if err == nil || !errors.Is(err, errNotH2) {
				return resp, err
			}
		}
	}

	return rt.h1.RoundTrip(req)
}

func (rt *chromeRoundTripper) CloseIdleConnections() {
	rt.h2.CloseIdleConnections()
	rt.h1.CloseIdleConnections()
}
