package anizone

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type ProbeResult struct {
	Name        string `json:"name"`
	Status      int    `json:"status"`
	Proto       string `json:"proto"`
	Server      string `json:"server"`
	CfMitigated string `json:"cf_mitigated"`
	CfRay       string `json:"cf_ray"`
	BodyLen     int    `json:"body_len"`
	BodySnippet string `json:"body_snippet"`
	Error       string `json:"error,omitempty"`
}

func ProbeAll(ctx context.Context) []ProbeResult {
	target := "https://anizone.to/anime?search=Naruto"
	pyUA := "Mozilla/5.0 (compatible; ProphecyAniZoneServer/1.0)"
	chromeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	ffUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0"

	run := func(name, ua string, rt http.RoundTripper) ProbeResult {
		res := ProbeResult{Name: name}
		client := &http.Client{Timeout: 12 * time.Second, Transport: rt}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Referer", "https://anizone.to/")

		resp, err := client.Do(req)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		defer resp.Body.Close()

		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snip := string(b)
		if len(snip) > 200 {
			snip = snip[:200]
		}
		res.Status = resp.StatusCode
		res.Proto = resp.Proto
		res.Server = resp.Header.Get("Server")
		res.CfMitigated = resp.Header.Get("Cf-Mitigated")
		res.CfRay = resp.Header.Get("Cf-Ray")
		res.BodyLen = len(b)
		res.BodySnippet = snip
		return res
	}

	makeUTLSH1 := func(id utls.ClientHelloID) http.RoundTripper {
		return &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				host, _, _ := net.SplitHostPort(addr)
				u := utls.UClient(raw, &utls.Config{ServerName: host, NextProtos: []string{"http/1.1"}}, id)
				if err := u.HandshakeContext(ctx); err != nil {
					_ = raw.Close()
					return nil, err
				}
				return u, nil
			},
		}
	}

	makeUTLSH2 := func(id utls.ClientHelloID) http.RoundTripper {
		return &http2.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				host, _, _ := net.SplitHostPort(addr)
				u := utls.UClient(raw, &utls.Config{ServerName: host}, id)
				if err := u.HandshakeContext(ctx); err != nil {
					_ = raw.Close()
					return nil, err
				}
				if u.ConnectionState().NegotiatedProtocol != "h2" {
					_ = u.Close()
					return nil, fmt.Errorf("not h2: %s", u.ConnectionState().NegotiatedProtocol)
				}
				return u, nil
			},
		}
	}

	return []ProbeResult{
		run("stdlib_h2_pyUA", pyUA, &http.Transport{ForceAttemptHTTP2: true}),
		run("chrome_h2_chromeUA", chromeUA, makeUTLSH2(utls.HelloChrome_Auto)),
		run("chrome_h1_pyUA", pyUA, makeUTLSH1(utls.HelloChrome_Auto)),
		run("chrome_h1_chromeUA", chromeUA, makeUTLSH1(utls.HelloChrome_Auto)),
		run("firefox_h2_ffUA", ffUA, makeUTLSH2(utls.HelloFirefox_Auto)),
		run("firefox_h1_pyUA", pyUA, makeUTLSH1(utls.HelloFirefox_Auto)),
	}
}
