package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
)

func (a *App) dispatchReleaseEvents() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var events []ReleaseEvent
		a.db.Where("processed_at IS NULL").Limit(20).Find(&events)
		for _, event := range events {
			var r Release
			if a.db.First(&r, event.ReleaseID).Error != nil {
				continue
			}
			var flows []Workflow
			a.db.Where("project_id=? AND enabled=?", r.ProjectID, true).Find(&flows)
			for _, w := range flows {
				if triggerMatches(w, r) {
					_, _ = createRun(a.db, a.cfg, w, r)
				}
			}
			now := time.Now()
			a.db.Model(&event).Update("processed_at", now)
		}
	}
}
func triggerMatches(w Workflow, r Release) bool {
	switch w.TriggerType {
	case "any":
		return true
	case "tag":
		return r.RefType == "tag"
	case "commit":
		return r.RefType == "commit"
	case "tag_glob":
		ok, _ := path.Match(w.TriggerGlob, r.Version)
		return r.RefType == "tag" && ok
	}
	return false
}
func createRun(db *gorm.DB, cfg Config, w Workflow, r Release) (WorkflowRun, error) {
	run := WorkflowRun{ProjectID: r.ProjectID, WorkflowID: w.ID, ReleaseID: r.ID, Status: "queued", Snapshot: w.Definition}
	e := db.Create(&run).Error
	if e == nil {
		run.LogPath = filepath.Join(cfg.WorkflowLogDir, fmt.Sprintf("run-%d.jsonl", run.ID))
		e = db.Model(&run).Update("log_path", run.LogPath).Error
	}
	return run, e
}

func runWorker(db *gorm.DB, cfg Config) error {
	if cfg.WorkerConcurrency <= 0 {
		cfg.WorkerConcurrency = 4
	}
	if cfg.WorkerLease <= 0 {
		cfg.WorkerLease = 30 * time.Second
	}
	if cfg.WorkerHeartbeat <= 0 {
		cfg.WorkerHeartbeat = 10 * time.Second
	}
	if err := os.MkdirAll(cfg.WorkflowWorkDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.WorkflowLogDir, 0755); err != nil {
		return err
	}
	worker := fmt.Sprintf("%s-%d", hostname(), os.Getpid())
	lastCleanup := time.Time{}
	for {
		if time.Since(lastCleanup) > time.Hour {
			cleanupWorkspaces(cfg)
			lastCleanup = time.Now()
		}
		run, ok := claimRun(db, cfg, worker)
		if !ok {
			time.Sleep(time.Second)
			continue
		}
		executeRun(db, cfg, worker, run)
	}
}
func hostname() string { h, _ := os.Hostname(); return h }
func claimRun(db *gorm.DB, cfg Config, worker string) (WorkflowRun, bool) {
	var list []WorkflowRun
	db.Where("status='queued' OR (status='running' AND lease_until < ?)", time.Now()).Order("id").Limit(10).Find(&list)
	for _, r := range list {
		until := time.Now().Add(cfg.WorkerLease)
		lock := WorkflowLock{ProjectID: r.ProjectID, WorkflowID: r.WorkflowID, RunID: r.ID, LeaseUntil: until}
		db.Where("project_id=? AND workflow_id=? AND lease_until < ?", r.ProjectID, r.WorkflowID, time.Now()).Delete(&WorkflowLock{})
		if db.Create(&lock).Error != nil {
			continue
		}
		res := db.Model(&WorkflowRun{}).Where("id=? AND (status='queued' OR lease_until < ?)", r.ID, time.Now()).Updates(map[string]any{"status": "running", "worker_id": worker, "lease_until": until, "started_at": time.Now()})
		if res.RowsAffected == 1 {
			r.Status = "running"
			r.WorkerID = worker
			r.LeaseUntil = &until
			return r, true
		}
		db.Where("run_id=?", r.ID).Delete(&WorkflowLock{})
	}
	return WorkflowRun{}, false
}

type runLogger struct {
	mu    sync.Mutex
	path  string
	db    *gorm.DB
	runID uint
}

