package main

import (
	"log"
	"os"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	db, err := openDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := migrateAndBootstrap(db, cfg); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		log.Printf("workflow worker started")
		if err := runWorker(db, cfg); err != nil {
			log.Fatal(err)
		}
		return
	}
	app, err := newApp(db, cfg)
	if err != nil {
		log.Fatal(err)
	}
	go app.dispatchReleaseEvents()
	go app.dispatchSchedules()
	log.Printf("product server listening on %s", cfg.HTTPAddr)
	if err := app.router.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
