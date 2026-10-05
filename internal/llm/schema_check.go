package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrSchemaViolation marks a reply that parsed as JSON but does not match the
// requested schema. Separate from a parse failure so callers can tell "not
// JSON at all" from "JSON of the wrong shape" in logs.
var ErrSchemaViolation = errors.New("response does not match the requested schema")

// maxSchemaIssues bounds the reported mismatches: the message goes into a log
// line and a retry instruction, and the first few are enough to act on.
const maxSchemaIssues = 5

// CheckResponseSchema validates a model's raw reply against the schema the
// caller asked for. It exists for vendors whose API cannot enforce a schema
// (DeepSeek among them): the proxy asks for JSON in the prompt, then verifies
// the shape here rather than handing an unchecked document to the caller.
//
// Only the constraints the schema can express are checked — types, required
// keys and array element shape. Descriptions and unknown extra keys are
// ignored, matching what the native structured-output modes accept.
func CheckResponseSchema(raw string, schema *ResponseSchema) error {
	if schema == nil {
		return nil
	}
	text := strings.TrimSpace(RemoveMarkdownCodeBlocks(raw))
	if text == "" {
		return fmt.Errorf("%w: empty response", ErrSchemaViolation)
	}

	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return fmt.Errorf("%w: not valid JSON: %v", ErrSchemaViolation, err)
	}

	issues := checkValue(doc, &SchemaProperty{
		Type:       schema.Type,
		Properties: schema.Properties,
		Items:      schema.Items,
		Required:   schema.Required,
	}, "$", nil)
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrSchemaViolation, strings.Join(issues, "; "))
}

// checkValue walks the document and the schema together, collecting readable
// paths for whatever does not line up.
func checkValue(value any, want *SchemaProperty, path string, issues []string) []string {
	if want == nil || len(issues) >= maxSchemaIssues {
		return issues
	}
	if value == nil {
		if !want.Nullable {
			issues = append(issues, path+" is null")
		}
		return issues
	}

	switch want.Type {
	case SchemaPropertyTypeObject:
		obj, ok := value.(map[string]any)
		if !ok {
			return append(issues, fmt.Sprintf("%s should be an object, got %s", path, jsonKind(value)))
		}
		for _, key := range want.Required {
			if _, present := obj[key]; !present {
				issues = append(issues, fmt.Sprintf("%s.%s is missing", path, key))
				if len(issues) >= maxSchemaIssues {
					return issues
				}
			}
		}
		for key, child := range want.Properties {
			if v, present := obj[key]; present {
				issues = checkValue(v, child, path+"."+key, issues)
				if len(issues) >= maxSchemaIssues {
					return issues
				}
			}
		}
	case SchemaPropertyTypeArray:
		arr, ok := value.([]any)
		if !ok {
			return append(issues, fmt.Sprintf("%s should be an array, got %s", path, jsonKind(value)))
		}
		for i, item := range arr {
			issues = checkValue(item, want.Items, fmt.Sprintf("%s[%d]", path, i), issues)
			if len(issues) >= maxSchemaIssues {
				return issues
			}
		}
	case SchemaPropertyTypeString:
		if _, ok := value.(string); !ok {
			issues = append(issues, fmt.Sprintf("%s should be a string, got %s", path, jsonKind(value)))
		}
	case SchemaPropertyTypeInteger:
		// Every JSON number decodes to float64; integers are whole numbers.
		n, ok := value.(float64)
		if !ok {
			issues = append(issues, fmt.Sprintf("%s should be an integer, got %s", path, jsonKind(value)))
		} else if n != float64(int64(n)) {
			issues = append(issues, fmt.Sprintf("%s should be an integer, got %v", path, n))
		}
	case SchemaPropertyTypeBoolean:
		if _, ok := value.(bool); !ok {
			issues = append(issues, fmt.Sprintf("%s should be a boolean, got %s", path, jsonKind(value)))
		}
	}
	return issues
}

func jsonKind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	default:
		return "an unknown value"
	}
}

// SchemaPromptInstruction describes the wanted shape in words, for models that
// cannot be handed a schema through the API. Appended to the system
// instruction so the reply is JSON in the first place.
func SchemaPromptInstruction(schema *ResponseSchema) string {
	if schema == nil {
		return ""
	}
	shape, err := json.Marshal(schema)
	if err != nil {
		return "Reply with a single JSON document and nothing else."
	}
	return "Reply with a single JSON document and nothing else: no prose, no markdown fences. " +
		"It must match this JSON Schema:\n" + string(shape)
}
