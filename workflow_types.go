package main

type WorkflowDefinition struct {
	Nodes []WorkflowNode `json:"nodes"`
	Edges []WorkflowEdge `json:"edges"`
}
type WorkflowNode struct {
	ID             string             `json:"id"`
	Type           string             `json:"type"`
	Config         map[string]any     `json:"config"`
	TimeoutSeconds int                `json:"timeout_seconds"`
	Retries        int                `json:"retries"`
	Position       map[string]float64 `json:"position,omitempty"`
}
type WorkflowEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

func validateDefinition(d WorkflowDefinition) bool {
	if len(d.Nodes) == 0 {
		return false
	}
	ids := map[string]bool{}
	indegree := map[string]int{}
	next := map[string][]string{}
	allowed := map[string]bool{"archive": true, "extract": true, "sftp_upload": true, "sftp_extract": true, "ssh_command": true, "http_webhook": true, "checksum_verify": true, "remote_file_exists": true, "value_match": true, "string_split": true, "foreach": true, "loop_end": true, "server_list": true, "server_list_end": true}
	for _, n := range d.Nodes {
		if n.ID == "" || ids[n.ID] || !allowed[n.Type] {
			return false
		}
		ids[n.ID] = true
		indegree[n.ID] = 0
	}
	for _, n := range d.Nodes {
		if n.Type != "foreach" && n.Type != "server_list" {
			continue
		}
		end, ok := n.Config["end_node_id"].(string)
		if !ok || end == "" || !ids[end] {
			return false
		}
		var endNode WorkflowNode
		for _, candidate := range d.Nodes {
			if candidate.ID == end {
				endNode = candidate
				break
			}
		}
		expected := "loop_end"
		if n.Type == "server_list" {
			expected = "server_list_end"
			ids, ok := uintList(n.Config["connection_ids"])
			if !ok || len(ids) == 0 {
				return false
			}
			mode, _ := n.Config["mode"].(string)
			if mode != "" && mode != "parallel" && mode != "sequential" {
				return false
			}
		}
		if endNode.Type != expected {
			return false
		}
	}
	for _, e := range d.Edges {
		if !ids[e.From] || !ids[e.To] || e.From == e.To {
			return false
		}
		if e.Condition != "" && e.Condition != "success" && e.Condition != "true" && e.Condition != "false" && e.Condition != "always" {
			return false
		}
		indegree[e.To]++
		next[e.From] = append(next[e.From], e.To)
	}
	q := []string{}
	for id, n := range indegree {
		if n == 0 {
			q = append(q, id)
		}
	}
	seen := 0
	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		seen++
		for _, to := range next[id] {
			indegree[to]--
			if indegree[to] == 0 {
				q = append(q, to)
			}
		}
	}
	if seen != len(d.Nodes) {
		return false
	}
	// Loop/server scopes are explicit subgraphs. They may not be nested and
	// may only be entered through their start or left through their end.
	claimed := map[string]string{}
	endOwners := map[string]string{}
	for _, n := range d.Nodes {
		if n.Type != "foreach" && n.Type != "server_list" {
			continue
		}
		end, _ := n.Config["end_node_id"].(string)
		if owner := endOwners[end]; owner != "" && owner != n.ID {
			return false
		}
		endOwners[end] = n.ID
		body := definitionScopeBody(d, n.ID, end)
		for id := range body {
			if owner := claimed[id]; owner != "" && owner != n.ID {
				return false
			}
			claimed[id] = n.ID
			for _, candidate := range d.Nodes {
				if candidate.ID == id && (candidate.Type == "foreach" || candidate.Type == "loop_end" || candidate.Type == "server_list" || candidate.Type == "server_list_end") {
					return false
				}
			}
		}
		for _, e := range d.Edges {
			_, fromBody := body[e.From]
			_, toBody := body[e.To]
			if toBody && e.From != n.ID && !fromBody {
				return false
			}
			if fromBody && e.To != end && !toBody {
				return false
			}
			if e.To == end && e.From != n.ID && !fromBody {
				return false
			}
		}
	}
	return true
}

func uintList(value any) ([]uint, bool) {
	result := []uint{}
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			switch x := value.(type) {
			case float64:
				if x <= 0 {
					return nil, false
				}
				result = append(result, uint(x))
			case int:
				if x <= 0 {
					return nil, false
				}
				result = append(result, uint(x))
			case uint:
				if x == 0 {
					return nil, false
				}
				result = append(result, x)
			default:
				return nil, false
			}
		}
	case []uint:
		result = append(result, values...)
	default:
		return nil, false
	}
	return result, true
}

func definitionScopeBody(d WorkflowDefinition, start, end string) map[string]struct{} {
	out := map[string][]string{}
	for _, e := range d.Edges {
		out[e.From] = append(out[e.From], e.To)
	}
	result := map[string]struct{}{}
	queue := append([]string{}, out[start]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == end {
			continue
		}
		if _, exists := result[id]; exists {
			continue
		}
		result[id] = struct{}{}
		queue = append(queue, out[id]...)
	}
	return result
}
