package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/woovi-pix-flutter-sdk/examples/backend/internal/demo"
)

func main() {
	addr := os.Getenv("DEMO_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := demo.NewServer()
	log.Printf("demo merchant backend (simulated PSP) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
