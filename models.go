package main

import "time"

type User struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"uniqueIndex;size:100;not null"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}
type Project struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"-" gorm:"uniqueIndex:idx_project_owner_name;not null"`
	Name      string    `json:"name" gorm:"uniqueIndex:idx_project_owner_name;size:150;not null"`
	CreatedAt time.Time `json:"created_at"`
}
type ProjectToken struct {
	ID         uint       `json:"id" gorm:"primaryKey"`
	ProjectID  uint       `json:"-" gorm:"uniqueIndex:uniq_project_token_project;not null"`
	Name       string     `json:"name" gorm:"size:150;not null"`
	Prefix     string     `json:"prefix" gorm:"size:16;not null"`
	TokenHash  string     `json:"-" gorm:"uniqueIndex;size:64;not null"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
type Release struct {
	ID         uint           `json:"id" gorm:"primaryKey"`
	ProjectID  uint           `json:"project_id" gorm:"uniqueIndex:idx_release_project_version;not null"`
	Version    string         `json:"version" gorm:"uniqueIndex:idx_release_project_version;size:255;not null"`
	CommitSHA  string         `json:"commit_sha"`
	Branch     string         `json:"branch"`
	JobURL     string         `json:"job_url"`
	PipelineID string         `json:"pipeline_id"`
	RefType    string         `json:"ref_type" gorm:"size:16"`
	CreatedAt  time.Time      `json:"created_at"`
	Files      []ArtifactFile `json:"files,omitempty" gorm:"constraint:OnDelete:CASCADE"`
}

type SSHCredential struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	UserID              uint      `json:"-" gorm:"index;not null"`
	Name                string    `json:"name"`
	PrivateKeyEncrypted string    `json:"-" gorm:"type:text"`
	PassphraseEncrypted string    `json:"-" gorm:"type:text"`
	PublicKey           string    `json:"public_key" gorm:"type:text"`
	Fingerprint         string    `json:"fingerprint"`
	CreatedAt           time.Time `json:"created_at"`
}
type SSHConnection struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	UserID         uint      `json:"-" gorm:"index;not null"`
	CredentialID   uint      `json:"credential_id"`
	Name           string    `json:"name"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Username       string    `json:"username"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type Workflow struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"index;not null"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	TriggerType string    `json:"trigger_type"`
	TriggerGlob string    `json:"trigger_glob"`
	Definition  string    `json:"-" gorm:"type:text"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type ReleaseEvent struct {
	ID          uint `gorm:"primaryKey"`
	ReleaseID   uint `gorm:"uniqueIndex"`
	ProcessedAt *time.Time
	CreatedAt   time.Time
}
type WorkflowRun struct {
	ID              uint              `json:"id" gorm:"primaryKey"`
	ProjectID       uint              `json:"project_id" gorm:"index"`
	WorkflowID      uint              `json:"workflow_id" gorm:"index"`
	ReleaseID       uint              `json:"release_id"`
	Status          string            `json:"status" gorm:"index"`
	Snapshot        string            `json:"-" gorm:"type:text"`
	WorkerID        string            `json:"worker_id"`
	LeaseUntil      *time.Time        `json:"lease_until"`
	CancelRequested bool              `json:"cancel_requested"`
	LogPath         string            `json:"-"`
	LogBytes        int64             `json:"log_bytes"`
	ErrorSummary    string            `json:"error_summary"`
	StartedAt       *time.Time        `json:"started_at"`
	FinishedAt      *time.Time        `json:"finished_at"`
	CreatedAt       time.Time         `json:"created_at"`
	Nodes           []WorkflowNodeRun `json:"nodes,omitempty" gorm:"foreignKey:RunID"`
}
type WorkflowNodeRun struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	RunID        uint       `json:"run_id" gorm:"index"`
	NodeKey      string     `json:"node_key"`
	Module       string     `json:"module"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	ErrorSummary string     `json:"error_summary"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}
type WorkflowLock struct {
	ID         uint `gorm:"primaryKey"`
	ProjectID  uint `gorm:"uniqueIndex:uniq_workflow_lock"`
	WorkflowID uint `gorm:"uniqueIndex:uniq_workflow_lock"`
	RunID      uint
	LeaseUntil time.Time
}
type ExecutionSlot struct {
	ID         uint `gorm:"primaryKey"`
	WorkerID   string
	NodeRunID  uint
	LeaseUntil *time.Time
}
type ArtifactFile struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ReleaseID    uint      `json:"-" gorm:"index;not null"`
	OriginalName string    `json:"name"`
	StoredName   string    `json:"-"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256" gorm:"size:64"`
	MIMEType     string    `json:"mime_type"`
	CreatedAt    time.Time `json:"created_at"`
}
