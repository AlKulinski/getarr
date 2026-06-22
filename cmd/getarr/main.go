package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aleksander/getarr/internal/config"
	"github.com/aleksander/getarr/internal/handlers"
	"github.com/aleksander/getarr/internal/store"
)

func main() {
	dataDir := flag.String("data", ".", "Directory for config.json")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	cfg, err := config.New(filepath.Join(*dataDir, "config.json"))
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	st := store.New()

	h, err := handlers.New(cfg, st)
	if err != nil {
		log.Fatalf("init handlers: %v", err)
	}

	addr := cfg.Get().ListenAddr
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("getarr listening on %s", addr)
	if err := http.ListenAndServe(addr, h.Routes()); err != nil {
		log.Fatalf("server: %v", err)
	}
}
