package llm

import (
	"errors"
	"strings"
	"testing"
)

// exercisesSchema mirrors a nested shape callers ask for, which is what broke
// against DeepSeek: an object holding an array of objects.
func exercisesSchema() *ResponseSchema {
	return &ResponseSchema{
		Type: SchemaPropertyTypeObject,
		Properties: map[string]*SchemaProperty{
			"items": {
				Type: SchemaPropertyTypeArray,
				Items: &SchemaProperty{
					Type: SchemaPropertyTypeObject,
					Properties: map[string]*SchemaProperty{
						"t":  {Type: SchemaPropertyTypeString},
						"p":  {Type: SchemaPropertyTypeInteger},
						"ma": {Type: SchemaPropertyTypeInteger},
						"q": {Type: SchemaPropertyTypeArray, Items: &SchemaProperty{
							Type:       SchemaPropertyTypeObject,
							Properties: map[string]*SchemaProperty{"t": {Type: SchemaPropertyTypeString}},
							Required:   []string{"t"},
						}},
					},
					Required: []string{"t", "p", "q"},
				},
			},
		},
		Required: []string{"items"},
	}
}

func TestCheckResponseSchemaAccepts(t *testing.T) {
	ok := `{"items":[{"t":"Title","p":1,"ma":5,"q":[{"t":"question?"}]}]}`
	if err := CheckResponseSchema(ok, exercisesSchema()); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	// Models wrap JSON in fences when nobody enforces a format.
	fenced := "```json\n" + ok + "\n```"
	if err := CheckResponseSchema(fenced, exercisesSchema()); err != nil {
		t.Fatalf("fenced document rejected: %v", err)
	}
	// A nil schema means the caller wanted free text.
	if err := CheckResponseSchema("just prose", nil); err != nil {
		t.Fatalf("nil schema rejected: %v", err)
	}
	// Extra keys are what the native modes tolerate, so we do too.
	if err := CheckResponseSchema(`{"items":[{"t":"T","p":1,"q":[],"extra":true}]}`, exercisesSchema()); err != nil {
		t.Fatalf("extra key rejected: %v", err)
	}
}

func TestCheckResponseSchemaRejects(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"prose instead of json": {"Here are your exercises!", "not valid JSON"},
		"empty":                 {"   ", "empty response"},
		"missing required key":  {`{"items":[{"p":1,"q":[]}]}`, "items[0].t is missing"},
		"wrong scalar type":     {`{"items":[{"t":"T","p":"first","q":[]}]}`, "items[0].p should be an integer"},
		"fractional integer":    {`{"items":[{"t":"T","p":1.5,"q":[]}]}`, "should be an integer"},
		"array where object":    {`{"items":[["nope"]]}`, "should be an object"},
		"object where array":    {`{"items":{"t":"T"}}`, "items should be an array"},
		"nested required":       {`{"items":[{"t":"T","p":1,"q":[{}]}]}`, "items[0].q[0].t is missing"},
		"top-level missing":     {`{"other":[]}`, "$.items is missing"},
		"null non-nullable":     {`{"items":[{"t":null,"p":1,"q":[]}]}`, "items[0].t is null"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckResponseSchema(tc.body, exercisesSchema())
			if err == nil {
				t.Fatal("invalid document accepted")
			}
			if !errors.Is(err, ErrSchemaViolation) {
				t.Fatalf("err = %v, want ErrSchemaViolation", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCheckResponseSchemaAllowsNullWhenNullable(t *testing.T) {
	schema := &ResponseSchema{
		Type:       SchemaPropertyTypeObject,
		Properties: map[string]*SchemaProperty{"note": {Type: SchemaPropertyTypeString, Nullable: true}},
		Required:   []string{"note"},
	}
	if err := CheckResponseSchema(`{"note":null}`, schema); err != nil {
		t.Fatalf("nullable field rejected: %v", err)
	}
}

func TestSchemaPromptInstructionDescribesTheShape(t *testing.T) {
	got := SchemaPromptInstruction(exercisesSchema())
	for _, want := range []string{"JSON", "items", "properties"} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction is missing %q: %s", want, got)
		}
	}
	if SchemaPromptInstruction(nil) != "" {
		t.Error("nil schema should produce no instruction")
	}
}
