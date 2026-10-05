package adapters

import (
	"context"
	"fmt"
	"github.com/ai-process/llm-proxy/internal/audio"
	"github.com/ai-process/llm-proxy/internal/llm"
	"math"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"google.golang.org/genai"
)

// ttsOutputSampleRate is what Gemini TTS produces (16-bit LE mono PCM) when the
// response MIME type omits an explicit rate.
const ttsOutputSampleRate = 24000

type GoogleAdapter struct {
	geminiClient *genai.Client
	throttle     *llm.ThrottleControl
	model        string
	ttsModel     string
	ttsVoice     string
	imageModel   string
}

func NewGoogleAdapter(geminiClient *genai.Client, model string, throttle *llm.ThrottleControl) *GoogleAdapter {
	return &GoogleAdapter{
		geminiClient: geminiClient,
		model:        model,
		throttle:     throttle,
		// Nano Banana 2 Lite: newest and cheapest of the flash image line.
	}
}

func (a *GoogleAdapter) GenerateText(chat *llm.ChatContext) (*llm.Response, error) {
	ctx := context.Background()

	// Built once, not per attempt: buildSystemInstruction consumes the chat's
	// per-request instruction, so rebuilding would silently drop it on the retry.
	config := a.buildGenerateConfig(chat)

	contents := a.contextToContents(chat.GetMessages())
	if len(contents) == 0 {
		return nil, fmt.Errorf("GoogleAdapter: no non-empty messages, cannot generate text")
	}

	result, finishReason, err := a.generateOnce(ctx, contents, config)
	if err != nil {
		return nil, err
	}

	// Thinking tokens are charged against MaxOutputTokens, and thinking is on by
	// default for every model here, so a ceiling can be spent entirely on thinking
	// and come back with nothing. Lift it and retry rather than fail a job that
	// would have succeeded uncapped — this log line is the signal to raise the tier.
	if finishReason == genai.FinishReasonMaxTokens && config.MaxOutputTokens > 0 {
		log.Warn().Str("model", a.model).Int32("ceiling", config.MaxOutputTokens).
			Msg("GoogleAdapter: output ceiling exhausted, retrying uncapped")
		config.MaxOutputTokens = 0
		result, finishReason, err = a.generateOnce(ctx, contents, config)
		if err != nil {
			return nil, err
		}
	}

	response := &llm.Response{}
	text := result.Text()

	// Truncated even without a ceiling: the model's own limit was reached, so the
	// payload is half-written and every structured caller here unmarshals JSON.
	if finishReason == genai.FinishReasonMaxTokens {
		return nil, llm.TruncatedError("GoogleAdapter", a.model, len(text))
	}
	if text == "" {
		return nil, fmt.Errorf("GoogleAdapter: empty response (finish reason: %s)", finishReason)
	}

	//trim "```json" at the beginning and "```" at the end
	if len(text) >= 10 && text[:7] == "```json" {
		text = text[7:]
		if text[len(text)-3:] == "```" {
			text = text[:len(text)-3]
		}
	}

	response.AddMessage(&llm.Message{
		Type: llm.MessageTypeBot,
		Text: text,
	})
	//log.Info().Interface("usage", result.UsageMetadata).Msg("GoogleAdapter: Usage info")
	response.Usage.Input = int(result.UsageMetadata.PromptTokenCount)
	response.Usage.Output = int(result.UsageMetadata.TotalTokenCount - result.UsageMetadata.PromptTokenCount)
	return response, nil
}

// contextToContents maps the whole conversation to genai contents, keeping roles:
// user messages stay user turns, assistant messages stay model turns. A trailing
// model turn is valid for Gemini — generation continues from it, so flows that
// end the context with an assistant message (e.g. answer generation, where the
// exercise question is a bot turn) keep their intended semantics.
// System messages are excluded: they go through buildSystemInstruction.
func (a *GoogleAdapter) contextToContents(messages []*llm.Message) []*genai.Content {
	var contents []*genai.Content
	for i, message := range messages {
		if message.Text == "" {
			log.Printf("GoogleAdapter: Message %d is empty, skipping.\n", i)
			continue
		}

		switch message.Type {
		case llm.MessageTypeUser:
			contents = append(contents, genai.NewContentFromText(message.Text, genai.RoleUser))
		case llm.MessageTypeBot:
			contents = append(contents, genai.NewContentFromText(message.Text, genai.RoleModel))
		}
	}
	return contents
}

