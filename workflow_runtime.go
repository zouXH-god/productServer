package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type runtimeValues struct {
	Env         environmentSnapshot
	Steps       map[string]map[string]any
	LoopItem    any
	LoopIndex   int
	Server      *runtimeServer
	TriggerType string
	ScheduledAt string
}

type runtimeServer struct {
	ID       uint   `json:"id"`
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	AuthType string `json:"auth_type"`
}

var templateToken = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func lookupRuntime(key string, v runtimeValues, p Project, r Release, input, work string) (any, bool) {
	switch key {
	case "project.id":
		return p.ID, true
	case "project.name":
		return p.Name, true
	case "release.id":
		return r.ID, true
	case "release.version":
		return r.Version, true
	case "release.ref_type":
		return r.RefType, true
	case "release.commit_sha":
		return r.CommitSHA, true
	case "release.branch":
		return r.Branch, true
	case "workspace.input":
		return input, true
	case "workspace.work":
		return work, true
	case "loop.item":
		return v.LoopItem, true
	case "loop.index":
		return v.LoopIndex, true
	case "trigger.type":
		return v.TriggerType, true
	case "trigger.scheduled_at":
		return v.ScheduledAt, true
	}
	if strings.HasPrefix(key, "server.") && v.Server != nil {
		switch strings.TrimPrefix(key, "server.") {
		case "id":
			return v.Server.ID, true
		case "index":
			return v.Server.Index, true
		case "name":
			return v.Server.Name, true
		case "host":
			return v.Server.Host, true
		case "port":
			return v.Server.Port, true
		case "username":
			return v.Server.Username, true
		case "auth_type":
			return v.Server.AuthType, true
		}
	}
	if strings.HasPrefix(key, "env.global.") {
		x, ok := v.Env.Global[strings.TrimPrefix(key, "env.global.")]
		return x, ok
	}
	if strings.HasPrefix(key, "env.project.") {
		x, ok := v.Env.Project[strings.TrimPrefix(key, "env.project.")]
		return x, ok
	}
	if strings.HasPrefix(key, "env.") {
		name := strings.TrimPrefix(key, "env.")
		if x, ok := v.Env.Project[name]; ok {
			return x, true
		}
		x, ok := v.Env.Global[name]
		return x, ok
	}
	parts := strings.Split(key, ".")
	if len(parts) >= 4 && parts[0] == "steps" && parts[2] == "outputs" {
		current, ok := any(v.Steps[parts[1]]).(map[string]any)
		if !ok {
			return nil, false
		}
		var value any = current
		for _, part := range parts[3:] {
			m, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			value, ok = m[part]
			if !ok {
				return nil, false
			}
		}
		return value, true
	}
	return nil, false
}
func renderRuntimeString(raw string, v runtimeValues, p Project, r Release, input, work string) (string, error) {
	var failure error
	result := templateToken.ReplaceAllStringFunc(raw, func(token string) string {
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(token, "{{"), "}}"))
		value, ok := lookupRuntime(key, v, p, r, input, work)
		if !ok {
			failure = fmt.Errorf("template value %s is unavailable", key)
			return ""
		}
		switch x := value.(type) {
		case string:
			return x
		default:
			data, _ := json.Marshal(x)
			return string(data)
		}
	})
	return result, failure
}
func renderRuntimeConfig(config map[string]any, v runtimeValues, p Project, r Release, input, work string) (map[string]any, error) {
	copy := map[string]any{}
	for key, value := range config {
		switch x := value.(type) {
		case string:
			y, err := renderRuntimeString(x, v, p, r, input, work)
			if err != nil {
				return nil, err
			}
			copy[key] = y
		case []any:
			items := make([]any, len(x))
			for i, item := range x {
				if text, ok := item.(string); ok {
					rendered, err := renderRuntimeString(text, v, p, r, input, work)
					if err != nil {
						return nil, err
					}
					items[i] = rendered
				} else {
					items[i] = item
				}
			}
			copy[key] = items
		default:
			copy[key] = value
		}
	}
	return copy, nil
}

