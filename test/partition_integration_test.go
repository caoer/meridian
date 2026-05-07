package test

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/partition"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestPartition_DateConsistency runs the date-consistency check through the
// engine against a VFS wiki with date-partitioned folders.
func TestPartition_DateConsistency(t *testing.T) {
	fs := testkit.Wiki(
		// Correct date — path matches frontmatter.
		testkit.F("wiki/2026/05/05/correct.md", `---
tags: [type/note]
created: "2026-05-05"
---
# Correct date
`),
		// Mismatch — path says May 5, frontmatter says May 10.
		testkit.F("wiki/2026/05/05/mismatch.md", `---
tags: [type/note]
created: "2026-05-10"
---
# Date mismatch
`),
		// Non-partitioned — should not trigger.
		testkit.F("wiki/general/note.md", `---
tags: [type/note]
created: "2026-05-05"
---
# Not partitioned
`),
	)

	eng := engine.New()
	eng.RegisterCheck("partition-date-consistency", partition.CheckDateConsistency)

	rule := testkit.Rule("date-check",
		testkit.Check("partition-date-consistency"),
		testkit.Severity("error"),
		testkit.On("wiki/**"),
		testkit.Param("date_field", "created"),
		testkit.MessageTemplate("Date mismatch: path={{.PathDate}} fm={{.FmDate}}"),
	)

	findings := eng.Run(fs, []rules.Rule{rule})

	testkit.AssertFindings(t, findings,
		testkit.Finding("date-check", "wiki/2026/05/05/mismatch.md", "error"),
	)
	testkit.AssertNoFinding(t, findings, "date-check", "wiki/2026/05/05/correct.md")
	testkit.AssertNoFinding(t, findings, "date-check", "wiki/general/note.md")
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindingMessage(t, findings, "date-check", "wiki/2026/05/05/mismatch.md", "2026-05-05")
}

// TestPartition_Rollup checks that old files in daily folders produce rollup warnings.
func TestPartition_Rollup(t *testing.T) {
	fs := testkit.Wiki(
		// Old file in daily folder (created 60 days ago from reference time).
		testkit.F("wiki/2026/03/01/old-daily.md", `---
tags: [type/note]
created: "2026-03-01"
---
# Old daily note
`),
		// Recent file in daily folder (same day as reference time).
		testkit.F("wiki/2026/05/05/recent.md", `---
tags: [type/note]
created: "2026-05-05"
---
# Recent daily note
`),
		// Monthly folder — should not trigger rollup.
		testkit.F("wiki/2026/03/archive.md", `---
tags: [type/note]
created: "2026-03-01"
---
# Monthly archive
`),
	)

	eng := engine.New()
	eng.RegisterCheck("partition-rollup", partition.CheckRollup)

	rule := testkit.Rule("rollup-check",
		testkit.Check("partition-rollup"),
		testkit.Severity("warn"),
		testkit.On("wiki/**"),
		testkit.Param("date_field", "created"),
		testkit.Param("reference_time", "2026-05-05"),
		testkit.Param("daily_max_days", 7),
		testkit.Param("weekly_max_days", 30),
		testkit.MessageTemplate("Rollup: {{.Age}} old in {{.Current}}, suggest {{.Suggested}}"),
	)

	findings := eng.Run(fs, []rules.Rule{rule})

	testkit.AssertFindings(t, findings,
		testkit.Finding("rollup-check", "wiki/2026/03/01/old-daily.md", "warn"),
	)
	testkit.AssertNoFinding(t, findings, "rollup-check", "wiki/2026/05/05/recent.md")
	testkit.AssertNoFinding(t, findings, "rollup-check", "wiki/2026/03/archive.md")
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindingMessage(t, findings, "rollup-check", "wiki/2026/03/01/old-daily.md", "monthly")
}

// TestPartition_MultipleChecksRegistered verifies all 3 partition checks
// can be registered and run together without interference.
func TestPartition_MultipleChecksRegistered(t *testing.T) {
	fs := testkit.Wiki(
		// Mismatch date.
		testkit.F("wiki/2026/05/05/mismatch.md", `---
tags: [type/note]
created: "2026-05-10"
---
# Mismatch
`),
		// Old daily.
		testkit.F("wiki/2026/03/01/old.md", `---
tags: [type/note]
created: "2026-03-01"
---
# Old
`),
		// Good file.
		testkit.F("wiki/2026/05/05/good.md", `---
tags: [type/note]
created: "2026-05-05"
---
# Good
`),
	)

	eng := engine.New()
	eng.RegisterCheck("partition-date-consistency", partition.CheckDateConsistency)
	eng.RegisterCheck("partition-rollup", partition.CheckRollup)

	rr := []rules.Rule{
		testkit.Rule("date-check",
			testkit.Check("partition-date-consistency"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("date_field", "created"),
			testkit.MessageTemplate("Date: path={{.PathDate}} fm={{.FmDate}}"),
		),
		testkit.Rule("rollup-check",
			testkit.Check("partition-rollup"),
			testkit.Severity("warn"),
			testkit.On("wiki/**"),
			testkit.Param("date_field", "created"),
			testkit.Param("reference_time", "2026-05-05"),
			testkit.MessageTemplate("Rollup: {{.Age}} in {{.Current}}"),
		),
	}

	findings := eng.Run(fs, rr)

	// Date mismatch finding.
	testkit.AssertFindings(t, findings,
		testkit.Finding("date-check", "wiki/2026/05/05/mismatch.md", "error"),
	)
	// Rollup finding for old daily.
	testkit.AssertFindings(t, findings,
		testkit.Finding("rollup-check", "wiki/2026/03/01/old.md", "warn"),
	)
	// Good file should be clean for date-check.
	testkit.AssertNoFinding(t, findings, "date-check", "wiki/2026/05/05/good.md")
}
