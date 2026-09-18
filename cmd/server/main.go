package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brabeem/tictactoe/internal/game"
	"github.com/brabeem/tictactoe/internal/store"
	"github.com/brabeem/tictactoe/internal/ws"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := flag.String("addr", ":8080", "listen address")
	queueCapacity := flag.Int("queue-capacity", 1024, "maximum players waiting for a match")
	moveTimeout := flag.Duration("move-timeout", 15*time.Second, "time allowed per move before the player is dropped")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := store.New(store.Deps{
		Hub:     store.NewHub(),
		Queue:   store.NewWaitQueue(*queueCapacity),
		Matches: store.NewMatchIndex(),
		Timers:  store.NewMatchTimers(store.RealClock{}),
		NewMatch: func(id, x, o string) store.Match {
			return game.NewMatch(id, x, o, game.NewGrid())
		},
		NewID:       rand.Text,
		MoveTimeout: *moveTimeout,
		Logger:      logger,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /ws", ws.NewHandler(s, rand.Text, ws.DefaultConfig(), logger))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 2)
	go func() { errc <- s.Run(ctx) }()
	go func() {
		logger.Info("listening", "addr", *addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			return err
		}
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
