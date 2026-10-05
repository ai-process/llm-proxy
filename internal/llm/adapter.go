package llm

type Response struct {
	Choices []*Message
	Usage   TokensUsage
}

type TokensUsage struct {
	Input  int
	Output int
}

func (r *Response) AddMessage(message *Message) {
	r.Choices = append(r.Choices, message)
}

// SpeechResult is one synthesized clip.
type SpeechResult struct {
	Audio      []byte      // OGG/Opus bytes (chat voice-message format)
	MimeType   string      // e.g. "audio/ogg"
	DurationMs int         // 0 when unknown (e.g. the OpenAI path)
	Model      string      // the TTS model actually used (not the chat model)
	Usage      TokensUsage // zero when the vendor reports no token accounting
}

// ClientAdapter is the proxy's slice of the shared adapter interface: text
// generation only. Files and transcription stay in the client services, whose
// download URLs carry credentials the proxy must not see.
type ClientAdapter interface {
	GenerateText(chat *ChatContext) (*Response, error)
}

// SpeechSynthesizer is an optional adapter capability, discovered by type
// assertion so the ClientAdapter contract stays untouched.
type SpeechSynthesizer interface {
	SynthesizeSpeech(text, language, userID string, meta map[string]string) (*SpeechResult, error)
}
