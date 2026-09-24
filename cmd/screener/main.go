package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	transport "github.com/team-screaner/screener-service/internal/adapter/http"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
func run() (result error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	responseKey, err := base64.StdEncoding.DecodeString(os.Getenv("RESPONSE_ENCRYPTION_KEY"))
	if err != nil || len(responseKey) != 32 {
		return errors.New("RESPONSE_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	maxOpen, err := connectionLimit("DB_MAX_OPEN_CONNS", 20)
	if err != nil {
		return err
	}
	maxIdle, err := connectionLimit("DB_MAX_IDLE_CONNS", 10)
	if err != nil {
		return err
	}
	if maxIdle > maxOpen {
		return errors.New("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	startup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if pingErr := db.PingContext(startup); pingErr != nil {
		cancel()
		return errors.New("database unavailable during startup")
	}
	err = checkSchema(startup, db)
	cancel()
	if err != nil {
		return err
	}
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	srv := &http.Server{Addr: address, Handler: transport.NewWithResponseKey(db, responseKey), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopped := make(chan error, 1)
	go func() { stopped <- srv.ListenAndServe() }()
	slog.Info("service started", "address", address)
	select {
	case serveErr := <-stopped:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", serveErr)
	case <-ctx.Done():
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdown)
	if err != nil {
		err = errors.Join(err, srv.Close())
	}
	serveErr := <-stopped
	if !errors.Is(serveErr, http.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	if err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	slog.Info("service stopped gracefully")
	return nil
}
func connectionLimit(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 1000 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 1000", key)
	}
	return value, nil
}
func checkSchema(ctx context.Context, db *sql.DB) error {
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	migrations, err := goose.CollectMigrations(dir, 0, goose.MaxVersion)
	if err != nil || len(migrations) == 0 {
		return errors.New("migration files unavailable")
	}
	var expected int64
	for _, migration := range migrations {
		if migration.Version > expected {
			expected = migration.Version
		}
	}
	var current int64
	// Read migration state without goose.EnsureDBVersion: application boot never creates schema.
	err = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_id),0) FROM (SELECT DISTINCT ON(version_id) version_id,is_applied FROM goose_db_version ORDER BY version_id,id DESC) versions WHERE is_applied`).Scan(&current)
	if err != nil {
		return errors.New("database schema unavailable; run migrate up")
	}
	if current != expected {
		return fmt.Errorf("database schema version %d does not match application version %d; run the deployment migration step", current, expected)
	}
	return nil
}
