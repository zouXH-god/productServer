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
	From string `json:"from"`
	To   string `json:"to"`
}

func validateDefinition(d WorkflowDefinition) bool {
	if len(d.Nodes) == 0 {
		return false
	}
	ids := map[string]bool{}
	indegree := map[string]int{}
	next := map[string][]string{}
	allowed := map[string]bool{"archive": true, "extract": true, "sftp_upload": true, "ssh_command": true, "http_webhook": true, "checksum_verify": true}
	for _, n := range d.Nodes {
		if n.ID == "" || ids[n.ID] || !allowed[n.Type] {
			return false
		}
		ids[n.ID] = true
		indegree[n.ID] = 0
	}
	for _, e := range d.Edges {
		if !ids[e.From] || !ids[e.To] || e.From == e.To {
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
	return seen == len(d.Nodes)
}
