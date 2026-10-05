package throttle

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ai-process/llm-proxy/internal/llm"
	"github.com/ai-process/llm-proxy/internal/proxydb"
)

func newTestThrottle(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	th := New(client, "test")
	return th, mr
}

func model(rpm int32, perKey, perUser int64) *proxydb.Model {
	return &proxydb.Model{ID: "m1", Vendor: "openai", RPM: rpm,
		DailyTokensPerKey: perKey, DailyTokensPerUser: perUser, Enabled: true}
}

func TestAllowRPMWindow(t *testing.T) {
	th, mr := newTestThrottle(t)
	ctx := context.Background()
	m := model(2, 0, 0)

	if !th.AllowRPM(ctx, "gen", m) || !th.AllowRPM(ctx, "gen", m) {
		t.Fatal("first two calls must pass")
	}
	if th.AllowRPM(ctx, "gen", m) {
		t.Fatal("third call in the same minute must be throttled")
	}
	// Another client key has its own window.
	if !th.AllowRPM(ctx, "other", m) {
		t.Fatal("another key must not share the window")
	}

	// Next minute: fresh window.
	base := th.now
	th.now = func() time.Time { return base().Add(time.Minute) }
	if !th.AllowRPM(ctx, "gen", m) {
		t.Fatal("next minute must open a fresh window")
	}

	// TTL is set so stale windows expire on their own.
	th.now = base
	key := th.keys.RPM("gen", m.ID, base().UTC().Unix()/60)
	if mr.TTL(key) <= 0 {
		t.Fatal("rpm key has no TTL")
	}
}

func TestReserveOverLimitRestoresCounter(t *testing.T) {
	th, mr := newTestThrottle(t)
	ctx := context.Background()
	m := model(0, 100, 0)
	day := time.Now().UTC().Format("20060102")
	key := th.keys.BudgetKey("gen", m.ID, day)

	res, tripped := th.Reserve(ctx, m, "gen", "", 60)
	if tripped != "" || len(res.Keys) != 1 {
		t.Fatalf("res=%v tripped=%q", res, tripped)
	}
	// 60 + 60 > 100: refused, and the counter must roll back to 60.
	res2, tripped := th.Reserve(ctx, m, "gen", "", 60)
	if res2 != nil || tripped != "budget_key" {
		t.Fatalf("res=%v tripped=%q, want budget_key", res2, tripped)
	}
	val, _ := mr.Get(key)
	if val != "60" {
		t.Fatalf("counter = %s, want 60 after rollback", val)
	}
	if mr.TTL(key) <= 0 {
		t.Fatal("budget key has no TTL")
	}
}

func TestReserveUserBudget(t *testing.T) {
	th, _ := newTestThrottle(t)
	ctx := context.Background()
	m := model(0, 0, 50)

	// Without a user id the user budget simply never applies.
	res, tripped := th.Reserve(ctx, m, "gen", "", 999)
	if tripped != "" || res != nil && len(res.Keys) != 0 {
		t.Fatalf("res=%v tripped=%q, want no enforcement", res, tripped)
	}

	if _, tripped = th.Reserve(ctx, m, "gen", "u1", 40); tripped != "" {
		t.Fatalf("first user reserve tripped: %q", tripped)
	}
	if _, tripped = th.Reserve(ctx, m, "gen", "u1", 40); tripped != "budget_user" {
		t.Fatalf("tripped=%q, want budget_user", tripped)
	}
	// A different user has their own budget.
	if _, tripped = th.Reserve(ctx, m, "gen", "u2", 40); tripped != "" {
		t.Fatalf("other user tripped: %q", tripped)
	}
}

func TestReserveTripsSecondBudgetUndoesFirst(t *testing.T) {
	th, mr := newTestThrottle(t)
	ctx := context.Background()
	m := model(0, 1000, 50) // key budget generous, user budget tight
	day := time.Now().UTC().Format("20060102")

	res, tripped := th.Reserve(ctx, m, "gen", "u1", 60)
	if res != nil || tripped != "budget_user" {
		t.Fatalf("res=%v tripped=%q", res, tripped)
	}
	// The key-budget increment must have been rolled back.
	val, _ := mr.Get(th.keys.BudgetKey("gen", m.ID, day))
	if val != "0" && val != "" {
		t.Fatalf("key budget = %q, want rolled back", val)
	}
}

func TestSettleAdjustsToActuals(t *testing.T) {
	th, mr := newTestThrottle(t)
	ctx := context.Background()
	m := model(0, 1000, 0)
	day := time.Now().UTC().Format("20060102")
	key := th.keys.BudgetKey("gen", m.ID, day)

	res, _ := th.Reserve(ctx, m, "gen", "", 100)

	// Actuals higher than the estimate: counter goes up.
	th.Settle(res, llm.TokensUsage{Input: 90, Output: 60})
	waitFor(t, func() bool { v, _ := mr.Get(key); return v == "150" })

	// Failure path: zero actuals release the whole reservation.
	res2, _ := th.Reserve(ctx, m, "gen", "", 100)
	th.Settle(res2, llm.TokensUsage{})
	waitFor(t, func() bool { v, _ := mr.Get(key); return v == "150" })
}

func TestFailOpenWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	th := New(client, "test")
	mr.Close() // Redis just died

	ctx := context.Background()
	m := model(1, 100, 0)
	if !th.AllowRPM(ctx, "gen", m) {
		t.Fatal("rpm must fail open")
	}
	res, tripped := th.Reserve(ctx, m, "gen", "", 50)
	if tripped != "" {
		t.Fatalf("reserve tripped %q, must fail open", tripped)
	}
	th.Settle(res, llm.TokensUsage{Input: 1, Output: 1}) // must not panic
}

func TestNilClientAllowsEverything(t *testing.T) {
	th := New(nil, "")
	ctx := context.Background()
	m := model(1, 1, 1)
	if !th.AllowRPM(ctx, "gen", m) {
		t.Fatal("nil client must allow")
	}
	if res, tripped := th.Reserve(ctx, m, "gen", "u", 999); res != nil || tripped != "" {
		t.Fatal("nil client must not reserve or trip")
	}
	th.Settle(nil, llm.TokensUsage{})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
