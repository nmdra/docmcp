package search

import "sort"

const fusionK = 60

// RRFScore scores a one-based rank without mixing candidate score scales.
func RRFScore(rank int, k float64) float64 {
	return 1 / (k + float64(rank))
}

// RRF combines ranked candidate lists using k=60. Higher scores rank first,
// with chunk IDs breaking ties. Each ID contributes at most once per list.
// Chunk metadata comes from its first occurrence, normally the dense list.
func RRF(lists ...[]Result) []Result {
	candidates := make(map[string]Result)
	for _, list := range lists {
		seen := make(map[string]bool)
		for i, result := range list {
			id := result.Chunk.ID
			if seen[id] {
				continue
			}
			seen[id] = true
			candidate, ok := candidates[id]
			if !ok {
				candidate = Result{Chunk: result.Chunk}
			}
			candidate.Score += RRFScore(i+1, fusionK)
			candidates[id] = candidate
		}
	}
	out := make([]Result, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Chunk.ID < out[j].Chunk.ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}
