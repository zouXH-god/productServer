package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openDatabase(c Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch strings.ToLower(c.DBDriver) {
	case "sqlite":
		if dir := filepath.Dir(c.DBDSN); dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		dialector = sqlite.Open(c.DBDSN)
	case "mysql":
		dialector = mysql.Open(c.DBDSN)
	case "postgres", "pgsql":
		dialector = postgres.Open(c.DBDSN)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q", c.DBDriver)
	}
	return gorm.Open(dialector, &gorm.Config{})
}
func migrateAndBootstrap(db *gorm.DB, c Config) error {
	hadUserEnabled := db.Migrator().HasColumn(&User{}, "enabled")
	// Older versions allowed multiple tokens. Keep the newest before adding the
	// unique project constraint.
	if db.Migrator().HasTable(&ProjectToken{}) {
		var tokens []ProjectToken
		if err := db.Order("project_id, id desc").Find(&tokens).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		for _, token := range tokens {
			if seen[token.ProjectID] {
				if err := db.Delete(&ProjectToken{}, token.ID).Error; err != nil {
					return err
				}
			} else {
				seen[token.ProjectID] = true
			}
		}
	}
	if err := db.AutoMigrate(&User{}, &Project{}, &ProjectMember{}, &SystemSetting{}, &ProjectToken{}, &Release{}, &ArtifactFile{}, &ArtifactAccessLog{}, &SSHCredential{}, &SSHConnection{}, &Workflow{}, &WorkflowRevision{}, &WorkflowSchedule{}, &ScheduleEvent{}, &ReleaseEvent{}, &WorkflowRun{}, &WorkflowNodeRun{}, &WorkflowLock{}, &ExecutionSlot{}, &EnvironmentVariable{}, &AIProvider{}, &AIConversation{}, &AIMessage{}); err != nil {
		return err
	}
	db.Model(&Project{}).Where("type='' OR type IS NULL").Update("type", "artifact")
	if !hadUserEnabled {
		db.Model(&User{}).Where("enabled=?", false).Update("enabled", true)
	}
	db.FirstOrCreate(&SystemSetting{Key: "registration_enabled"}, SystemSetting{Key: "registration_enabled", Value: "true"})
	if err := normalizeStoredWorkflowCanvases(db); err != nil {
		return err
	}
	if c.WorkerConcurrency <= 0 {
		c.WorkerConcurrency = 4
	}
	for i := 1; i <= c.WorkerConcurrency; i++ {
		db.FirstOrCreate(&ExecutionSlot{}, ExecutionSlot{ID: uint(i)})
	}
	var projects []Project
	if err := db.Find(&projects).Error; err != nil {
		return err
	}
	for _, project := range projects {
		db.FirstOrCreate(&ProjectMember{}, ProjectMember{ProjectID: project.ID, UserID: project.UserID, Role: "owner"})
		if project.Type == "scheduled" {
			continue
		}
		var count int64
		if err := db.Model(&ProjectToken{}).Where("project_id = ?", project.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if _, _, err := generateProjectToken(db, project.ID); err != nil {
				return err
			}
		}
	}
	var count int64
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		var admins int64
		db.Model(&User{}).Where("is_admin=?", true).Count(&admins)
		if admins == 0 {
			var first User
			if db.Order("id").First(&first).Error == nil {
				db.Model(&first).Updates(map[string]any{"is_admin": true, "enabled": true})
			}
		}
		return nil
	}
	if strings.TrimSpace(c.AdminUsername) == "" || c.AdminPassword == "" {
		return fmt.Errorf("ADMIN_USERNAME and ADMIN_PASSWORD are required for initial user")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Create(&User{Username: strings.TrimSpace(c.AdminUsername), PasswordHash: string(h), IsAdmin: true, Enabled: true}).Error
}