type nodeResult struct {
	ID         string
	OK         bool
	Skipped    bool
	Matched    *bool
	Outputs    map[string]any
	EndID      string
	EndOutputs map[string]any
}

func loopBody(def WorkflowDefinition, start, end string) map[string]WorkflowNode {
	all := map[string]WorkflowNode{}
	out := map[string][]string{}
	for _, n := range def.Nodes {
		all[n.ID] = n
	}
	for _, e := range def.Edges {
		out[e.From] = append(out[e.From], e.To)
	}
	result := map[string]WorkflowNode{}
	queue := append([]string{}, out[start]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == end {
			continue
		}
		if _, ok := result[id]; ok {
			continue
		}
		if n, ok := all[id]; ok {
			result[id] = n
			queue = append(queue, out[id]...)
		}
	}
	return result
}

func edgeActive(edge WorkflowEdge, result nodeResult) bool {
	condition := edge.Condition
	if condition == "" {
		condition = "success"
	}
	switch condition {
	case "always":
		return !result.Skipped
	case "true":
		return result.OK && result.Matched != nil && *result.Matched
	case "false":
		return result.OK && result.Matched != nil && !*result.Matched
	default:
		return result.OK && !result.Skipped
	}
}

func executeRunV2(db *gorm.DB, cfg Config, worker string, run WorkflowRun) {
	var def WorkflowDefinition
	if json.Unmarshal([]byte(run.Snapshot), &def) != nil {
		finishRun(db, run.ID, "failed", "invalid workflow snapshot")
		return
	}
	var release Release
	var project Project
	if run.ReleaseID != 0 {
		db.Preload("Files").First(&release, run.ReleaseID)
	} else {
		release.Version = run.DisplayVersion
	}
	db.First(&project, run.ProjectID)
	env, err := decodeEnvironmentSnapshot(cfg, run.EnvironmentSnapshot)
	if err != nil {
		finishRun(db, run.ID, "failed", err.Error())
		return
	}
	root := filepath.Join(cfg.WorkflowWorkDir, fmt.Sprintf("run-%d", run.ID))
	input, work := filepath.Join(root, "input"), filepath.Join(root, "work")
	os.MkdirAll(input, 0755)
	os.MkdirAll(work, 0755)
	for _, file := range release.Files {
		target, pathErr := releaseWorkspacePath(input, file)
		if pathErr != nil {
			finishRun(db, run.ID, "failed", pathErr.Error())
			return
		}
		if err = copyFile(filepath.Join(cfg.StorageDir, itoa(project.ID), itoa(release.ID), file.StoredName), target); err != nil {
			finishRun(db, run.ID, "failed", err.Error())
			return
		}
	}
	log := &runLogger{path: run.LogPath, db: db, runID: run.ID, secrets: env.Secrets}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go heartbeat(ctx, db, cfg, worker, run.ID)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var current WorkflowRun
				if db.Select("cancel_requested").First(&current, run.ID).Error == nil && current.CancelRequested {
					cancel()
					return
				}
			}
		}
	}()
	nodes := map[string]WorkflowNode{}
	incoming := map[string][]WorkflowEdge{}
	owned := map[string]bool{}
	scopeEnds := map[string]bool{}
	for _, n := range def.Nodes {
		if n.Type == "foreach" || n.Type == "server_list" {
			end, _ := n.Config["end_node_id"].(string)
			scopeEnds[end] = true
			owned[end] = true
			for id := range loopBody(def, n.ID, end) {
				owned[id] = true
			}
		}
	}
	for _, n := range def.Nodes {
		nodes[n.ID] = n
		if !owned[n.ID] || scopeEnds[n.ID] {
			db.Where(WorkflowNodeRun{RunID: run.ID, NodeKey: n.ID}).FirstOrCreate(&WorkflowNodeRun{RunID: run.ID, NodeKey: n.ID, Module: n.Type, Status: "pending"})
		}
	}
	for _, edge := range def.Edges {
		incoming[edge.To] = append(incoming[edge.To], edge)
	}
	results := map[string]nodeResult{}
	steps := map[string]map[string]any{}
	var mu sync.Mutex
	failed := false
	topCount := len(nodes) - len(owned)
	completedTop := 0
	for completedTop < topCount && !failed {
		ready := []WorkflowNode{}
		for id, node := range nodes {
			if owned[id] {
				continue
			}
			if _, done := results[id]; done {
				continue
			}
			all := true
			for _, edge := range incoming[id] {
				if _, ok := results[edge.From]; !ok {
					all = false
					break
				}
			}
			if all {
				ready = append(ready, node)
			}
		}
		if len(ready) == 0 {
			failed = true
			break
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
		channel := make(chan nodeResult, len(ready))
		for _, node := range ready {
			active := len(incoming[node.ID]) == 0
			for _, edge := range incoming[node.ID] {
				if edgeActive(edge, results[edge.From]) {
					active = true
				}
			}
			if !active {
				db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, node.ID).Update("status", "skipped")
				channel <- nodeResult{ID: node.ID, OK: true, Skipped: true}
				continue
			}
			go func(n WorkflowNode) {
				mu.Lock()
				values := runtimeValues{Env: env, Steps: cloneStepOutputs(steps), TriggerType: run.TriggerSource}
				if run.ScheduledFor != nil {
					values.ScheduledAt = run.ScheduledFor.Format(time.RFC3339)
				}
				mu.Unlock()
				if n.Type == "foreach" {
					channel <- executeLoopNode(ctx, db, cfg, run, project, release, def, n, input, work, log, values)
				} else if n.Type == "server_list" {
					channel <- executeServerListNode(ctx, db, cfg, run, project, release, def, n, input, work, log, values)
				} else {
					channel <- executeRuntimeNode(ctx, db, cfg, run, project, release, n, input, work, log, values, "", nil)
				}
			}(node)
		}
		for range ready {
			result := <-channel
			results[result.ID] = result
			completedTop++
			if result.Outputs != nil {
				steps[result.ID] = result.Outputs
			}
			if result.EndID != "" {
				results[result.EndID] = nodeResult{ID: result.EndID, OK: result.OK, Outputs: result.EndOutputs}
				steps[result.EndID] = result.EndOutputs
			}
			if !result.OK {
				failed = true
				cancel()
			}
		}
	}
	var current WorkflowRun
	db.Select("cancel_requested").First(&current, run.ID)
	if current.CancelRequested {
		finishRun(db, run.ID, "cancelled", "cancel requested")
	} else if failed {
		finishRun(db, run.ID, "failed", "one or more nodes failed")
	} else {
		finishRun(db, run.ID, "succeeded", "")
		os.RemoveAll(root)
	}
	db.Where("run_id=?", run.ID).Delete(&WorkflowLock{})
}

