package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BioHazard786/Warpdrop/backend/internal/config"
	"github.com/BioHazard786/Warpdrop/backend/internal/logging"
	"github.com/BioHazard786/Warpdrop/backend/internal/server"
	"github.com/BioHazard786/Warpdrop/backend/web"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz on the local server and exit (for distroless images)")
	flag.Parse()
	cfg := config.Load()

	if *healthcheck {
		c := http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://127.0.0.1:" + cfg.Port + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}

	logging.Setup(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	s, h := server.New(cfg, web.FS())
	stop := make(chan struct{})
	go s.Janitor(stop)

	srv := &http.Server{
		Addr:              net.JoinHostPort("", cfg.Port),
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		close(stop)
		sd, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = srv.Shutdown(sd)
	}()

	slog.Info("listening", "addr", srv.Addr, "level", cfg.LogLevel, "format", cfg.LogFormat, "relay", cfg.RelayEnabled, "turn", cfg.TURNSecret != "")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}
