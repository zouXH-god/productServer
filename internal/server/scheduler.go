package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func nextSchedule(expression, timezone string, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone")
	}
	schedule, err := cronParser.Parse(expression)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid five-field cron: %w", err)
	}
	return schedule.Next(after.In(location)).UTC(), nil
}

func (a *App) scheduleRoutes(api *gin.RouterGroup) {
	api.GET("/projects/:id/workflows/:workflowId/schedule", a.getWorkflowSchedule)
	api.PUT("/projects/:id/workflows/:workflowId/schedule", a.saveWorkflowSchedule)
	api.DELETE("/projects/:id/workflows/:workflowId/schedule", a.deleteWorkflowSchedule)
	api.GET("/projects/:id/workflows/:workflowId/schedule-events", a.listScheduleEvents)
	api.POST("/projects/:id/workflows/:workflowId/schedule/trigger", a.triggerWorkflowSchedule)
}
func (a *App) scheduleContext(c *gin.Context, required int) (Project, Workflow, bool) {
	pid, ok := parseID(c, "id")
	if !ok {
		return Project{}, Workflow{}, false
	}
	project, _, ok := a.projectAccess(c, pid, required)
	if !ok {
		return project, Workflow{}, false
	}
	if project.Type != "scheduled" {
		fail(c, 400, "invalid_project_type", "schedules are only available for scheduled projects")
		return project, Workflow{}, false
	}
	var workflow Workflow
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&workflow).Error != nil {
		fail(c, 404, "not_found", "workflow not found")
		return project, workflow, false
	}
	return project, workflow, true
}
func (a *App) getWorkflowSchedule(c *gin.Context) {
	_, workflow, ok := a.scheduleContext(c, projectView)
	if !ok {
		return
	}
	var schedule WorkflowSchedule
	if a.db.Where("workflow_id=?", workflow.ID).First(&schedule).Error != nil {
		fail(c, 404, "not_found", "schedule not configured")
		return
	}
	c.JSON(200, schedule)
}
func (a *App) saveWorkflowSchedule(c *gin.Context) {
	project, workflow, ok := a.scheduleContext(c, projectDevelop)
	if !ok {
		return
	}
	var in struct {
		Cron          string `json:"cron"`
		Timezone      string `json:"timezone"`
		Enabled       bool   `json:"enabled"`
		AllowParallel bool   `json:"allow_parallel"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid schedule")
		return
	}
	in.Cron = strings.TrimSpace(in.Cron)
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.Timezone == "" {
		in.Timezone = "Asia/Shanghai"
	}
	next, err := nextSchedule(in.Cron, in.Timezone, time.Now())
	if err != nil {
		fail(c, 400, "invalid_schedule", err.Error())
		return
	}
	var schedule WorkflowSchedule
	result := a.db.Where("workflow_id=?", workflow.ID).First(&schedule)
	schedule.ProjectID = project.ID
	schedule.WorkflowID = workflow.ID
	schedule.Cron = in.Cron
	schedule.Timezone = in.Timezone
	schedule.Enabled = in.Enabled
	schedule.AllowParallel = in.AllowParallel
	if in.Enabled {
		schedule.NextRunAt = &next
	} else {
		schedule.NextRunAt = nil
	}
	if result.Error == gorm.ErrRecordNotFound {
		a.db.Create(&schedule)
	} else {
		a.db.Save(&schedule)
	}
	c.JSON(200, schedule)
}
func (a *App) deleteWorkflowSchedule(c *gin.Context) {
	_, workflow, ok := a.scheduleContext(c, projectDevelop)
	if !ok {
		return
	}
	a.db.Where("workflow_id=?", workflow.ID).Delete(&WorkflowSchedule{})
	c.Status(204)
}
func (a *App) listScheduleEvents(c *gin.Context) {
	_, workflow, ok := a.scheduleContext(c, projectView)
	if !ok {
		return
	}
	var schedule WorkflowSchedule
	if a.db.Where("workflow_id=?", workflow.ID).First(&schedule).Error != nil {
		c.JSON(200, []ScheduleEvent{})
		return
	}
	var events []ScheduleEvent
	a.db.Where("schedule_id=?", schedule.ID).Order("id desc").Limit(100).Find(&events)
	c.JSON(200, events)
}
func (a *App) triggerWorkflowSchedule(c *gin.Context) {
	project, workflow, ok := a.scheduleContext(c, projectDevelop)
	if !ok {
		return
	}
	var schedule WorkflowSchedule
	if a.db.Where("workflow_id=?", workflow.ID).First(&schedule).Error != nil {
		fail(c, 404, "not_found", "schedule not configured")
		return
	}
	event, run, err := a.fireSchedule(project, workflow, schedule, time.Now().UTC())
	if err != nil {
		fail(c, 500, "schedule_failed", err.Error())
		return
	}
	c.JSON(201, gin.H{"event": event, "run": run})
}

func (a *App) dispatchSchedules() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		a.dispatchDueSchedules()
		<-ticker.C
	}
}
func (a *App) dispatchDueSchedules() {
	now := time.Now().UTC()
	var schedules []WorkflowSchedule
	a.db.Where("enabled=? AND next_run_at IS NOT NULL AND next_run_at<=?", true, now).Order("next_run_at").Limit(50).Find(&schedules)
	for _, schedule := range schedules {
		due := *schedule.NextRunAt
		next, err := nextSchedule(schedule.Cron, schedule.Timezone, now)
		if err != nil {
			a.db.Model(&schedule).Updates(map[string]any{"enabled": false, "next_run_at": nil})
			continue
		}
		claimed := a.db.Model(&WorkflowSchedule{}).Where("id=? AND enabled=? AND next_run_at<=?", schedule.ID, true, now).Updates(map[string]any{"next_run_at": next, "last_run_at": due})
		if claimed.RowsAffected != 1 {
			continue
		}
		var project Project
		var workflow Workflow
		if a.db.First(&project, schedule.ProjectID).Error != nil || a.db.First(&workflow, schedule.WorkflowID).Error != nil {
			continue
		}
		a.fireSchedule(project, workflow, schedule, due)
	}
}
func (a *App) fireSchedule(project Project, workflow Workflow, schedule WorkflowSchedule, scheduledFor time.Time) (ScheduleEvent, *WorkflowRun, error) {
	event := ScheduleEvent{ScheduleID: schedule.ID, ScheduledFor: scheduledFor, Status: "created"}
	if err := a.db.Create(&event).Error; err != nil {
		return event, nil, err
	}
	if !workflow.Enabled {
		event.Status = "skipped"
		event.Message = "workflow is disabled"
		a.db.Save(&event)
		return event, nil, nil
	}
	if !schedule.AllowParallel {
		var count int64
		a.db.Model(&WorkflowRun{}).Where("workflow_id=? AND status IN ?", workflow.ID, []string{"queued", "running"}).Count(&count)
		if count > 0 {
			event.Status = "skipped"
			event.Message = "previous workflow run is still active"
			a.db.Save(&event)
			return event, nil, nil
		}
	}
	run, err := createScheduledRun(a.db, a.cfg, workflow, project, scheduledFor, schedule.AllowParallel)
	if err != nil {
		event.Status = "failed"
		event.Message = err.Error()
		a.db.Save(&event)
		return event, nil, err
	}
	event.RunID = run.ID
	a.db.Save(&event)
	return event, &run, nil
}
func createScheduledRun(db *gorm.DB, cfg Config, w Workflow, p Project, scheduledFor time.Time, parallel bool) (WorkflowRun, error) {
	return createEmptyRun(db, cfg, w, p, "schedule", &scheduledFor, parallel)
}
func createEmptyRun(db *gorm.DB, cfg Config, w Workflow, p Project, source string, scheduledFor *time.Time, parallel bool) (WorkflowRun, error) {
	environment, err := buildEnvironmentSnapshot(db, cfg, p.UserID, p.ID)
	if err != nil {
		return WorkflowRun{}, err
	}
	version := "manual"
	if scheduledFor != nil {
		version = scheduledFor.Format(time.RFC3339)
	}
	run := WorkflowRun{ProjectID: p.ID, WorkflowID: w.ID, Status: "queued", Snapshot: w.Definition, EnvironmentSnapshot: environment, TriggerSource: source, ScheduledFor: scheduledFor, DisplayVersion: version, AllowParallel: parallel}
	if err = db.Create(&run).Error; err != nil {
		return run, err
	}
	run.LogPath = filepath.Join(cfg.WorkflowLogDir, fmt.Sprintf("run-%d.jsonl", run.ID))
	err = db.Model(&run).Update("log_path", run.LogPath).Error
	return run, err
}
