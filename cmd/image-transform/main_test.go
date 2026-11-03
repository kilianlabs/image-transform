package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHealthcheckUsesConfiguredAddress(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			var status atomic.Int32
			status.Store(http.StatusOK)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(int(status.Load()))
			}))
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			defer server.Close()
			_, port, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOST", host)
			t.Setenv("PORT", port)
			if code := healthcheck(); code != 0 {
				t.Fatalf("healthy server returned exit code %d", code)
			}
			status.Store(http.StatusServiceUnavailable)
			if code := healthcheck(); code != 1 {
				t.Fatalf("unavailable server returned exit code %d", code)
			}
		})
	}
}
