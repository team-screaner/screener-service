package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/team-screaner/screener-service/internal/adapter/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("migration command failed", "error", err)
		os.Exit(1)
	}
}
func run() (result error) {
	if len(os.Args) != 2 {
		return errors.New("usage: migrate up|down|status|seed")
	}
	command := os.Args[1]
	if command != "up" && command != "down" && command != "status" && command != "seed" {
		return errors.New("usage: migrate up|down|status|seed")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(10 * time.Minute)
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 5*time.Minute)
	defer cancel()
	ping, pingCancel := context.WithTimeout(ctx, 15*time.Second)
	err = db.PingContext(ping)
	pingCancel()
	if err != nil {
		return errors.New("database unavailable")
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	if dialectErr := goose.SetDialect("postgres"); dialectErr != nil {
		return dialectErr
	}
	switch command {
	case "up":
		err = goose.UpContext(ctx, db, dir)
	case "down":
		err = goose.DownContext(ctx, db, dir)
	case "status":
		err = goose.StatusContext(ctx, db, dir)
	case "seed":
		err = postgres.Seed(ctx, db)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", command, err)
	}
	slog.Info("migration command completed", "command", command)
	return nil
}
