package domain

import "time"

type User struct {
	ID                 uint      `json:"id" gorm:"primaryKey"`
	Username           string    `json:"username" gorm:"uniqueIndex;size:100;not null"`
	Email              *string   `json:"email,omitempty" gorm:"uniqueIndex;size:255"`
	PasswordHash       string    `json:"-"`
	IsAdmin            bool      `json:"is_admin" gorm:"index"`
	Enabled            bool      `json:"enabled" gorm:"index;not null;default:true"`
	MustChangePassword bool      `json:"must_change_password"`
	TokenVersion       int       `json:"-" gorm:"not null;default:0"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
type Project struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	UserID           uint      `json:"-" gorm:"uniqueIndex:idx_project_owner_name;not null"`
	Name             string    `json:"name" gorm:"uniqueIndex:idx_project_owner_name;size:150;not null"`
	Type             string    `json:"type" gorm:"size:16;not null;default:artifact;index"`
	MaxArtifactBytes int64     `json:"max_artifact_bytes" gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
}
type ProjectMember struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"uniqueIndex:uniq_project_member;not null"`
	UserID    uint      `json:"user_id" gorm:"uniqueIndex:uniq_project_member;index;not null"`
	Role      string    `json:"role" gorm:"size:16;not null;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      User      `json:"user,omitempty" gorm:"foreignKey:UserID"`
}
type SystemSetting struct {
	Key       string    `json:"key" gorm:"primaryKey;size:100"`
	Value     string    `json:"value" gorm:"type:text"`
	UpdatedAt time.Time `json:"updated_at"`
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
	ID          uint           `json:"id" gorm:"primaryKey"`
	ProjectID   uint           `json:"project_id" gorm:"uniqueIndex:idx_release_project_version;not null"`
	Version     string         `json:"version" gorm:"uniqueIndex:idx_release_project_version;size:255;not null"`
	CommitSHA   string         `json:"commit_sha"`
	Branch      string         `json:"branch"`
	JobURL      string         `json:"job_url"`
	PipelineID  string         `json:"pipeline_id"`
	RefType     string         `json:"ref_type" gorm:"size:16"`
	CreatedAt   time.Time      `json:"created_at"`
	Files       []ArtifactFile `json:"files,omitempty" gorm:"constraint:OnDelete:CASCADE"`
	AccessCount int64          `json:"access_count" gorm:"->;-:migration"`
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
	ID                uint      `json:"id" gorm:"primaryKey"`
	UserID            uint      `json:"-" gorm:"index;not null"`
	CredentialID      uint      `json:"credential_id"`
	AuthType          string    `json:"auth_type" gorm:"size:16;not null;default:key"`
	PasswordEncrypted string    `json:"-" gorm:"type:text"`
	Name              string    `json:"name"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Username          string    `json:"username"`
	TimeoutSeconds    int       `json:"timeout_seconds"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
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
type WorkflowRevision struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"index;not null"`
	WorkflowID  uint      `json:"workflow_id" gorm:"index;not null"`
	UserID      uint      `json:"user_id" gorm:"index;not null"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	TriggerType string    `json:"trigger_type"`
	TriggerGlob string    `json:"trigger_glob"`
	Definition  string    `json:"-" gorm:"type:text"`
	CreatedAt   time.Time `json:"created_at" gorm:"index"`
	User        User      `json:"user,omitempty" gorm:"foreignKey:UserID"`
}
type ReleaseEvent struct {
	ID          uint `gorm:"primaryKey"`
	ReleaseID   uint `gorm:"uniqueIndex"`
	ProcessedAt *time.Time
	CreatedAt   time.Time
}
type WorkflowRun struct {
	ID                  uint              `json:"id" gorm:"primaryKey"`
	ProjectID           uint              `json:"project_id" gorm:"index"`
	WorkflowID          uint              `json:"workflow_id" gorm:"index"`
	ReleaseID           uint              `json:"release_id"`
	TriggerSource       string            `json:"trigger_source" gorm:"size:16;index"`
	ScheduledFor        *time.Time        `json:"scheduled_for,omitempty" gorm:"index"`
	DisplayVersion      string            `json:"display_version"`
	AllowParallel       bool              `json:"allow_parallel"`
	Status              string            `json:"status" gorm:"index"`
	Snapshot            string            `json:"-" gorm:"type:text"`
	EnvironmentSnapshot string            `json:"-" gorm:"type:text"`
	WorkerID            string            `json:"worker_id"`
	LeaseUntil          *time.Time        `json:"lease_until"`
	CancelRequested     bool              `json:"cancel_requested"`
	LogPath             string            `json:"-"`
	LogBytes            int64             `json:"log_bytes"`
	ErrorSummary        string            `json:"error_summary"`
	StartedAt           *time.Time        `json:"started_at"`
	FinishedAt          *time.Time        `json:"finished_at"`
	CreatedAt           time.Time         `json:"created_at"`
	Nodes               []WorkflowNodeRun `json:"nodes,omitempty" gorm:"foreignKey:RunID"`
}
type WorkflowSchedule struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	ProjectID     uint       `json:"project_id" gorm:"index;not null"`
	WorkflowID    uint       `json:"workflow_id" gorm:"uniqueIndex;not null"`
	Cron          string     `json:"cron" gorm:"size:100;not null"`
	Timezone      string     `json:"timezone" gorm:"size:100;not null"`
	Enabled       bool       `json:"enabled" gorm:"index"`
	AllowParallel bool       `json:"allow_parallel"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty" gorm:"index"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
type ScheduleEvent struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ScheduleID   uint      `json:"schedule_id" gorm:"uniqueIndex:uniq_schedule_fire;index;not null"`
	ScheduledFor time.Time `json:"scheduled_for" gorm:"uniqueIndex:uniq_schedule_fire;index;not null"`
	Status       string    `json:"status" gorm:"size:16;index"`
	RunID        uint      `json:"run_id,omitempty" gorm:"index"`
	Message      string    `json:"message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