// generateOnce performs a single GenerateContent call, reporting the candidate's
// finish reason alongside the result so callers can react to a truncated response.
func (a *GoogleAdapter) generateOnce(ctx context.Context, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, genai.FinishReason, error) {
	a.throttle.WaitForSlot()
	result, err := a.geminiClient.Models.GenerateContent(ctx, a.model, contents, config)
	if err != nil {
		return nil, genai.FinishReasonUnspecified, err
	}
	finishReason := genai.FinishReasonUnspecified
	if len(result.Candidates) > 0 {
		finishReason = result.Candidates[0].FinishReason
	}
	return result, finishReason, nil
}

// buildGenerateConfig assembles the per-request vendor config: system instruction,
// output ceiling, and JSON response schema. Split out from GenerateText so the
// wiring is testable without a live client.
func (a *GoogleAdapter) buildGenerateConfig(chat *llm.ChatContext) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{
		SystemInstruction: a.buildSystemInstruction(chat),
	}

	// Clamped: the field is int32 and a caller-supplied value beyond that would
	// wrap negative, which the vendor reads as a nonsense ceiling.
	if max := chat.GetMaxOutputTokens(); max > 0 {
		if max > math.MaxInt32 {
			max = math.MaxInt32
		}
		config.MaxOutputTokens = int32(max)
	}

	if chat.GetResponseSchema() != nil {
		llm.EnforceAllRequired(chat.GetResponseSchema())
		if genaiSchema := convertSchemaToGenai(chat.GetResponseSchema()); genaiSchema != nil {
			config.ResponseSchema = genaiSchema
			// Response schema requires application/json MIME type
			config.ResponseMIMEType = "application/json"
		}
	}

	if chat.GoogleSearchEnabled() {
		if config.ResponseSchema != nil {
			// Gemini rejects search grounding combined with a JSON response schema;
			// the schema is the caller's contract, so the tool is what gets dropped.
			log.Warn().Msg("google search grounding dropped: incompatible with a response schema")
		} else {
			config.Tools = []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}
		}
	}
	return config
}

// buildSystemInstruction combines base system instruction and per-request instruction
func (a *GoogleAdapter) buildSystemInstruction(chat *llm.ChatContext) *genai.Content {
	text := buildSystemInstructionText(chat)
	if text == "" {
		return genai.NewContentFromText(defaultSystemInstruction, genai.RoleUser)
	}
	return genai.NewContentFromText(text, "")
}

func convertLLMSchemaPropertyTypeToGenaiType(llmType llm.SchemaPropertyType) genai.Type {
	switch llmType {
	case llm.SchemaPropertyTypeString:
		return genai.TypeString
	case llm.SchemaPropertyTypeInteger:
		return genai.TypeInteger
	case llm.SchemaPropertyTypeBoolean:
		return genai.TypeBoolean
	case llm.SchemaPropertyTypeObject:
		return genai.TypeObject
	case llm.SchemaPropertyTypeArray:
		return genai.TypeArray
	default:
		return genai.TypeUnspecified
	}
}

