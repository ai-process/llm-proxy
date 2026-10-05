package llm

import "sort"

type SchemaPropertyType string

const (
	SchemaPropertyTypeString  SchemaPropertyType = "string"
	SchemaPropertyTypeInteger SchemaPropertyType = "integer"
	SchemaPropertyTypeBoolean SchemaPropertyType = "boolean"
	SchemaPropertyTypeObject  SchemaPropertyType = "object"
	SchemaPropertyTypeArray   SchemaPropertyType = "array"
)

type SchemaProperty struct {
	Type        SchemaPropertyType         `json:"type"`
	Description string                     `json:"description,omitempty"`
	Properties  map[string]*SchemaProperty `json:"properties,omitempty"` // For object types
	Items       *SchemaProperty            `json:"items,omitempty"`      // For array types
	Required    []string                   `json:"required,omitempty"`   // Required fields for object types
	Nullable    bool                       `json:"nullable,omitempty"`   // If true, allows null values (type becomes ["type", "null"] in JSON Schema)
}

type ResponseSchema struct {
	Type       SchemaPropertyType         `json:"type"`
	Properties map[string]*SchemaProperty `json:"properties"`
	Items      *SchemaProperty            `json:"items,omitempty"`
	Required   []string                   `json:"required,omitempty"`
}

// EnforceAllRequired mutates the schema so that every object-level field is required.
// This is applied recursively to nested objects (including array items).
func EnforceAllRequired(schema *ResponseSchema) *ResponseSchema {
	if schema == nil {
		return nil
	}

	enforceAllRequiredOnProperty(&SchemaProperty{
		Type:       schema.Type,
		Properties: schema.Properties,
		Items:      schema.Items,
		Required:   schema.Required,
	})

	// Copy back the computed required list for the top-level object.
	if schema.Type == SchemaPropertyTypeObject && len(schema.Properties) > 0 {
		schema.Required = allKeys(schema.Properties)
	}

	return schema
}

func enforceAllRequiredOnProperty(p *SchemaProperty) {
	if p == nil {
		return
	}

	if p.Type == SchemaPropertyTypeObject && len(p.Properties) > 0 {
		p.Required = allKeys(p.Properties)
		for _, child := range p.Properties {
			enforceAllRequiredOnProperty(child)
		}
	}

	if p.Type == SchemaPropertyTypeArray && p.Items != nil {
		enforceAllRequiredOnProperty(p.Items)
	}
}

func allKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Deterministic order helps with tests/logging and avoids needless diffs.
	sort.Strings(keys)
	return keys
}
