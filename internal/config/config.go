package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

func LoadDotEnv(path string) error {
	if err := godotenv.Load(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type Config struct {
	HTTPAddr, DBDriver, DBDSN, StorageDir, JWTSecret                       string
	JWTExpiry                                                              time.Duration
	MaxFileBytes, MaxBatchBytes                                            int64
	AdminUsername, AdminPassword                                           string
	SecretEncryptionKey, WorkflowLogDir, WorkflowWorkDir, WebhookAllowlist string
	WorkerConcurrency                                                      int
	WorkerLease, WorkerHeartbeat, FailedWorkspaceRetention                 time.Duration
	AIMaxContextChars, AIMaxToolRounds                                     int
	AIRequestTimeout                                                       time.Duration
	AIMaxResponseBytes                                                     int64
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func envInt64(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}
func Load() (Config, error) {
	maxFile, err := envInt64("MAX_FILE_BYTES", 1<<30)
	if err != nil {
		return Config{}, err
	}
	maxBatch, err := envInt64("MAX_BATCH_BYTES", 2<<30)
	if err != nil {
		return Config{}, err
	}
	expiry, err := time.ParseDuration(env("JWT_EXPIRY", "24h"))
	if err != nil || expiry <= 0 {
		return Config{}, fmt.Errorf("JWT_EXPIRY must be a positive duration")
	}
	c := Config{HTTPAddr: env("HTTP_ADDR", ":8080"), DBDriver: env("DB_DRIVER", "sqlite"), DBDSN: env("DB_DSN", "productserver.db"), StorageDir: env("STORAGE_DIR", "data/artifacts"), JWTSecret: env("JWT_SECRET", "change-me-in-production"), JWTExpiry: expiry, MaxFileBytes: maxFile, MaxBatchBytes: maxBatch, AdminUsername: env("ADMIN_USERNAME", "admin"), AdminPassword: env("ADMIN_PASSWORD", "admin123456")}
	c.SecretEncryptionKey = env("SECRET_ENCRYPTION_KEY", "")
	c.WorkflowLogDir = env("WORKFLOW_LOG_DIR", "data/logs")
	c.WorkflowWorkDir = env("WORKFLOW_WORK_DIR", "data/work")
	c.WebhookAllowlist = env("WEBHOOK_ALLOWLIST", "")
	concurrency, err := envInt64("WORKER_GLOBAL_CONCURRENCY", 4)
	if err != nil {
		return Config{}, err
	}
	c.WorkerConcurrency = int(concurrency)
	c.WorkerLease, err = time.ParseDuration(env("WORKER_LEASE_DURATION", "30s"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid WORKER_LEASE_DURATION")
	}
	c.WorkerHeartbeat, err = time.ParseDuration(env("WORKER_HEARTBEAT_INTERVAL", "10s"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid WORKER_HEARTBEAT_INTERVAL")
	}
	c.FailedWorkspaceRetention, err = time.ParseDuration(env("FAILED_WORKSPACE_RETENTION", "72h"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid FAILED_WORKSPACE_RETENTION")
	}
	aiContext, err := envInt64("AI_MAX_CONTEXT_CHARS", 200000)
	if err != nil {
		return Config{}, err
	}
	aiRounds, err := envInt64("AI_MAX_TOOL_ROUNDS", 12)
	if err != nil {
		return Config{}, err
	}
	aiBytes, err := envInt64("AI_MAX_RESPONSE_BYTES", 8<<20)
	if err != nil {
		return Config{}, err
	}
	c.AIMaxContextChars, c.AIMaxToolRounds, c.AIMaxResponseBytes = int(aiContext), int(aiRounds), aiBytes
	c.AIRequestTimeout, err = time.ParseDuration(env("AI_REQUEST_TIMEOUT", "120s"))
	if err != nil || c.AIRequestTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid AI_REQUEST_TIMEOUT")
	}
	if c.MaxBatchBytes < c.MaxFileBytes {
		return Config{}, fmt.Errorf("MAX_BATCH_BYTES must be >= MAX_FILE_BYTES")
	}
	return c, nil
}