func convertPropertySchemaToGenai(llmProperty *llm.SchemaProperty) *genai.Schema {
	if llmProperty == nil {
		return nil
	}

	genaiSchema := &genai.Schema{
		Type:        convertLLMSchemaPropertyTypeToGenaiType(llmProperty.Type),
		Description: llmProperty.Description,
	}

	// Handle nullable types: JSON Schema uses ["type", "null"] for nullable fields
	// Since genai.Schema might not support this directly, we note it in the description
	// The field being optional (not in Required) should allow null values
	if llmProperty.Nullable && genaiSchema.Description != "" {
		genaiSchema.Description += " (nullable: can be null)"
	} else if llmProperty.Nullable {
		genaiSchema.Description = "nullable: can be null"
	}

	if len(llmProperty.Properties) > 0 {
		genaiSchema.Properties = make(map[string]*genai.Schema)
		for name, prop := range llmProperty.Properties {
			genaiSchema.Properties[name] = convertPropertySchemaToGenai(prop)
		}
	}

	if llmProperty.Items != nil {
		genaiSchema.Items = convertPropertySchemaToGenai(llmProperty.Items)
	}

	// Add required fields if this is an object type with required fields
	if len(llmProperty.Required) > 0 {
		genaiSchema.Required = llmProperty.Required
	}

	return genaiSchema
}

func convertSchemaToGenai(llmResponseSchema *llm.ResponseSchema) *genai.Schema {
	if llmResponseSchema == nil {
		return nil
	}

	genaiSchema := &genai.Schema{
		Type:     convertLLMSchemaPropertyTypeToGenaiType(llmResponseSchema.Type),
		Required: llmResponseSchema.Required,
	}

	if len(llmResponseSchema.Properties) > 0 {
		genaiSchema.Properties = make(map[string]*genai.Schema)
		for name, prop := range llmResponseSchema.Properties {
			genaiSchema.Properties[name] = convertPropertySchemaToGenai(prop)
		}
	}

	if llmResponseSchema.Items != nil {
		genaiSchema.Items = convertPropertySchemaToGenai(llmResponseSchema.Items)
	}

	return genaiSchema
}

// speakInstruction steers the TTS model to read the phrase verbatim; without it
// it occasionally treats the text as a task and answers it ("tried to generate
// text" 400s). Naming the language is load-bearing: Cyrillic scripts (Serbian,
// Russian, Bulgarian, …) share most letters, so without an explicit language
// Gemini defaults to the dominant one (Russian) and mispronounces the rest.
func speakInstruction(language string) string {
	if l := strings.TrimSpace(language); l != "" {
		return "Read the following aloud in " + l + ", exactly as written, with native " + l + " pronunciation. Do not translate it:\n\n"
	}
	return "Read the following aloud exactly as written, in its native pronunciation:\n\n"
}

// mimeSampleRate parses the rate from a PCM MIME type like
// "audio/L16;codec=pcm;rate=24000"; 0 when absent.
func mimeSampleRate(mime string) int {
	for _, param := range strings.Split(mime, ";") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(param), "rate="); ok {
			if rate, err := strconv.Atoi(v); err == nil && rate > 0 {
				return rate
			}
		}
	}
	return 0
}

// SetImageModel overrides the native image model used by GenerateImage.
// Empty keeps the default.
func (a *GoogleAdapter) SetImageModel(model string) {
	if model != "" {
		a.imageModel = model
	}
}

// GenerateImage renders one image from a text prompt via Gemini's native image
// modality. Like TTS, the image model is its own setting, not a chat tier.
func (a *GoogleAdapter) GenerateImage(prompt, userID string, meta map[string]string) (*llm.ImageResult, error) {
	failed := func(err error) (*llm.ImageResult, error) {
		return &llm.ImageResult{Model: a.imageModel}, err
	}

	cfg := &genai.GenerateContentConfig{
		ResponseModalities: []string{string(genai.ModalityImage)},
		// Landscape: these images are chat photo captions, never posters.
		ImageConfig: &genai.ImageConfig{AspectRatio: "16:9"},
	}
	contents := []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}

	a.throttle.WaitForSlot()
	resp, err := a.geminiClient.Models.GenerateContent(context.Background(), a.imageModel, contents, cfg)
	if err != nil {
		return failed(fmt.Errorf("gemini image: %w", err))
	}

	result := &llm.ImageResult{Model: a.imageModel}
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part.InlineData == nil || len(part.InlineData.Data) == 0 {
				continue
			}
			result.Data = part.InlineData.Data
			result.MimeType = part.InlineData.MIMEType
			break
		}
		if len(result.Data) > 0 {
			break
		}
	}
	if len(result.Data) == 0 {
		return failed(fmt.Errorf("no image in Gemini response"))
	}
	if resp.UsageMetadata != nil {
		result.Usage.Input = int(resp.UsageMetadata.PromptTokenCount)
		result.Usage.Output = int(resp.UsageMetadata.CandidatesTokenCount)
	}
	return result, nil
}

