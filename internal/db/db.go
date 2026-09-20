package db

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/unstrange/backend/internal/config"
)

var Pool *pgxpool.Pool

// Connect opens the direct endpoint for startup migrations.
func Connect() {
	Pool = connectPool(config.C.DatabaseURL)
}

// ConnectRuntime switches to the pooled endpoint after migrations, before serving requests.
// Local setups with only DATABASE_URL keep using their existing connection pool.
func ConnectRuntime() {
	if config.C.DatabasePooledURL == "" {
		return
	}
	pool := connectPool(config.C.DatabasePooledURL)
	previous := Pool
	Pool = pool
	if previous != nil {
		previous.Close()
	}
}

func connectPool(databaseURL string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatal("invalid database connection configuration")
	}

	// Force IPv4 — prevents "no route to host" on networks without IPv6
	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, "tcp4", addr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("unable to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("unable to ping database: %v", err)
	}

	log.Println("connected to database")
	return pool
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
