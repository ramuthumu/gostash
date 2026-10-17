package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ramuthumu/gostash/internal/db"
	"github.com/ramuthumu/gostash/internal/server"
)

// version is stamped at build time via -ldflags="-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := os.Getenv("READLATER_DATA")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dataDir = filepath.Join(home, ".readlater")
	}
	// 0700: the DB holds the user's reading history.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "readlater.db")

	store, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	addr := os.Getenv("READLATER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090" // loopback only by default; set READLATER_ADDR=":8090" to expose on all interfaces
	}

	mediaDir := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		log.Fatal(err)
	}

	auth, err := server.AuthConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	srv := server.New(store, mediaDir, auth)
	log.Printf("gostash %s listening on http://%s", version, addr)
	if err := srv.ListenAndServe(ctx, addr); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
