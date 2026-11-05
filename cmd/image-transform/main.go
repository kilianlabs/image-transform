package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kilianlabs/image-transform/internal/config"
	"github.com/kilianlabs/image-transform/internal/httpapi"
	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/native"
	"github.com/kilianlabs/image-transform/internal/service"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("Image Transform stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Read(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	processor, err := native.Start(imageproc.Limits{MaxPixels: cfg.MaxPixels, MaxAxis: cfg.MaxAxis, MaxFrames: cfg.MaxFrames, MaxOutputBytes: cfg.MaxOutputBytes}, cfg.VIPSThreads)
	if err != nil {
		return err
	}
	defer native.Shutdown()
	probeCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	err = processor.Probe(probeCtx, cfg.TempDir)
	cancel()
	if err != nil {
		return errors.New("native codec startup checks failed")
	}
	jobs := service.New(processor, cfg.TempDir, cfg.MaxInputBytes, service.AdmissionLimits{
		Concurrent:          cfg.MaxConcurrent,
		InspectConcurrent:   cfg.MaxInspectConcurrent,
		TransformConcurrent: cfg.MaxTransformConcurrent,
	}, cfg.RequestTimeout)
	var ready atomic.Bool
	ready.Store(true)
	app := httpapi.New(jobs, ready.Load)
	server := httpapi.Server(net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), app.Handler, cfg.RequestTimeout)
	errCh := make(chan error, 1)
	go func() {
		slog.Info("Image Transform listening", "address", server.Addr)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	ready.Store(false)
	jobs.Stop()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(drain)
	_ = server.Close()
	// Native calls cannot be forcibly interrupted safely. Wait before shutting
	// libvips down; the container stop timeout is the final process-level bound.
	jobs.Wait()
	slog.Info("Image Transform stopped")
	return err
}
func healthcheck() int {
	host := os.Getenv("HOST")
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health")
	if err != nil {
		return 1
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		return 1
	}
	return 0
}
