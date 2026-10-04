package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/woovi-pix-flutter-sdk/examples/backend/internal/demo"
)

func main() {
	addr := os.Getenv("DEMO_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := demo.NewServer()
	var closeStore func()
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		store, err := demo.OpenPostgres(ctx, databaseURL)
		if err != nil {
			log.Fatal(err)
		}
		server = demo.NewServerWithStore(store, time.Now)
		closeStore = store.Close
		log.Print("using PostgreSQL checkout store")
	} else {
		log.Print("WARNING: DATABASE_URL unset; using ephemeral in-memory demo store")
	}
	if closeStore != nil {
		defer closeStore()
	}

	httpServer := &http.Server{
		Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("demo merchant backend (simulated PSP) listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