// SetTTS overrides the text-to-speech model and voice used by SynthesizeSpeech.
// Empty values keep the defaults.
func (a *GoogleAdapter) SetTTS(model, voice string) {
	if model != "" {
		a.ttsModel = model
	}
	if voice != "" {
		a.ttsVoice = voice
	}
}

// SynthesizeSpeech generates OGG/Opus audio for a short phrase. Gemini returns
// raw PCM, which is transcoded to OGG/Opus via ffmpeg (must be on PATH).
func (a *GoogleAdapter) SynthesizeSpeech(text, language, userID string, meta map[string]string) (*llm.SpeechResult, error) {
	// failed carries the actual TTS model alongside an error so usage tracking
	// records the real model (not the wrapping candidate's chat-model label).
	failed := func(err error) (*llm.SpeechResult, error) {
		return &llm.SpeechResult{Model: a.ttsModel}, err
	}
	if !audio.Available() {
		return failed(audio.ErrFFmpegMissing)
	}

	cfg := &genai.GenerateContentConfig{
		ResponseModalities: []string{string(genai.ModalityAudio)},
		SpeechConfig:       &genai.SpeechConfig{},
	}
	if a.ttsVoice != "" {
		cfg.SpeechConfig.VoiceConfig = &genai.VoiceConfig{
			PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: a.ttsVoice},
		}
	}
	contents := []*genai.Content{genai.NewContentFromText(speakInstruction(language)+text, genai.RoleUser)}

	ctx := context.Background()
	var lastErr error
	// One local retry for a transient API error. A reply carrying no audio is
	// NOT retried here — it is retryable at the resilience layer, which also
	// reaches the other vendor; retrying both places multiplies the round trips.
	for attempt := 0; attempt < 2; attempt++ {
		a.throttle.WaitForSlot()
		resp, err := a.geminiClient.Models.GenerateContent(ctx, a.ttsModel, contents, cfg)
		if err != nil {
			lastErr = err
			continue
		}
		var pcm []byte
		rate := ttsOutputSampleRate
		for _, cand := range resp.Candidates {
			if cand.Content == nil {
				continue
			}
			for _, part := range cand.Content.Parts {
				if part.InlineData == nil || len(part.InlineData.Data) == 0 {
					continue
				}
				pcm = append(pcm, part.InlineData.Data...)
				if r := mimeSampleRate(part.InlineData.MIMEType); r > 0 {
					rate = r
				}
			}
		}
		if len(pcm) == 0 {
			return failed(fmt.Errorf("gemini returned text, not speech: %w", llm.ErrNoAudioContent))
		}

		ogg, err := audio.EncodePCMToOggOpus(ctx, pcm, rate)
		if err != nil {
			return failed(err)
		}

		durationMs := audio.PCMDurationMs(pcm, rate)
		if durationMs < audio.MinClipMillis {
			// Encoder pads short clips with trailing silence to this length.
			durationMs = audio.MinClipMillis
		}
		result := &llm.SpeechResult{
			Audio:      ogg,
			MimeType:   "audio/ogg",
			DurationMs: durationMs,
			Model:      a.ttsModel,
		}
		if resp.UsageMetadata != nil {
			result.Usage.Input = int(resp.UsageMetadata.PromptTokenCount)
			result.Usage.Output = int(resp.UsageMetadata.CandidatesTokenCount)
		}
		return result, nil
	}
	return failed(fmt.Errorf("gemini tts: %w", lastErr))
}