func (l *runLogger) write(node, stream, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(l.path), 0755)
	f, e := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if e != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(map[string]any{"time": time.Now().UTC(), "node": node, "stream": stream, "message": msg})
	line = append(line, '\n')
	n, _ := f.Write(line)
	l.db.Model(&WorkflowRun{}).Where("id=?", l.runID).Updates(map[string]any{"log_bytes": gorm.Expr("log_bytes + ?", n)})
}
func executeRun(db *gorm.DB, cfg Config, worker string, run WorkflowRun) {
	var def WorkflowDefinition
	if json.Unmarshal([]byte(run.Snapshot), &def) != nil {
		finishRun(db, run.ID, "failed", "invalid workflow snapshot")
		return
	}
	var release Release
	var project Project
	db.Preload("Files").First(&release, run.ReleaseID)
	db.First(&project, run.ProjectID)
	root := filepath.Join(cfg.WorkflowWorkDir, fmt.Sprintf("run-%d", run.ID))
	input := filepath.Join(root, "input")
	work := filepath.Join(root, "work")
	os.MkdirAll(input, 0755)
	os.MkdirAll(work, 0755)
	for _, f := range release.Files {
		copyFile(filepath.Join(cfg.StorageDir, itoa(project.ID), itoa(release.ID), f.StoredName), filepath.Join(input, f.OriginalName))
	}
	log := &runLogger{path: run.LogPath, db: db, runID: run.ID}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go heartbeat(ctx, db, cfg, worker, run.ID)
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				var x WorkflowRun
				if db.Select("cancel_requested").First(&x, run.ID).Error == nil && x.CancelRequested {
					cancel()
					return
				}
			}
		}
	}()
	deps := map[string][]string{}
	nodes := map[string]WorkflowNode{}
	for _, n := range def.Nodes {
		nodes[n.ID] = n
		db.Where(WorkflowNodeRun{RunID: run.ID, NodeKey: n.ID}).FirstOrCreate(&WorkflowNodeRun{RunID: run.ID, NodeKey: n.ID, Module: n.Type, Status: "pending"})
	}
	for _, e := range def.Edges {
		deps[e.To] = append(deps[e.To], e.From)
	}
	done := map[string]bool{}
	failed := false
	for len(done) < len(nodes) && !failed {
		var ready []WorkflowNode
		for id, n := range nodes {
			if done[id] {
				continue
			}
			ok := true
			for _, d := range deps[id] {
				if !done[d] {
					ok = false
				}
			}
			if ok {
				ready = append(ready, n)
			}
		}
		if len(ready) == 0 {
			failed = true
			break
		}
		results := make(chan bool, len(ready))
		for _, n := range ready {
			go func(node WorkflowNode) {
				results <- executeNode(ctx, db, cfg, run, project, release, node, input, work, log)
			}(n)
		}
		for i, n := range ready {
			_ = i
			done[n.ID] = true
		}
		for range ready {
			ok := <-results
			if !ok {
				failed = true
				cancel()
			}
		}
		var fresh WorkflowRun
		db.First(&fresh, run.ID)
		if fresh.CancelRequested {
			finishRun(db, run.ID, "cancelled", "cancel requested")
			return
		}
	}
	if failed {
		finishRun(db, run.ID, "failed", "one or more nodes failed")
	} else {
		finishRun(db, run.ID, "succeeded", "")
		os.RemoveAll(root)
	}
	db.Where("run_id=?", run.ID).Delete(&WorkflowLock{})
}
func heartbeat(ctx context.Context, db *gorm.DB, cfg Config, worker string, runID uint) {
	t := time.NewTicker(cfg.WorkerHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			until := time.Now().Add(cfg.WorkerLease)
			db.Model(&WorkflowRun{}).Where("id=? AND worker_id=?", runID, worker).Update("lease_until", until)
			db.Model(&WorkflowLock{}).Where("run_id=?", runID).Update("lease_until", until)
		}
	}
}
func finishRun(db *gorm.DB, id uint, status, msg string) {
	now := time.Now()
	db.Model(&WorkflowRun{}).Where("id=?", id).Updates(map[string]any{"status": status, "error_summary": msg, "finished_at": now, "lease_until": nil})
}
func acquireSlot(db *gorm.DB, worker string, nodeID uint, lease time.Duration) (uint, bool) {
	var slots []ExecutionSlot
	db.Order("id").Find(&slots)
	for _, s := range slots {
		res := db.Model(&ExecutionSlot{}).Where("id=? AND (lease_until IS NULL OR lease_until < ?)", s.ID, time.Now()).Updates(map[string]any{"worker_id": worker, "node_run_id": nodeID, "lease_until": time.Now().Add(lease)})
		if res.RowsAffected == 1 {
			return s.ID, true
		}
	}
	return 0, false
}
func executeNode(parent context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, n WorkflowNode, input, work string, log *runLogger) bool {
	var nr WorkflowNodeRun
	db.Where("run_id=? AND node_key=?", run.ID, n.ID).First(&nr)
	slot, ok := acquireSlot(db, run.WorkerID, nr.ID, cfg.WorkerLease)
	for !ok {
		time.Sleep(500 * time.Millisecond)
		slot, ok = acquireSlot(db, run.WorkerID, nr.ID, cfg.WorkerLease)
	}
	defer db.Model(&ExecutionSlot{}).Where("id=?", slot).Updates(map[string]any{"worker_id": "", "node_run_id": 0, "lease_until": nil})
	slotCtx, stopSlot := context.WithCancel(parent)
	defer stopSlot()
	go func() {
		t := time.NewTicker(cfg.WorkerHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-slotCtx.Done():
				return
			case <-t.C:
				db.Model(&ExecutionSlot{}).Where("id=? AND node_run_id=?", slot, nr.ID).Update("lease_until", time.Now().Add(cfg.WorkerLease))
			}
		}
	}()
	attempts := n.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		timeout := time.Duration(n.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Minute
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		now := time.Now()
		db.Model(&nr).Updates(map[string]any{"status": "running", "attempts": attempt, "started_at": now})
		log.write(n.ID, "system", fmt.Sprintf("starting %s attempt %d", n.Type, attempt))
		e := executeModule(ctx, db, cfg, run, p, r, n, input, work, log)
		cancel()
		if e == nil {
			now = time.Now()
			db.Model(&nr).Updates(map[string]any{"status": "succeeded", "finished_at": now})
			return true
		}
		log.write(n.ID, "stderr", e.Error())
		if attempt == attempts {
			now = time.Now()
			db.Model(&nr).Updates(map[string]any{"status": "failed", "error_summary": e.Error(), "finished_at": now})
			return false
		}
	}
	return false
}
func cleanupWorkspaces(cfg Config) {
	if cfg.FailedWorkspaceRetention <= 0 {
		return
	}
	entries, _ := os.ReadDir(cfg.WorkflowWorkDir)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, e := entry.Info()
		if e == nil && time.Since(info.ModTime()) > cfg.FailedWorkspaceRetention {
			_ = os.RemoveAll(filepath.Join(cfg.WorkflowWorkDir, entry.Name()))
		}
	}
}
func copyFile(src, dst string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	out, e := os.Create(dst)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
