package adapters

import (
	"testing"

	"github.com/ai-process/llm-proxy/internal/llm"
	usagev1 "github.com/ai-process/llm-proxy/internal/usagereport/gen/usage/v1"
)

type stubAdapter struct{ resp *llm.Response }

func (s *stubAdapter) GenerateText(chat *llm.ChatContext) (*llm.Response, error) {
	return s.resp, nil
}

type capturingTracker struct {
	userID, actionID, vendor, model string
	in, out                         int
	meta                            map[string]string
	calls                           int
}

func (c *capturingTracker) RecordUsage(userID, actionID, vendor, model string, inputTokens, outputTokens int, durationMs int64, status usagev1.Status, meta map[string]string) error {
	c.userID, c.actionID, c.vendor, c.model = userID, actionID, vendor, model
	c.in, c.out = inputTokens, outputTokens
	c.meta = meta
	c.calls++
	return nil
}

// The whole point of usage meta is per-job token accounting: whatever the
// caller sets on the chat must reach the tracker verbatim.
func TestTrackingAdapterPassesCallerMeta(t *testing.T) {
	resp := &llm.Response{Usage: llm.TokensUsage{Input: 123, Output: 45}}
	tracker := &capturingTracker{}
	adapter := NewTrackingAdapter(&stubAdapter{resp: resp}, tracker, "google", "gemini-test")

	chat := llm.NewChatContext()
	chat.SetActionID("app.test")
	chat.SetCaller(llm.Caller{
		UserID: "batch-worker",
		Meta:   map[string]string{"job_id": "job-1", "gentask_id": "task-2"},
	})

	if _, err := adapter.GenerateText(chat); err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if tracker.calls != 1 {
		t.Fatalf("expected 1 tracker call, got %d", tracker.calls)
	}
	if tracker.userID != "batch-worker" || tracker.actionID != "app.test" {
		t.Fatalf("attribution lost: %q %q", tracker.userID, tracker.actionID)
	}
	if tracker.in != 123 || tracker.out != 45 {
		t.Fatalf("tokens lost: %d/%d", tracker.in, tracker.out)
	}
	if tracker.meta["job_id"] != "job-1" || tracker.meta["gentask_id"] != "task-2" {
		t.Fatalf("meta lost: %v", tracker.meta)
	}
}

// Without a caller the tracker still records, with nil meta.
func TestTrackingAdapterNilMeta(t *testing.T) {
	tracker := &capturingTracker{}
	adapter := NewTrackingAdapter(&stubAdapter{resp: &llm.Response{}}, tracker, "google", "gemini-test")

	chat := llm.NewChatContext()
	chat.SetInternalUserID("someone")
	if _, err := adapter.GenerateText(chat); err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if tracker.calls != 1 || tracker.meta != nil {
		t.Fatalf("expected 1 call with nil meta, got calls=%d meta=%v", tracker.calls, tracker.meta)
	}
}
