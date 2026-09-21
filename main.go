package main

import (
	"log"
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
	app, err := newApp(db, cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("product server listening on %s", cfg.HTTPAddr)
	if err := app.router.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
