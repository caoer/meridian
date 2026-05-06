package rules

import (
	"testing"
)

// --- Severity ---

func TestSeverity_ParseValid(t *testing.T) {
	cases := []struct {
		input string
		want  Severity
	}{
		{"error", SeverityError},
		{"warn", SeverityWarn},
		{"info", SeverityInfo},
		{"off", SeverityOff},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSeverity(tc.input)
			if err != nil {
				t.Fatalf("ParseSeverity(%q) error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseSeverity(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSeverity_ParseInvalid(t *testing.T) {
	_, err := ParseSeverity("critical")
	if err == nil {
		t.Fatal("ParseSeverity(\"critical\") should error")
	}
}

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarn, "warn"},
		{SeverityInfo, "info"},
		{SeverityOff, "off"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.sev.String(); got != tc.want {
				t.Errorf("Severity.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- On filter parsing ---

func TestParseOnFilter_SingleString(t *testing.T) {
	f := ParseOnFilter([]string{"wiki/**"})

	if len(f.Paths) != 1 || f.Paths[0] != "wiki/**" {
		t.Errorf("Paths = %v, want [wiki/**]", f.Paths)
	}
	if len(f.Tags) != 0 {
		t.Errorf("Tags = %v, want empty", f.Tags)
	}
	if len(f.Excludes) != 0 {
		t.Errorf("Excludes = %v, want empty", f.Excludes)
	}
}

func TestParseOnFilter_MixedArray(t *testing.T) {
	f := ParseOnFilter([]string{
		"wiki/sources/**",
		"#type/source",
		"!wiki/_archive/**",
		"!#status/archived",
	})

	if len(f.Paths) != 1 || f.Paths[0] != "wiki/sources/**" {
		t.Errorf("Paths = %v, want [wiki/sources/**]", f.Paths)
	}
	if len(f.Tags) != 1 || f.Tags[0] != "type/source" {
		t.Errorf("Tags = %v, want [type/source]", f.Tags)
	}
	if len(f.Excludes) != 2 {
		t.Fatalf("Excludes = %v, want 2 items", f.Excludes)
	}
	// First exclude: path
	if f.Excludes[0].Path != "wiki/_archive/**" || f.Excludes[0].Tag != "" {
		t.Errorf("Excludes[0] = %+v, want path=wiki/_archive/**", f.Excludes[0])
	}
	// Second exclude: tag
	if f.Excludes[1].Tag != "status/archived" || f.Excludes[1].Path != "" {
		t.Errorf("Excludes[1] = %+v, want tag=status/archived", f.Excludes[1])
	}
}

func TestParseOnFilter_TagOnly(t *testing.T) {
	f := ParseOnFilter([]string{"#type/reference"})

	if len(f.Paths) != 0 {
		t.Errorf("Paths = %v, want empty", f.Paths)
	}
	if len(f.Tags) != 1 || f.Tags[0] != "type/reference" {
		t.Errorf("Tags = %v, want [type/reference]", f.Tags)
	}
}

func TestParseOnFilter_MultiplePathsAND(t *testing.T) {
	f := ParseOnFilter([]string{"wiki/**", "*.md"})

	if len(f.Paths) != 2 {
		t.Fatalf("Paths = %v, want 2 items", f.Paths)
	}
	if f.Paths[0] != "wiki/**" || f.Paths[1] != "*.md" {
		t.Errorf("Paths = %v, want [wiki/** *.md]", f.Paths)
	}
}

func TestParseOnFilter_MultipleTagsAND(t *testing.T) {
	f := ParseOnFilter([]string{"#type/source", "#domain/locus"})

	if len(f.Tags) != 2 {
		t.Fatalf("Tags = %v, want 2 items", f.Tags)
	}
}

// --- Rule struct basic ---

func TestRule_FieldsAccessible(t *testing.T) {
	r := Rule{
		ID:       "required-fields",
		Check:    "field-exists",
		Message:  "Missing required field: {{.Field}}",
		Severity: SeverityError,
		On:       OnFilter{Paths: []string{"wiki/**"}},
		Params:   map[string]any{"frontmatter": []string{"tags", "created"}},
	}

	if r.ID != "required-fields" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Check != "field-exists" {
		t.Errorf("Check = %q", r.Check)
	}
	if r.Severity != SeverityError {
		t.Errorf("Severity = %v", r.Severity)
	}
}
