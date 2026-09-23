package server

import (
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

type dashboardProject struct {
	ID            uint      `json:"id"`
	Name          string    `json:"name"`
	ReleaseCount  int64     `json:"release_count"`
	WorkflowCount int64     `json:"workflow_count"`
	LatestVersion string    `json:"latest_version"`
	LastActivity  time.Time `json:"last_activity"`
	Type          string    `json:"type"`
	Role          string    `json:"role"`
}
type dashboardRun struct {
	ID             uint       `json:"id"`
	ProjectID      uint       `json:"project_id"`
	WorkflowID     uint       `json:"workflow_id"`
	ProjectName    string     `json:"project_name"`
	WorkflowName   string     `json:"workflow_name"`
	Version        string     `json:"version"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	NodeCount      int64      `json:"node_count"`
	CompletedNodes int64      `json:"completed_nodes"`
	TriggerSource  string     `json:"trigger_source"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`
}

func (a *App) dashboard(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	var members []ProjectMember
	a.db.Where("user_id=?", uid).Find(&members)
	memberIDs := []uint{}
	roles := map[uint]string{}
	for _, m := range members {
		memberIDs = append(memberIDs, m.ProjectID)
		roles[m.ProjectID] = m.Role
	}
	var projects []Project
	if len(memberIDs) > 0 {
		a.db.Where("id IN ?", memberIDs).Order("id desc").Find(&projects)
	}
	summaries := make([]dashboardProject, 0, len(projects))
	var releases, workflows, running, failed int64
	for _, p := range projects {
		var rc, wc int64
		a.db.Model(&Release{}).Where("project_id=?", p.ID).Count(&rc)
		a.db.Model(&Workflow{}).Where("project_id=?", p.ID).Count(&wc)
		var latest Release
		a.db.Where("project_id=?", p.ID).Order("id desc").First(&latest)
		activity := p.CreatedAt
		if !latest.CreatedAt.IsZero() {
			activity = latest.CreatedAt
		}
		summaries = append(summaries, dashboardProject{ID: p.ID, Name: p.Name, ReleaseCount: rc, WorkflowCount: wc, LatestVersion: latest.Version, LastActivity: activity, Type: p.Type, Role: roles[p.ID]})
		releases += rc
		workflows += wc
	}
	ids := make([]uint, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	runs := []dashboardRun{}
	if len(ids) > 0 {
		a.db.Model(&WorkflowRun{}).Where("project_id IN ? AND status='running'", ids).Count(&running)
		a.db.Model(&WorkflowRun{}).Where("project_id IN ? AND status='failed' AND created_at>=?", ids, time.Now().Add(-7*24*time.Hour)).Count(&failed)
		var raw []WorkflowRun
		a.db.Where("project_id IN ?", ids).Order("id desc").Limit(10).Find(&raw)
		for _, r := range raw {
			var p Project
			var w Workflow
			var rel Release
			var total, done int64
			a.db.First(&p, r.ProjectID)
			a.db.First(&w, r.WorkflowID)
			a.db.First(&rel, r.ReleaseID)
			if rel.Version == "" {
				rel.Version = r.DisplayVersion
			}
			a.db.Model(&WorkflowNodeRun{}).Where("run_id=?", r.ID).Count(&total)
			a.db.Model(&WorkflowNodeRun{}).Where("run_id=? AND status IN ?", r.ID, []string{"succeeded", "failed", "cancelled"}).Count(&done)
			runs = append(runs, dashboardRun{r.ID, r.ProjectID, r.WorkflowID, p.Name, w.Name, rel.Version, r.Status, r.CreatedAt, r.StartedAt, r.FinishedAt, total, done, r.TriggerSource, r.ScheduledFor})
		}
	}
	c.JSON(200, gin.H{"stats": gin.H{"projects": len(projects), "releases": releases, "workflows": workflows, "running": running, "failed": failed}, "projects": summaries, "runs": runs})
}
func (a *App) allRuns(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	var memberships []ProjectMember
	a.db.Where("user_id=?", uid).Find(&memberships)
	memberIDs := []uint{}
	for _, m := range memberships {
		memberIDs = append(memberIDs, m.ProjectID)
	}
	var projects []Project
	if len(memberIDs) > 0 {
		a.db.Where("id IN ?", memberIDs).Find(&projects)
	}
	ids := []uint{}
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	runs := []dashboardRun{}
	if len(ids) == 0 {
		c.JSON(200, runs)
		return
	}
	q := a.db.Where("project_id IN ?", ids)
	if raw := c.Query("project_id"); raw != "" {
		projectID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(c, 400, "invalid_project_id", "project_id must be an integer")
			return
		}
		q = q.Where("project_id=?", uint(projectID))
	}
	if raw := c.Query("workflow_id"); raw != "" {
		workflowID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(c, 400, "invalid_workflow_id", "workflow_id must be an integer")
			return
		}
		q = q.Where("workflow_id=?", uint(workflowID))
	}
	if s := c.Query("status"); s != "" {
		q = q.Where("status=?", s)
	}
	var raw []WorkflowRun
	q.Order("id desc").Limit(200).Find(&raw)
	for _, r := range raw {
		var p Project
		var w Workflow
		var rel Release
		var total, done int64
		a.db.First(&p, r.ProjectID)
		a.db.First(&w, r.WorkflowID)
		a.db.First(&rel, r.ReleaseID)
		if rel.Version == "" {
			rel.Version = r.DisplayVersion
		}
		a.db.Model(&WorkflowNodeRun{}).Where("run_id=?", r.ID).Count(&total)
		a.db.Model(&WorkflowNodeRun{}).Where("run_id=? AND status IN ?", r.ID, []string{"succeeded", "failed", "cancelled"}).Count(&done)
		runs = append(runs, dashboardRun{r.ID, r.ProjectID, r.WorkflowID, p.Name, w.Name, rel.Version, r.Status, r.CreatedAt, r.StartedAt, r.FinishedAt, total, done, r.TriggerSource, r.ScheduledFor})
	}
	c.JSON(200, runs)
}
