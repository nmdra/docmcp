package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/docmcp/docmcp/internal/store"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// lexicalCandidates uses standard BM25 over the scoped local corpus, with
// k1=1.2 and b=0.75. Query terms have equal weight. Term frequency saturates
// and document length is normalized against the scoped corpus average.
// There are no field weights, stoplists, query-specific rules, or fitted constants.
func lexicalCandidates(ctx context.Context, chunks []store.Chunk, query string, limit int) ([]Result, error) {
	terms, err := technicalTokens(ctx, query)
	if err != nil {
		return nil, err
	}
	orderedTerms := make([]string, 0, len(terms))
	for term := range terms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		orderedTerms = append(orderedTerms, term)
	}
	sort.Strings(orderedTerms)

	// Cache the document tokens for the scoring pass. Frequencies count chunks,
	// not occurrences, and are rebuilt from this library/version on each query.
	documents := make([]map[string]int, len(chunks))
	lengths := make([]int, len(chunks))
	totalLength := 0
	frequencies := make(map[string]int, len(terms))
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		documents[i], err = technicalTokens(ctx, chunk.Title+"\n"+chunk.HeadingPath+"\n"+chunk.Content)
		if err != nil {
			return nil, err
		}
		for _, count := range documents[i] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			lengths[i] += count
		}
		totalLength += lengths[i]
		for _, term := range orderedTerms {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if documents[i][term] > 0 {
				frequencies[term]++
			}
		}
	}
	if totalLength == 0 {
		return nil, ctx.Err()
	}
	averageLength := float64(totalLength) / float64(len(chunks))
	weights := make(map[string]float64, len(terms))
	for _, term := range orderedTerms {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if frequency := frequencies[term]; frequency > 0 {
			weights[term] = math.Log(1 + (float64(len(chunks)-frequency)+0.5)/(float64(frequency)+0.5))
		}
	}

	var ranked []Result
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score := 0.0
		for _, term := range orderedTerms {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if frequency := documents[i][term]; frequency > 0 {
				tf := float64(frequency)
				normalization := bm25K1 * (1 - bm25B + bm25B*float64(lengths[i])/averageLength)
				score += weights[term] * tf * (bm25K1 + 1) / (tf + normalization)
			}
		}
		if score > 0 {
			ranked = append(ranked, Result{Chunk: chunk, Score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Chunk.ID < ranked[j].Chunk.ID
		}
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ranked, nil
}

// technicalTokens counts compound identifiers and their component terms.
// Each analyzer term counts once per identifier occurrence. Document length
// is the sum of these term counts, including compound/component expansions.
// Sentence punctuation at the edge of an identifier is not part of the token.
func technicalTokens(ctx context.Context, text string) (map[string]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokens := make(map[string]int)
	// Scan with cancellation checks so a large individual chunk does not delay
	// cancellation until the whole corpus tokenization pass has finished.
	var identifiers []string
	start := -1
	for i, r := range text {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		separator := !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-/", r)
		if separator {
			if start >= 0 {
				identifiers = append(identifiers, text[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		identifiers = append(identifiers, text[start:])
	}
	for _, identifier := range identifiers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identifier = strings.Trim(identifier, "._-")
		if identifier == "" {
			continue
		}
		// Count each analyzer term once per identifier occurrence, rather than
		// counting the full-token and component expansions of a plain word twice.
		emitted := map[string]bool{strings.ToLower(identifier): true}
		for _, part := range strings.FieldsFunc(identifier, func(r rune) bool { return strings.ContainsRune("._-/", r) }) {
			emitted[strings.ToLower(part)] = true
			runes := []rune(part)
			start := 0
			for i := 1; i < len(runes); i++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				// Split lower-to-upper transitions and acronym-to-word edges.
				if unicode.IsUpper(runes[i]) && (unicode.IsLower(runes[i-1]) ||
					(i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1]))) {
					emitted[strings.ToLower(string(runes[start:i]))] = true
					start = i
				}
			}
			emitted[strings.ToLower(string(runes[start:]))] = true
		}
		for term := range emitted {
			tokens[term]++
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}

// hasTechnicalIdentifier routes explicit identifier syntax to hybrid search.
// It recognizes slash commands, internal dots/hyphens, camel case, and all-caps
// tokens in otherwise normally cased queries. Plain words and fully uppercase
// phrases stay dense-only. All-caps emphasis can also select hybrid retrieval.
func hasTechnicalIdentifier(query string) bool {
	identifiers := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-/", r)
	})
	isAlphanumeric := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	hasLowercase := false
	for _, r := range query {
		if unicode.IsLower(r) {
			hasLowercase = true
			break
		}
	}
	for _, identifier := range identifiers {
		runes := []rune(identifier)
		uppercaseLetters := 0
		allLettersUppercase := true
		for _, r := range runes {
			if unicode.IsLetter(r) {
				uppercaseLetters++
				if !unicode.IsUpper(r) {
					allLettersUppercase = false
				}
			}
		}
		if hasLowercase && allLettersUppercase && uppercaseLetters >= 2 {
			return true
		}
		for i, r := range runes {
			if r == '/' && i+1 < len(runes) && isAlphanumeric(runes[i+1]) {
				return true
			}
			if (r == '.' || r == '-') && i > 0 && i+1 < len(runes) && isAlphanumeric(runes[i-1]) && isAlphanumeric(runes[i+1]) {
				return true
			}
			if i > 0 && unicode.IsLower(runes[i-1]) && unicode.IsUpper(r) {
				return true
			}
		}
	}
	return false
}
