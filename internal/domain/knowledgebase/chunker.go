package knowledgebase

import "strings"

// chunkSize/chunkOverlap bound how ingested text is split before being
// embedded. Character-based rather than token-based — no tokenizer
// dependency — but comfortably under every embedding API's ~8k-token
// input limit for any script. The overlap keeps a sentence that straddles
// a boundary from disappearing from both chunks' context.
const (
	chunkSize    = 1200
	chunkOverlap = 150
)

// splitText breaks text into overlapping windows of at most size runes,
// snapping each break to the nearest preceding whitespace so a chunk
// doesn't end mid-word. Deliberately simple — no sentence/paragraph-aware
// NLP — good enough to keep each chunk a coherent, embeddable unit.
func splitText(text string, size, overlap int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	if overlap >= size {
		overlap = 0
	}

	var chunks []string
	for start := 0; start < len(runes); {
		end := start + size
		if end >= len(runes) {
			end = len(runes)
		} else if snap := lastWhitespace(runes[start:end]); snap > size/2 {
			end = start + snap
		}

		if chunk := strings.TrimSpace(string(runes[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func lastWhitespace(rs []rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		switch rs[i] {
		case ' ', '\n', '\t', '\r':
			return i
		}
	}
	return -1
}
