package llm

import (
	"errors"
	"fmt"
)

// Output ceilings for generation calls. Output tokens cost ~8x input on the
// expensive tier ($10.00 vs $1.25 per 1M), so an unbounded generation is the
// costliest failure mode in the service. These bound the tail; they are not
// sized to the length each prompt asks for.
//
// The reason they look so generous: thinking tokens are charged against the same
// budget, thinking is on by default for every Gemini 2.5/3.x model in the
// collection, and a thinking budget alone can reach tens of thousands of tokens.
// A ceiling near the actual answer length would be spent entirely on thinking and
// return nothing. Each tier therefore has to cover thinking plus the answer, and
// the adapters retry uncapped if a ceiling is exhausted anyway.
//
// Bounding thinking directly (ThinkingConfig.ThinkingBudget) would be the sharper
// lever, but this SDK exposes only ThinkingBudget while Gemini 3 documents
// thinking_level, so support is unverified — sending an unsupported value risks
// 400-ing every generation call. Validate against the live API before adding it.
const (
	// MaxTokensSmall covers short structured replies: course descriptions,
	// detected language codes, a couple of extra questions, audio phrase picks.
	MaxTokensSmall = 8_000
	// MaxTokensPlan covers list-shaped plans: lesson lists, course ladders, deck plans.
	MaxTokensPlan = 16_000
	// MaxTokensTheory covers one lesson's HTML theory (prompts ask for ~1-4k characters).
	MaxTokensTheory = 16_000
	// MaxTokensLarge covers the big structured payloads: exercises with questions,
	// stepping JSON, phrase banks, placement tests.
	MaxTokensLarge = 32_000
	// MaxTokensCombined covers the vocab single-call path that returns bank,
	// theory, stepping and exercises together.
	MaxTokensCombined = 48_000
)

// ErrOutputTruncated means the vendor ran out of output budget even with no
// ceiling of ours in play — the adapters lift theirs and retry before surfacing
// this, so reaching it means the model's own limit was hit. Deliberately not
// retryable: the same prompt hits the same wall, so retrying burns the full
// prompt cost on a doomed call. Callers see a failed task instead of a
// half-written JSON payload silently reaching the database.
var ErrOutputTruncated = errors.New("output truncated: hit max output tokens")

// TruncatedError builds the error both adapters return when a generation stops on
// its output ceiling, so the ceiling that needs raising is named in the log line.
func TruncatedError(vendor, model string, producedChars int) error {
	return fmt.Errorf("%s: %w (model %s, produced %d chars)", vendor, ErrOutputTruncated, model, producedChars)
}
