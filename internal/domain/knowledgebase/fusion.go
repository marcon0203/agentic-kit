package knowledgebase

import (
	"sort"
	"strconv"
)

// rrfK is the standard Reciprocal Rank Fusion damping constant (Cormack,
// Clarke & Buettcher 2009) — large enough that a result's exact rank
// matters less than simply appearing near the top of either list.
const rrfK = 60

// fuseRRF combines any number of independently-ranked result lists (here:
// Milvus vector search, Elasticsearch keyword search — the two legs of
// 多路召回) into one ranking via Reciprocal Rank Fusion: a chunk scores
// 1/(rrfK+rank+1) in each list it appears in, summed across every list,
// then the merged set is sorted descending by that sum. A chunk both
// routes agree on outranks one only a single route found — that agreement
// is the entire reason to run two retrieval routes instead of one.
//
// A pure function, deliberately: fusion is a ranking algorithm, not I/O,
// so it belongs in the domain and is trivially unit-testable without a
// live Milvus or Elasticsearch.
func fuseRRF(topK int, ranked ...[]SearchResult) []SearchResult {
	type entry struct {
		result SearchResult
		score  float64
	}
	byKey := make(map[string]*entry)
	order := make([]string, 0, topK*2)
	for _, list := range ranked {
		for rank, r := range list {
			key := chunkKey(r.SourceRef, r.ChunkIndex)
			e, ok := byKey[key]
			if !ok {
				e = &entry{result: r}
				byKey[key] = e
				order = append(order, key)
			}
			e.score += 1.0 / float64(rrfK+rank+1)
		}
	}

	out := make([]SearchResult, 0, len(order))
	for _, key := range order {
		e := byKey[key]
		e.result.Score = e.score
		out = append(out, e.result)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out
}

func chunkKey(sourceRef string, chunkIndex int) string {
	return sourceRef + "\x00" + strconv.Itoa(chunkIndex)
}
