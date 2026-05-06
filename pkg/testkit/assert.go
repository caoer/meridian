package testkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/types"
)

// ExpectedFinding describes an expected finding for assertion.
type ExpectedFinding struct {
	RuleID   string
	FilePath string
	Severity string
}

// Finding creates an expected finding.
func Finding(ruleID, filePath, severity string) ExpectedFinding {
	return ExpectedFinding{RuleID: ruleID, FilePath: filePath, Severity: severity}
}

// AssertFindings checks that each expected finding exists in actual.
func AssertFindings(t *testing.T, actual []types.Finding, expected ...ExpectedFinding) {
	t.Helper()
	for _, e := range expected {
		found := false
		for _, a := range actual {
			if a.RuleID == e.RuleID && a.FilePath == e.FilePath && a.Severity == e.Severity {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected finding: rule=%s file=%s severity=%s\nGot %d findings:\n%s",
				e.RuleID, e.FilePath, e.Severity, len(actual), formatFindings(actual))
		}
	}
}

// AssertNoFinding checks that NO finding exists for the given rule+file.
func AssertNoFinding(t *testing.T, actual []types.Finding, ruleID, filePath string) {
	t.Helper()
	for _, a := range actual {
		if a.RuleID == ruleID && a.FilePath == filePath {
			t.Errorf("Unexpected finding: rule=%s file=%s severity=%s msg=%q",
				a.RuleID, a.FilePath, a.Severity, a.Message)
		}
	}
}

// AssertClean checks that there are no findings.
func AssertClean(t *testing.T, actual []types.Finding) {
	t.Helper()
	if len(actual) > 0 {
		t.Errorf("Expected 0 findings, got %d:\n%s", len(actual), formatFindings(actual))
	}
}

// AssertFindingCount checks the total number of findings.
func AssertFindingCount(t *testing.T, actual []types.Finding, want int) {
	t.Helper()
	if len(actual) != want {
		t.Errorf("Expected %d findings, got %d:\n%s", want, len(actual), formatFindings(actual))
	}
}

// AssertFindingMessage checks that a finding for rule+file contains a message substring.
func AssertFindingMessage(t *testing.T, actual []types.Finding, ruleID, filePath, msgSubstr string) {
	t.Helper()
	for _, a := range actual {
		if a.RuleID == ruleID && a.FilePath == filePath {
			if !strings.Contains(a.Message, msgSubstr) {
				t.Errorf("Finding rule=%s file=%s: message %q does not contain %q",
					ruleID, filePath, a.Message, msgSubstr)
			}
			return
		}
	}
	t.Errorf("No finding for rule=%s file=%s to check message", ruleID, filePath)
}

func formatFindings(fs []types.Finding) string {
	if len(fs) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for i, f := range fs {
		fmt.Fprintf(&b, "  [%d] rule=%s file=%s severity=%s line=%d msg=%q\n",
			i, f.RuleID, f.FilePath, f.Severity, f.Line, f.Message)
	}
	return b.String()
}
