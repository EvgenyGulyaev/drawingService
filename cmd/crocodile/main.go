// Crocodile runs just the game API locally without drawing-service/Drive credentials.
package main

import (
	"flag"
	"log"
	"net"
	"time"

	"drawingService/internal/game"
	"github.com/go-www/silverlining"
	bolt "go.etcd.io/bbolt"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8091", "API listen address (Vite proxies requests from devices)")
	dbPath := flag.String("db", "crocodile.db", "game database path")
	flag.Parse()
	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	h, err := game.New(db)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for now := range time.NewTicker(time.Minute).C {
			if err := h.Cleanup(now); err != nil {
				log.Printf("game cleanup: %v", err)
			}
		}
	}()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("crocodile API: http://%s (database %s)", *addr, *dbPath)
	srv := &silverlining.Server{Handler: h.Serve, MaxBodySize: 1024 * 1024}
	if err := srv.Serve(ln); err != nil {
		log.Fatal(err)
	}
}
