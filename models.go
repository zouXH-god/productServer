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
	CreatedAt  time.Time      `json:"created_at"`
	Files      []ArtifactFile `json:"files,omitempty" gorm:"constraint:OnDelete:CASCADE"`
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
