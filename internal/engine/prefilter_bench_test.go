package engine

// Microbenchmark isolating the parseInlineSuppress byte prefilter. Suppression
// directives are rare in real docs — nearly every line lacks both '<' and '%',
// so the gate skips all 6 directive regexes on it. Compare before/after by
// reverting scanner.go to the merge-base and re-running (see results receipt).

import (
	"strings"
	"testing"
)

func suppressBenchBody() string {
	var b strings.Builder
	prose := []string{
		"This is an ordinary prose line with no suppression directive present.",
		"Another sentence continuing the paragraph, again free of angle or percent.",
		"Wiki bodies are overwhelmingly prose; directives appear a handful of times.",
	}
	for section := 0; section < 60; section++ {
		for _, p := range prose {
			b.WriteString(p)
			b.WriteByte('\n')
		}
		// One real directive every few sections (carries '<' so the gate admits it).
		if section%5 == 0 {
			b.WriteString("<!-- md:ignore rule-a -->\n")
		}
	}
	return b.String()
}

func BenchmarkParseInlineSuppress_ProseHeavy(b *testing.B) {
	body := suppressBenchBody()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parseInlineSuppress(body, 1)
	}
}
