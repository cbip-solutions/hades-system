// SPDX-License-Identifier: MIT
package mapping

import (
	"sort"
	"strings"
	"unicode"
)

func extractKeywords(body []byte, n int) []string {
	tokens := tokenize(string(body))
	if len(tokens) == 0 {
		return nil
	}
	tf := map[string]int{}
	for _, t := range tokens {
		tf[t]++
	}
	type scored struct {
		term  string
		score float64
	}
	scoredList := make([]scored, 0, len(tf))
	for term, count := range tf {
		idfApprox := 1.0 / float64(1+len(term))
		scoredList = append(scoredList, scored{
			term:  term,
			score: float64(count) * (1.0 - idfApprox),
		})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score != scoredList[j].score {
			return scoredList[i].score > scoredList[j].score
		}

		return scoredList[i].term < scoredList[j].term
	})
	if n > len(scoredList) {
		n = len(scoredList)
	}
	out := make([]string, 0, n)
	for _, s := range scoredList[:n] {
		out = append(out, s.term)
	}
	return out
}

func tokenize(s string) []string {
	out := []string{}
	current := strings.Builder{}
	flush := func() {
		w := strings.ToLower(current.String())
		current.Reset()
		if len(w) < 3 {
			return
		}
		if stopWords[w] {
			return
		}
		out = append(out, w)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

var stopWords = map[string]bool{

	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true,
	"are": true, "you": true, "from": true, "your": true, "have": true, "will": true,
	"can": true, "but": true, "use": true, "all": true, "not": true, "any": true,
	"how": true, "what": true, "when": true, "where": true, "which": true, "who": true,
	"its": true, "was": true, "were": true, "been": true, "their": true, "they": true,

	"las": true, "los": true, "una": true, "uno": true, "que": true, "del": true,
	"por": true, "para": true, "con": true, "sin": true, "como": true, "esto": true,
	"esta": true, "este": true, "estos": true, "estas": true, "pero": true, "sino": true,
}
