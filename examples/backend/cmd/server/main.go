package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	var store demo.CheckoutStore = demo.NewMemoryStore()
	var closeStore func()
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		postgresStore, err := demo.OpenPostgres(ctx, databaseURL)
		if err != nil {
			log.Fatal(err)
		}
		store = postgresStore
		closeStore = postgresStore.Close
		log.Print("using PostgreSQL checkout store")
	} else {
		log.Print("WARNING: DATABASE_URL unset; using ephemeral in-memory demo store")
	}
	if closeStore != nil {
		defer closeStore()
	}
	var webhookVerifier demo.WebhookVerifier
	webhookEnabled := os.Getenv("ENABLE_WOOVI_WEBHOOK") == "true"
	demoPayEnabled := os.Getenv("ENABLE_DEMO_PSP") == "true"
	if webhookEnabled && demoPayEnabled {
		log.Fatal("ENABLE_DEMO_PSP cannot be enabled together with ENABLE_WOOVI_WEBHOOK")
	}
	if webhookEnabled {
		webhookVerifier = demo.NewWooviSignatureVerifier(nil, os.Getenv("WOOVI_WEBHOOK_PUBLIC_KEYS_URL"))
		log.Print("Woovi charge webhook receiver enabled; order creation remains simulator-only")
	}
	var reconciliationWorker *demo.ChargeReconciliationWorker
	if os.Getenv("ENABLE_WOOVI_RECONCILIATION") == "true" {
		postgresStore, ok := store.(*demo.PostgresStore)
		if !ok {
			log.Fatal("ENABLE_WOOVI_RECONCILIATION requires DATABASE_URL and PostgreSQL")
		}
		appID := os.Getenv("WOOVI_APP_ID")
		if appID == "" {
			log.Fatal("ENABLE_WOOVI_RECONCILIATION requires server-side WOOVI_APP_ID")
		}
		client, err := demo.NewWooviChargeClient(appID, os.Getenv("WOOVI_API_BASE_URL"), nil)
		if err != nil {
			log.Fatal("invalid Woovi reconciliation client configuration")
		}
		interval := 30 * time.Second
		if raw := os.Getenv("WOOVI_RECONCILIATION_INTERVAL"); raw != "" {
			interval, err = time.ParseDuration(raw)
			if err != nil || interval < time.Second || interval > time.Hour {
				log.Fatal("WOOVI_RECONCILIATION_INTERVAL must be between 1s and 1h")
			}
		}
		batchSize := 25
		if raw := os.Getenv("WOOVI_RECONCILIATION_BATCH_SIZE"); raw != "" {
			batchSize, err = strconv.Atoi(raw)
			if err != nil || batchSize < 1 || batchSize > 500 {
				log.Fatal("WOOVI_RECONCILIATION_BATCH_SIZE must be between 1 and 500")
			}
		}
		retryPolicy := demo.DefaultReconciliationRetryPolicy()
		if raw := os.Getenv("WOOVI_RECONCILIATION_RETRY_BASE"); raw != "" {
			retryPolicy.BaseDelay, err = time.ParseDuration(raw)
			if err != nil {
				log.Fatal("invalid WOOVI_RECONCILIATION_RETRY_BASE")
			}
		}
		if raw := os.Getenv("WOOVI_RECONCILIATION_RETRY_MAX"); raw != "" {
			retryPolicy.MaxDelay, err = time.ParseDuration(raw)
			if err != nil {
				log.Fatal("invalid WOOVI_RECONCILIATION_RETRY_MAX")
			}
		}
		if raw := os.Getenv("WOOVI_RECONCILIATION_RETRY_JITTER"); raw != "" {
			retryPolicy.JitterFraction, err = strconv.ParseFloat(raw, 64)
			if err != nil {
				log.Fatal("invalid WOOVI_RECONCILIATION_RETRY_JITTER")
			}
		}
		reconciliationWorker, err = demo.NewChargeReconciliationWorker(postgresStore, client, interval, batchSize, retryPolicy, time.Now, func(cycle demo.ReconciliationCycle) {
			log.Printf("woovi_reconciliation claimed=%d resolved=%d failed=%d queue_error=%t", cycle.Claimed, cycle.Resolved, cycle.Failed, cycle.Err != nil)
		})
		if err != nil {
			log.Fatal("invalid Woovi reconciliation worker configuration")
		}
		log.Printf("Woovi charge reconciliation enabled (GET-only; interval=%s batch=%d)", interval, batchSize)
	}
	server := demo.NewServerWithOptions(store, time.Now, webhookVerifier, demoPayEnabled)
	if demoPayEnabled {
		log.Print("WARNING: unauthenticated demo payment route enabled; never expose outside local development")
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
	var workerDone chan struct{}
	if reconciliationWorker != nil {
		workerDone = make(chan struct{})
		go func() {
			defer close(workerDone)
			if err := reconciliationWorker.Run(ctx); err != nil {
				log.Printf("Woovi reconciliation worker stopped: %T", err)
			}
		}()
	}
	log.Printf("demo merchant backend (simulated PSP) listening on %s", addr)
	listenErr := httpServer.ListenAndServe()
	stop()
	if workerDone != nil {
		<-workerDone
	}
	if listenErr != nil && listenErr != http.ErrServerClosed {
		log.Fatal(listenErr)
	}
}