func executeLoopNode(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, def WorkflowDefinition, n WorkflowNode, input, work string, log *runLogger, values runtimeValues) nodeResult {
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": "running", "started_at": time.Now()})
	end, _ := n.Config["end_node_id"].(string)
	fail := func(message string) nodeResult {
		db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": "failed", "error_summary": message, "finished_at": time.Now()})
		return nodeResult{ID: n.ID, OK: false, EndID: end}
	}
	ref, _ := n.Config["items_from"].(string)
	value, ok := lookupRuntime(strings.TrimSpace(ref), values, p, r, input, work)
	if !ok {
		return fail("loop items output is unavailable")
	}
	var items []any
	switch x := value.(type) {
	case []any:
		items = x
	case []string:
		for _, v := range x {
			items = append(items, v)
		}
	default:
		return fail("loop items must be an array")
	}
	if len(items) > 100 {
		return fail("loop items exceeds 100")
	}
	body := loopBody(def, n.ID, end)
	incoming := map[string][]WorkflowEdge{}
	for _, e := range def.Edges {
		if _, yes := body[e.To]; yes {
			incoming[e.To] = append(incoming[e.To], e)
		}
	}
	aggregate := make([]any, len(items))
	for index, item := range items {
		local := cloneStepOutputs(values.Steps)
		done := map[string]nodeResult{}
		failed := false
		for len(done) < len(body) && !failed {
			ready := []WorkflowNode{}
			for id, node := range body {
				if _, yes := done[id]; yes {
					continue
				}
				all := true
				for _, e := range incoming[id] {
					if e.From == n.ID {
						continue
					}
					if _, yes := done[e.From]; !yes {
						all = false
					}
				}
				if all {
					ready = append(ready, node)
				}
			}
			if len(ready) == 0 {
				failed = true
				break
			}
			for _, node := range ready {
				active := len(incoming[node.ID]) == 0
				for _, e := range incoming[node.ID] {
					if e.From == n.ID || edgeActive(e, done[e.From]) {
						active = true
					}
				}
				if !active {
					done[node.ID] = nodeResult{ID: node.ID, OK: true, Skipped: true}
					continue
				}
				i := index
				rv := runtimeValues{Env: values.Env, Steps: local, LoopItem: item, LoopIndex: index, TriggerType: values.TriggerType, ScheduledAt: values.ScheduledAt}
				res := executeRuntimeNode(ctx, db, cfg, run, p, r, node, input, work, log, rv, n.ID, &i)
				done[node.ID] = res
				if res.Outputs != nil {
					local[node.ID] = res.Outputs
				}
				if !res.OK {
					failed = true
					break
				}
			}
		}
		if failed {
			return fail("loop iteration failed")
		}
		stepOut := map[string]any{}
		for id := range body {
			if v, yes := local[id]; yes {
				stepOut[id] = v
			}
		}
		aggregate[index] = map[string]any{"item": item, "index": index, "steps": stepOut}
	}
	outputs := map[string]any{"items": items, "count": len(items)}
	endOutputs := map[string]any{"items": aggregate, "count": len(items)}
	raw, _ := json.Marshal(outputs)
	endRaw, _ := json.Marshal(endOutputs)
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": "succeeded", "outputs": string(raw), "finished_at": time.Now()})
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, end).Updates(map[string]any{"status": "succeeded", "outputs": string(endRaw), "started_at": time.Now(), "finished_at": time.Now()})
	return nodeResult{ID: n.ID, OK: true, Outputs: outputs, EndID: end, EndOutputs: endOutputs}
}

