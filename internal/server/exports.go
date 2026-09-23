package server

import (
	configpkg "productserver/internal/config"

	"gorm.io/gorm"
)

type Config = configpkg.Config

// LoadDotEnv loads development environment variables when the file exists.
func LoadDotEnv(path string) error { return configpkg.LoadDotEnv(path) }

// LoadConfig reads and validates the process configuration.
func LoadConfig() (Config, error) { return configpkg.Load() }

// OpenDatabase opens the configured database connection.
func OpenDatabase(cfg Config) (*gorm.DB, error) { return openDatabase(cfg) }

// MigrateAndBootstrap applies schema migrations and initializes required data.
func MigrateAndBootstrap(db *gorm.DB, cfg Config) error { return migrateAndBootstrap(db, cfg) }

// NewApp creates the HTTP application.
func NewApp(db *gorm.DB, cfg Config) (*App, error) { return newApp(db, cfg) }

// RunWorker starts the workflow worker process.
func RunWorker(db *gorm.DB, cfg Config) error { return runWorker(db, cfg) }

// StartBackgroundServices starts release-event and schedule dispatchers.
func (a *App) StartBackgroundServices() {
	go a.dispatchReleaseEvents()
	go a.dispatchSchedules()
}

// Run starts the HTTP server on addr.
func (a *App) Run(addr string) error { return a.router.Run(addr) }
