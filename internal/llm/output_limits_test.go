package llm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTruncatedErrorWrapsSentinel(t *testing.T) {
	err := TruncatedError("GoogleAdapter", "gemini-3.1-pro-preview", 1234)
	if !errors.Is(err, ErrOutputTruncated) {
		t.Fatalf("TruncatedError must wrap ErrOutputTruncated so callers can match it, got %v", err)
	}
	for _, want := range []string{"GoogleAdapter", "gemini-3.1-pro-preview", "1234"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q should name %q so the ceiling to raise is identifiable", err, want)
		}
	}
}

// A truncated generation is deterministic under the same prompt and ceiling, so
// the resilient adapter must not spend 3 retries plus 2 fallbacks reproducing it.
func TestTruncationIsNotRetryable(t *testing.T) {
	if IsRetryableError(TruncatedError("GoogleAdapter", "gemini-3.6-flash", 10)) {
		t.Fatal("truncation must not be retryable: retrying pays the full prompt cost for the same failure")
	}
}

func TestTruncationSurvivesCallerWrapping(t *testing.T) {
	wrapped := fmt.Errorf("vocab.stepping: %w", TruncatedError("GoogleAdapter", "m", 1))
	if !errors.Is(wrapped, ErrOutputTruncated) {
		t.Fatal("callers wrap adapter errors; the sentinel must still match through %w")
	}
}

func TestOutputCeilingsAreOrdered(t *testing.T) {
	if !(MaxTokensSmall < MaxTokensPlan && MaxTokensPlan <= MaxTokensTheory &&
		MaxTokensTheory < MaxTokensLarge && MaxTokensLarge < MaxTokensCombined) {
		t.Fatal("ceilings should grow with payload size; a smaller tier above a larger one is a typo")
	}
}

func TestMaxOutputTokensDefaultsToUnset(t *testing.T) {
	chat := NewChatContext()
	if got := chat.GetMaxOutputTokens(); got != 0 {
		t.Fatalf("a fresh context must not impose a ceiling, got %d", got)
	}
	chat.SetMaxOutputTokens(MaxTokensTheory)
	if got := chat.GetMaxOutputTokens(); got != MaxTokensTheory {
		t.Fatalf("want %d, got %d", MaxTokensTheory, got)
	}
}
