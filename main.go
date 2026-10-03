package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type runtimeConfig struct {
	Address      string
	PublicOrigin string
}

const maxRequestHeaderBytes = 32 * 1024

func configFromEnvironment() (runtimeConfig, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	origin := os.Getenv("PUBLIC_ORIGIN")
	if origin == "" {
		origin = "https://urlendar.stearz.net"
	}
	if strings.IndexFunc(origin, unicode.IsControl) >= 0 {
		return runtimeConfig{}, fmt.Errorf("PUBLIC_ORIGIN must not contain control characters")
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.ForceQuery {
		return runtimeConfig{}, fmt.Errorf("PUBLIC_ORIGIN must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	parsed.Path = ""
	return runtimeConfig{Address: ":" + port, PublicOrigin: parsed.String()}, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := configFromEnvironment()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	handler := requestLog(logger, newServer(appConfig{PublicOrigin: config.PublicOrigin, Now: time.Now}))
	server := &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxRequestHeaderBytes,
	}

	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		logger.Error("server failed to listen", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("urlendar listening", "address", config.Address, "origin", config.PublicOrigin)
	if err := serve(ctx, server, listener, 10*time.Second); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		serveErr := <-serveErrors
		if shutdownErr != nil {
			return shutdownErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}

type responseStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &responseStatusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}
