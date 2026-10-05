package adapters

import (
	"github.com/ai-process/llm-proxy/internal/llm"
	"testing"
)

func TestBuildGenerateConfigSendsOutputCeiling(t *testing.T) {
	chat := llm.NewChatContext()
	chat.AddSystemMessage("be brief")
	chat.AddUserMessage("go")
	chat.SetMaxOutputTokens(llm.MaxTokensTheory)

	config := (&GoogleAdapter{}).buildGenerateConfig(chat)
	if config.MaxOutputTokens != int32(llm.MaxTokensTheory) {
		t.Fatalf("ceiling must reach the vendor config: want %d, got %d", llm.MaxTokensTheory, config.MaxOutputTokens)
	}
}

// An unset ceiling must stay absent rather than become an explicit 0, which the
// vendor would read as "generate nothing".
func TestBuildGenerateConfigOmitsUnsetCeiling(t *testing.T) {
	chat := llm.NewChatContext()
	chat.AddUserMessage("go")

	config := (&GoogleAdapter{}).buildGenerateConfig(chat)
	if config.MaxOutputTokens != 0 {
		t.Fatalf("want no ceiling field set, got %d", config.MaxOutputTokens)
	}
}

func TestBuildGenerateConfigCarriesSystemInstruction(t *testing.T) {
	chat := llm.NewChatContext()
	chat.AddSystemMessage("you are a linguist")

	config := (&GoogleAdapter{}).buildGenerateConfig(chat)
	if config.SystemInstruction == nil || len(config.SystemInstruction.Parts) == 0 {
		t.Fatal("system instruction must be attached to the config")
	}
	if got := config.SystemInstruction.Parts[0].Text; got != "you are a linguist" {
		t.Fatalf("want the system text verbatim, got %q", got)
	}
}

func TestBuildGenerateConfigSetsJSONModeWithSchema(t *testing.T) {
	chat := llm.NewChatContext()
	chat.AddUserMessage("go")
	chat.SetResponseSchema(&llm.ResponseSchema{
		Type: llm.SchemaPropertyTypeObject,
		Properties: map[string]*llm.SchemaProperty{
			"word": {Type: llm.SchemaPropertyTypeString},
		},
	})

	config := (&GoogleAdapter{}).buildGenerateConfig(chat)
	if config.ResponseSchema == nil {
		t.Fatal("schema must be forwarded to the vendor config")
	}
	if config.ResponseMIMEType != "application/json" {
		t.Fatalf("a response schema requires JSON mime type, got %q", config.ResponseMIMEType)
	}
}