type WorkflowNodeRun struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	RunID              uint       `json:"run_id" gorm:"index"`
	NodeKey            string     `json:"node_key"`
	Module             string     `json:"module"`
	Status             string     `json:"status"`
	Attempts           int        `json:"attempts"`
	ErrorSummary       string     `json:"error_summary"`
	Outputs            string     `json:"outputs,omitempty" gorm:"type:text"`
	SensitiveOutputs   string     `json:"-" gorm:"type:text"`
	OutputsSensitive   bool       `json:"outputs_sensitive"`
	LoopNodeKey        string     `json:"loop_node_key,omitempty" gorm:"index"`
	IterationIndex     *int       `json:"iteration_index,omitempty"`
	ServerListNodeKey  string     `json:"server_list_node_key,omitempty" gorm:"index"`
	ServerConnectionID uint       `json:"server_connection_id,omitempty" gorm:"index"`
	ServerIndex        *int       `json:"server_index,omitempty"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
}
type EnvironmentVariable struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	UserID         uint      `json:"-" gorm:"uniqueIndex:uniq_env_scope_name;not null"`
	ProjectID      uint      `json:"project_id" gorm:"uniqueIndex:uniq_env_scope_name;not null;default:0"`
	Name           string    `json:"name" gorm:"uniqueIndex:uniq_env_scope_name;size:100;not null"`
	Value          string    `json:"value,omitempty" gorm:"type:text"`
	ValueEncrypted string    `json:"-" gorm:"type:text"`
	Sensitive      bool      `json:"sensitive"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type AIProvider struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	UserID           uint      `json:"-" gorm:"index;not null"`
	Name             string    `json:"name"`
	BaseURL          string    `json:"base_url"`
	Model            string    `json:"model"`
	APIKeyEncrypted  string    `json:"-" gorm:"type:text"`
	HeadersEncrypted string    `json:"-" gorm:"type:text"`
	TimeoutSeconds   int       `json:"timeout_seconds"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type AIConversation struct {
	ID             uint        `json:"id" gorm:"primaryKey"`
	UserID         uint        `json:"-" gorm:"index;not null"`
	ProjectID      uint        `json:"project_id" gorm:"index;not null"`
	WorkflowID     *uint       `json:"workflow_id" gorm:"index"`
	ProviderID     uint        `json:"provider_id"`
	Title          string      `json:"title"`
	CanvasSnapshot string      `json:"canvas_snapshot" gorm:"type:text"`
	CanvasRevision int64       `json:"canvas_revision"`
	Generating     bool        `json:"generating"`
	LastActiveAt   time.Time   `json:"last_active_at"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Messages       []AIMessage `json:"messages,omitempty" gorm:"foreignKey:ConversationID"`
}
type AIMessage struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ConversationID uint      `json:"conversation_id" gorm:"index;not null"`
	Role           string    `json:"role"`
	Content        string    `json:"content" gorm:"type:text"`
	ToolCalls      string    `json:"tool_calls,omitempty" gorm:"type:text"`
	ToolResults    string    `json:"tool_results,omitempty" gorm:"type:text"`
	Operations     string    `json:"operations,omitempty" gorm:"type:text"`
	Status         string    `json:"status"`
	ErrorSummary   string    `json:"error_summary,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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
	Kind         string    `json:"kind" gorm:"size:16;not null;default:uploaded"`
	ArchiveName  string    `json:"archive_name,omitempty" gorm:"size:255"`
	CreatedAt    time.Time `json:"created_at"`
}

type ArtifactAccessLog struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ProjectID      uint      `json:"project_id" gorm:"index;not null"`
	ReleaseID      uint      `json:"release_id" gorm:"index;not null"`
	ArtifactFileID uint      `json:"artifact_file_id" gorm:"index;not null"`
	UserID         *uint     `json:"user_id,omitempty" gorm:"index"`
	IPAddress      string    `json:"ip_address" gorm:"size:128;not null"`
	AccessMethod   string    `json:"access_method" gorm:"size:32;not null"`
	HTTPMethod     string    `json:"http_method" gorm:"size:16;not null"`
	CreatedAt      time.Time `json:"created_at" gorm:"index"`
}
