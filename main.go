package main

import (
	"log"
	"os"

	"productserver/internal/server"
)

func main() {
	if err := server.LoadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}
	cfg, err := server.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	db, err := server.OpenDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := server.MigrateAndBootstrap(db, cfg); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		log.Printf("workflow worker started")
		if err := server.RunWorker(db, cfg); err != nil {
			log.Fatal(err)
		}
		return
	}
	app, err := server.NewApp(db, cfg)
	if err != nil {
		log.Fatal(err)
	}
	app.StartBackgroundServices()
	log.Printf("product server listening on %s", cfg.HTTPAddr)
	if err := app.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
