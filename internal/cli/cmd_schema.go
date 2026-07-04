package cli

// SchemaData is the response payload for `md schema`.
type SchemaData struct {
	Schema map[string]any `json:"schema"`
}
