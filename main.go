package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/ramuthumu/gostash/internal/db"
	"github.com/ramuthumu/gostash/internal/server"
)

func main() {
	dataDir := os.Getenv("READLATER_DATA")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dataDir = filepath.Join(home, ".readlater")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
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

	srv := server.New(store)
	log.Printf("readlater listening on http://%s", addr)
	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatal(err)
	}
}
