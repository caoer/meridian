package domains

import (
	"sort"
	"strings"
)

// Domain represents a tag namespace (e.g., "domain/locus", "type/source").
type Domain struct {
	Prefix string         // e.g., "domain"
	Values []string       // sorted unique values
	Count  map[string]int // value -> page count
}

// Registry holds discovered domains from a wiki scan.
type Registry struct {
	domains   map[string]*Domain
	pageCount int
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		domains: make(map[string]*Domain),
	}
}

// PageCount returns the number of Add() calls (pages registered).
func (r *Registry) PageCount() int {
	return r.pageCount
}

// Add extracts prefix/value pairs from tags and updates counts.
// Tags without a "/" separator are ignored.
func (r *Registry) Add(tags []string) {
	r.pageCount++
	for _, tag := range tags {
		idx := strings.IndexByte(tag, '/')
		if idx < 0 {
			continue
		}
		prefix := tag[:idx]
		value := tag[idx+1:]
		if prefix == "" || value == "" {
			continue
		}

		d, ok := r.domains[prefix]
		if !ok {
			d = &Domain{
				Prefix: prefix,
				Count:  make(map[string]int),
			}
			r.domains[prefix] = d
		}

		if d.Count[value] == 0 {
			d.Values = append(d.Values, value)
			sort.Strings(d.Values)
		}
		d.Count[value]++
	}
}

// Tree returns all domains keyed by prefix.
func (r *Registry) Tree() map[string]*Domain {
	return r.domains
}

// Get returns a single domain by prefix, or nil if not found.
func (r *Registry) Get(prefix string) *Domain {
	return r.domains[prefix]
}

// Prefixes returns sorted prefix names.
func (r *Registry) Prefixes() []string {
	names := make([]string, 0, len(r.domains))
	for name := range r.domains {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
