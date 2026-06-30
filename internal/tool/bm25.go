package tool

import (
	"math"
	"strings"
	"unicode"
)

// BM25Index is a BM25 ranking index over a set of documents.
type BM25Index struct {
	docs  []bm25Doc
	avgDL float64            // average document length
	idf   map[string]float64 // term -> inverse document frequency
	k1    float64            // term frequency saturation (default 1.5)
	b     float64            // document length normalization (default 0.75)
}

type bm25Doc struct {
	ID        string
	Tokens    []string
	TokenFreq map[string]int // term -> count in this doc
	Length    int            // total tokens
}

// BM25Doc is a document to index.
type BM25Doc struct {
	ID   string
	Text string // full text to tokenize and index
}

// BM25Result is a ranked search result.
type BM25Result struct {
	ID    string
	Score float64
}

// NewBM25Index builds a BM25 index over the given documents.
func NewBM25Index(docs []BM25Doc) *BM25Index {
	if len(docs) == 0 {
		return &BM25Index{
			idf: make(map[string]float64),
			k1:  1.5,
			b:   0.75,
		}
	}

	internal := make([]bm25Doc, len(docs))
	totalLen := 0
	df := make(map[string]int) // document frequency: term -> number of docs containing it

	for i, d := range docs {
		tokens := tokenize(d.Text)
		tf := make(map[string]int, len(tokens))
		for _, t := range tokens {
			tf[t]++
		}
		internal[i] = bm25Doc{
			ID:        d.ID,
			Tokens:    tokens,
			TokenFreq: tf,
			Length:    len(tokens),
		}
		totalLen += len(tokens)
		// Update document frequency: count each term once per document.
		for term := range tf {
			df[term]++
		}
	}

	avgDL := float64(totalLen) / float64(len(docs))

	// Compute IDF for each term.
	N := float64(len(docs))
	idf := make(map[string]float64, len(df))
	for term, nq := range df {
		idf[term] = math.Log((N-float64(nq)+0.5)/(float64(nq)+0.5) + 1)
	}

	return &BM25Index{
		docs:  internal,
		avgDL: avgDL,
		idf:   idf,
		k1:    1.5,
		b:     0.75,
	}
}

// Search returns the top-N documents matching the query, sorted by BM25 score descending.
func (idx *BM25Index) Search(query string, n int) []BM25Result {
	if len(idx.docs) == 0 || n <= 0 {
		return nil
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Unique query terms to avoid double-counting.
	seen := make(map[string]bool, len(queryTerms))
	unique := make([]string, 0, len(queryTerms))
	for _, t := range queryTerms {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}

	results := make([]BM25Result, 0, len(idx.docs))
	for _, doc := range idx.docs {
		score := 0.0
		for _, qt := range unique {
			idfVal, ok := idx.idf[qt]
			if !ok {
				continue
			}
			freq := doc.TokenFreq[qt]
			if freq == 0 {
				continue
			}
			dl := float64(doc.Length)
			numerator := float64(freq) * (idx.k1 + 1)
			denominator := float64(freq) + idx.k1*(1-idx.b+idx.b*dl/idx.avgDL)
			score += idfVal * numerator / denominator
		}
		if score > 0 {
			results = append(results, BM25Result{
				ID:    doc.ID,
				Score: score,
			})
		}
	}

	// Sort by score descending.
	sortResults(results)

	if n > len(results) {
		n = len(results)
	}
	return results[:n]
}

// sortResults sorts BM25 results by score descending using insertion sort.
func sortResults(r []BM25Result) {
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && r[j].Score > r[j-1].Score; j-- {
			r[j], r[j-1] = r[j-1], r[j]
		}
	}
}

// stopWords are common English words that add noise to BM25 ranking.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "to": true,
	"and": true, "or": true, "in": true, "on": true, "at": true,
	"for": true, "of": true, "with": true, "by": true, "it": true,
	"as": true, "be": true, "was": true, "are": true, "been": true,
	"has": true, "have": true, "had": true, "this": true, "that": true,
	"from": true, "but": true, "not": true, "its": true, "can": true,
	"will": true, "do": true, "does": true, "did": true, "no": true,
	"if": true, "so": true, "up": true, "out": true, "about": true,
	"into": true, "than": true, "then": true, "there": true, "also": true,
}

// tokenize splits text into lowercase terms for indexing.
// It splits on whitespace and punctuation, removes empty tokens,
// and filters out stop words.
func tokenize(text string) []string {
	var tokens []string
	var buf strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			buf.WriteRune(unicode.ToLower(r))
		} else {
			if buf.Len() > 0 {
				w := buf.String()
				if !stopWords[w] {
					tokens = append(tokens, w)
				}
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		w := buf.String()
		if !stopWords[w] {
			tokens = append(tokens, w)
		}
	}

	return tokens
}
