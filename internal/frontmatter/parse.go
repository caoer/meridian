package frontmatter

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// bom is the UTF-8 byte order mark.
var bom = []byte{0xEF, 0xBB, 0xBF}

// ParseReader extracts YAML frontmatter from a reader.
// Returns nil, nil if the content has no frontmatter (no leading ---).
// Returns nil, nil for empty input.
// Returns nil, error for unclosed frontmatter or invalid YAML.
func ParseReader(r io.Reader) (*Doc, error) {
	scanner := bufio.NewScanner(r)

	// First line must be ---
	if !scanner.Scan() {
		return nil, nil // empty
	}

	firstLine := scanner.Bytes()
	firstLine = bytes.TrimPrefix(firstLine, bom)

	if string(bytes.TrimSpace(firstLine)) != "---" {
		return nil, nil // no frontmatter
	}

	var yamlBuf bytes.Buffer
	lineNum := 1 // already read line 1 (opening ---)
	closed := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		yamlBuf.WriteString(line)
		yamlBuf.WriteByte('\n')
	}

	if !closed {
		return nil, fmt.Errorf("unclosed frontmatter (no closing ---)")
	}

	// Parse YAML
	meta := make(map[string]any)
	rawYAML := yamlBuf.String()
	if strings.TrimSpace(rawYAML) != "" {
		if err := yaml.Unmarshal([]byte(rawYAML), &meta); err != nil {
			return nil, fmt.Errorf("invalid YAML in frontmatter: %w", err)
		}
	}

	// Read remaining body
	var bodyBuf bytes.Buffer
	for scanner.Scan() {
		bodyBuf.WriteString(scanner.Text())
		bodyBuf.WriteByte('\n')
	}

	return &Doc{
		RawYAML:    rawYAML,
		Meta:       meta,
		Body:       bodyBuf.String(),
		BodyOffset: lineNum + 1, // body starts after closing ---
	}, nil
}

// ParseBytes is a convenience wrapper.
func ParseBytes(data []byte) (*Doc, error) {
	return ParseReader(bytes.NewReader(data))
}
