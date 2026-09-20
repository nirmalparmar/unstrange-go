package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/unstrange/backend/internal/community"
	"github.com/unstrange/backend/internal/config"
	"github.com/unstrange/backend/internal/db"
	"github.com/unstrange/backend/internal/media"
	"github.com/unstrange/backend/internal/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config.Load()
	db.Connect()
	defer db.Close()
	db.Migrate()
	db.MigrateCommunity()
	db.ConnectRuntime()
	media.Init()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go community.PurgeExpiredMedia(ctx)

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(config.C.BindAddress, config.C.Port),
		Handler:           server.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			log.Printf("graceful shutdown: %v", err)
			_ = httpServer.Close()
		}
	}()

	log.Printf("server listening on %s", httpServer.Addr)
	err := httpServer.ListenAndServe()
	stop()
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
