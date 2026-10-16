package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-ping/ping"
)

func tcpProber(target string) Prober {
	return func(ctx context.Context, to time.Duration) (time.Duration, error) {
		d := net.Dialer{Timeout: to}
		start := time.Now()
		conn, err := d.DialContext(ctx, "tcp", target)
		if err != nil {
			return 0, err
		}
		conn.Close()
		return time.Since(start), nil
	}
}

// httpProber probes target as a URL; 2xx-3xx = ok (redirects not followed).
func httpProber(target string) Prober {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return func(ctx context.Context, to time.Duration) (time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
		if err != nil {
			return 0, err
		}
		start := time.Now()
		resp, err := client.Do(req) // ctx carries the timeout
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return 0, fmt.Errorf("status %d", resp.StatusCode)
		}
		return time.Since(start), nil
	}
}

// dnsProber resolves each of targets; empty servers uses the system resolver,
// otherwise the first server over UDP :53.
func dnsProber(targets, servers []string) Prober {
	return func(ctx context.Context, to time.Duration) (time.Duration, error) {
		start := time.Now()
		for _, name := range targets {
			if len(servers) == 0 {
				if _, err := net.DefaultResolver.LookupHost(ctx, name); err != nil {
					return 0, err
				}
			} else {
				r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: to}).DialContext(ctx, "udp", servers[0]+":53")
				}}
				if _, err := r.LookupHost(ctx, name); err != nil {
					return 0, err
				}
			}
		}
		return time.Since(start), nil
	}
}

func icmpProber(target string) Prober {
	return func(ctx context.Context, to time.Duration) (time.Duration, error) {
		p, err := ping.NewPinger(target)
		if err != nil {
			return 0, err
		}
		p.Count = 1
		p.Timeout = to
		p.SetPrivileged(false) // unprivileged datagram socket
		// go-ping v1.2.0 lacks RunWithContext; bridge ctx via Stop.
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				p.Stop()
			case <-done:
			}
		}()
		if err := p.Run(); err != nil {
			return 0, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if st := p.Statistics(); st.PacketsRecv == 0 {
			return 0, errors.New("no reply")
		} else {
			return st.AvgRtt, nil
		}
	}
}