func executeServerListNode(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, def WorkflowDefinition, n WorkflowNode, input, work string, log *runLogger, values runtimeValues) nodeResult {
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": "running", "started_at": time.Now()})
	end, _ := n.Config["end_node_id"].(string)
	fail := func(message string) nodeResult {
		db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": "failed", "error_summary": message, "finished_at": time.Now()})
		db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, end).Updates(map[string]any{"status": "failed", "error_summary": message, "finished_at": time.Now()})
		return nodeResult{ID: n.ID, OK: false, EndID: end}
	}
	ids, ok := uintList(n.Config["connection_ids"])
	if !ok || len(ids) == 0 {
		return fail("server list requires at least one connection")
	}
	servers := make([]runtimeServer, 0, len(ids))
	for index, id := range ids {
		var conn SSHConnection
		if db.Select("id,name,host,port,username,auth_type").Where("id=? AND user_id=?", id, p.UserID).First(&conn).Error != nil {
			return fail(fmt.Sprintf("server connection %d is unavailable", id))
		}
		servers = append(servers, runtimeServer{ID: conn.ID, Index: index, Name: conn.Name, Host: conn.Host, Port: conn.Port, Username: conn.Username, AuthType: conn.AuthType})
	}
	body := loopBody(def, n.ID, end)
	incoming := map[string][]WorkflowEdge{}
	for _, e := range def.Edges {
		if _, yes := body[e.To]; yes {
			incoming[e.To] = append(incoming[e.To], e)
		}
	}
	type serverResult struct {
		index  int
		item   map[string]any
		failed bool
	}
	results := make(chan serverResult, len(servers))
	limit := 1
	mode, _ := n.Config["mode"].(string)
	if mode != "sequential" {
		limit = int(num(n.Config, "concurrency"))
		if limit <= 0 {
			limit = 4
		}
		if limit > len(servers) {
			limit = len(servers)
		}
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for index := range servers {
		server := servers[index]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- serverResult{index: server.Index, failed: true, item: map[string]any{"server": server, "status": "cancelled", "error": ctx.Err().Error()}}
				return
			}
			defer func() { <-sem }()
			local := cloneStepOutputs(values.Steps)
			done := map[string]nodeResult{}
			failed, message := false, ""
			for len(done) < len(body) && !failed {
				ready := []WorkflowNode{}
				for id, node := range body {
					if _, yes := done[id]; yes {
						continue
					}
					all := true
					for _, e := range incoming[id] {
						if e.From != n.ID {
							if _, yes := done[e.From]; !yes {
								all = false
							}
						}
					}
					if all {
						ready = append(ready, node)
					}
				}
				if len(ready) == 0 {
					failed, message = true, "server task graph could not make progress"
					break
				}
				sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
				for _, node := range ready {
					active := len(incoming[node.ID]) == 0
					for _, e := range incoming[node.ID] {
						if e.From == n.ID || edgeActive(e, done[e.From]) {
							active = true
						}
					}
					if !active {
						done[node.ID] = nodeResult{ID: node.ID, OK: true, Skipped: true}
						continue
					}
					rv := runtimeValues{Env: values.Env, Steps: local, Server: &server, TriggerType: values.TriggerType, ScheduledAt: values.ScheduledAt}
					res := executeRuntimeNode(ctx, db, cfg, run, p, r, node, input, work, log, rv, n.ID, nil)
					done[node.ID] = res
					if res.Outputs != nil {
						local[node.ID] = res.Outputs
					}
					if !res.OK {
						failed = true
						message = "one or more server task nodes failed"
						break
					}
				}
			}
			stepOut := map[string]any{}
			for id := range body {
				sensitive, _ := body[id].Config["sensitive_output"].(bool)
				if output, yes := local[id]; yes && !sensitive {
					stepOut[id] = output
				}
			}
			status := "succeeded"
			if failed {
				status = "failed"
			}
			results <- serverResult{index: server.Index, failed: failed, item: map[string]any{"server": server, "status": status, "error": message, "steps": stepOut}}
		}()
	}
	wg.Wait()
	close(results)
	aggregate := make([]any, len(servers))
	anyFailed := false
	for result := range results {
		aggregate[result.index] = result.item
		anyFailed = anyFailed || result.failed
	}
	serverItems := make([]any, len(servers))
	for i := range servers {
		serverItems[i] = servers[i]
	}
	outputs := map[string]any{"servers": serverItems, "count": len(servers)}
	endOutputs := map[string]any{"items": aggregate, "count": len(servers)}
	raw, _ := json.Marshal(outputs)
	endRaw, _ := json.Marshal(endOutputs)
	status := "succeeded"
	if anyFailed {
		status = "failed"
	}
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, n.ID).Updates(map[string]any{"status": status, "outputs": string(raw), "finished_at": time.Now()})
	db.Model(&WorkflowNodeRun{}).Where("run_id=? AND node_key=?", run.ID, end).Updates(map[string]any{"status": status, "outputs": string(endRaw), "started_at": time.Now(), "finished_at": time.Now()})
	return nodeResult{ID: n.ID, OK: !anyFailed, Outputs: outputs, EndID: end, EndOutputs: endOutputs}
}
func cloneStepOutputs(source map[string]map[string]any) map[string]map[string]any {
	result := map[string]map[string]any{}
	for key, value := range source {
		copy := map[string]any{}
		for k, v := range value {
			copy[k] = v
		}
		result[key] = copy
	}
	return result
}
func executeRuntimeNode(parent context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, n WorkflowNode, input, work string, log *runLogger, values runtimeValues, loopKey string, index *int) nodeResult {
	key := n.ID
	if values.Server != nil {
		key = fmt.Sprintf("%s[server:%d]", n.ID, values.Server.ID)
	} else if index != nil {
		key = n.ID + "[" + strconv.Itoa(*index) + "]"
	}
	var nr WorkflowNodeRun
	seed := WorkflowNodeRun{RunID: run.ID, NodeKey: key, Module: n.Type, Status: "pending", LoopNodeKey: loopKey, IterationIndex: index}
	if values.Server != nil {
		seed.ServerListNodeKey = loopKey
		seed.ServerConnectionID = values.Server.ID
		i := values.Server.Index
		seed.ServerIndex = &i
		seed.LoopNodeKey = ""
	}
	db.Where(WorkflowNodeRun{RunID: run.ID, NodeKey: key}).FirstOrCreate(&nr, seed)
	var slot uint
	for {
		var ok bool
		slot, ok = acquireSlot(db, run.WorkerID, nr.ID, cfg.WorkerLease)
		if ok {
			break
		}
		select {
		case <-parent.Done():
			return nodeResult{ID: n.ID, OK: false}
		case <-time.After(250 * time.Millisecond):
		}
	}
	defer db.Model(&ExecutionSlot{}).Where("id=?", slot).Updates(map[string]any{"worker_id": "", "node_run_id": 0, "lease_until": nil})
	timeout := time.Duration(n.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	config, err := renderRuntimeConfig(n.Config, values, p, r, input, work)
	if err != nil {
		db.Model(&nr).Updates(map[string]any{"status": "failed", "error_summary": err.Error()})
		return nodeResult{ID: n.ID, OK: false}
	}
	n.Config = config
	if values.Server != nil && serverModule(n.Type) {
		n.Config["connection_id"] = values.Server.ID
	}
	db.Model(&nr).Updates(map[string]any{"status": "running", "started_at": time.Now()})
	log.write(key, "system", fmt.Sprintf("开始执行 %s", n.Type))
	var outputs map[string]any
	var matched *bool
	attempts := n.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		db.Model(&nr).Update("attempts", attempt)
		outputs, matched, err = executeModule(ctx, db, cfg, run, p, r, n, input, work, log)
		if err == nil {
			break
		}
	}
	if err != nil {
		db.Model(&nr).Updates(map[string]any{"status": "failed", "error_summary": err.Error(), "finished_at": time.Now()})
		log.write(key, "stderr", err.Error())
		return nodeResult{ID: n.ID, OK: false}
	}
	raw, _ := json.Marshal(outputs)
	if len(raw) > 1<<20 {
		err = fmt.Errorf("node output exceeds 1 MiB")
		db.Model(&nr).Updates(map[string]any{"status": "failed", "error_summary": err.Error()})
		return nodeResult{ID: n.ID, OK: false}
	}
	sensitive, _ := n.Config["sensitive_output"].(bool)
	updates := map[string]any{"status": "succeeded", "finished_at": time.Now(), "outputs_sensitive": sensitive}
	if sensitive {
		encrypted, e := encryptSecret(cfg.SecretEncryptionKey, string(raw))
		if e != nil {
			return nodeResult{ID: n.ID, OK: false}
		}
		updates["sensitive_outputs"] = encrypted
	} else {
		updates["outputs"] = string(raw)
		if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
			log.write(key, "output", string(raw))
		}
	}
	db.Model(&nr).Updates(updates)
	return nodeResult{ID: n.ID, OK: true, Matched: matched, Outputs: outputs}
}
