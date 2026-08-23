package bundlegraph

import "fmt"

// ParseGraph extracts a Graph from a Bundle definition already decoded into
// generic JSON (map[string]any / []any / string), i.e. the same shape
// dslschema.Validator.Validate consumes. Call this only after schema
// validation has passed — it assumes the document shape schemas/
// bundle.schema.json guarantees and returns an error if that assumption is
// violated (defensive, not a substitute for schema validation).
func ParseGraph(def map[string]any) (Graph, error) {
	var g Graph

	agentsRaw, _ := def["agents"].([]any)
	for _, a := range agentsRaw {
		agentMap, ok := a.(map[string]any)
		if !ok {
			return Graph{}, fmt.Errorf("bundlegraph: agents[] entry is not an object")
		}
		node, _ := agentMap["alias"].(string)
		if node == "" {
			node, _ = agentMap["ref"].(string)
		}
		if node == "" {
			return Graph{}, fmt.Errorf("bundlegraph: agents[] entry missing ref/alias")
		}
		g.Nodes = append(g.Nodes, node)
	}

	orch, ok := def["orchestration"].(map[string]any)
	if !ok {
		return Graph{}, fmt.Errorf("bundlegraph: missing orchestration block")
	}
	g.Entry, _ = orch["entry"].(string)

	edgesRaw, _ := orch["edges"].([]any)
	for i, e := range edgesRaw {
		edgeMap, ok := e.(map[string]any)
		if !ok {
			return Graph{}, fmt.Errorf("bundlegraph: edges[%d] is not an object", i)
		}
		from, _ := edgeMap["from"].(string)
		condition, _ := edgeMap["condition"].(string)
		mode, _ := edgeMap["mode"].(string)
		if mode == "" {
			mode = "sequential"
		}
		join, _ := edgeMap["join"].(string)
		if join == "" {
			join = "wait_all"
		}

		var to []string
		switch v := edgeMap["to"].(type) {
		case string:
			to = []string{v}
		case []any:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return Graph{}, fmt.Errorf("bundlegraph: edges[%d].to contains a non-string entry", i)
				}
				to = append(to, s)
			}
		default:
			return Graph{}, fmt.Errorf("bundlegraph: edges[%d].to has an unexpected type", i)
		}

		g.Edges = append(g.Edges, Edge{Index: i, From: from, To: to, Condition: condition, Mode: mode, Join: join})
	}

	return g, nil
}
