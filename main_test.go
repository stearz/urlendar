package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("PUBLIC_ORIGIN", "https://calendar.example")

	config, err := configFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":9090" {
		t.Fatalf("address = %q", config.Address)
	}
	if config.PublicOrigin != "https://calendar.example" {
		t.Fatalf("origin = %q", config.PublicOrigin)
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("PUBLIC_ORIGIN", "")

	config, err := configFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":8080" {
		t.Fatalf("address = %q", config.Address)
	}
	if config.PublicOrigin != "https://urlendar.stearz.net" {
		t.Fatalf("origin = %q", config.PublicOrigin)
	}
}

func TestConfigRejectsUnsafePublicOrigin(t *testing.T) {
	for _, origin := range []string{
		"calendar.example",
		"ftp://calendar.example",
		"https://user:pass@calendar.example",
		"https://calendar.example/path",
		"https://calendar.example?query=yes",
		"https://calendar.example#fragment",
		"https://calendar.example\r\nX-Injected: yes",
		"	https://calendar.example",
		"https://calendar.example	",
		"https://:443",
		"https://calendar.example:bad",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("PUBLIC_ORIGIN", origin)
			if _, err := configFromEnvironment(); err == nil {
				t.Fatalf("expected %q to be rejected", origin)
			}
		})
	}
}

func TestServeWaitsForActiveRequestsDuringShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, listener, time.Second) }()

	responseDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			defer response.Body.Close()
			_, requestErr = io.ReadAll(response.Body)
		}
		responseDone <- requestErr
	}()
	<-started
	cancel()

	select {
	case err := <-done:
		t.Fatalf("serve returned before the active request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-responseDone; err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve failed: %v", err)
	}
}

func TestRequestHeaderLimitCoversMaximumQuery(t *testing.T) {
	if maxRequestHeaderBytes <= maxRawQueryBytes {
		t.Fatalf("MaxHeaderBytes = %d, query limit = %d", maxRequestHeaderBytes, maxRawQueryBytes)
	}
}
