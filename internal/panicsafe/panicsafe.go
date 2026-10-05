// Package panicsafe turns a panic into a reported, recoverable error instead
// of a silent process death. The proxy serves many concurrent LLM calls; a
// panic in any of them (a nil deref, a bad vendor response) would otherwise take
// the service down with nothing in Sentry to explain it.
package panicsafe

import (
	"runtime/debug"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog/log"
)

// sentryFlushTimeout bounds how long Report waits for the event to reach
// Sentry. We flush on every panic so the report is delivered even if the
// process dies moments later for an unrelated reason.
const sentryFlushTimeout = 2 * time.Second

// Report captures a recovered panic value as a first-class Sentry exception
// (with the panic stacktrace, so issues group by origin) and logs a console
// breadcrumb. It is a no-op when r is nil. Safe when Sentry is disabled: the
// default hub has no client and the capture/flush become no-ops.
//
// The console line is logged at warn, not error, on purpose: the service's
// zerolog->Sentry writer captures error-level logs, so logging the panic at
// error would double-report it. The authoritative Sentry event is the
// hub.Recover exception below.
func Report(where string, r any) {
	if r == nil {
		return
	}
	hub := sentry.CurrentHub()
	hub.Recover(r)
	hub.Flush(sentryFlushTimeout)

	log.Warn().
		Str("where", where).
		Interface("panic", r).
		Bytes("stack", debug.Stack()).
		Msg("recovered panic (reported to Sentry)")
}

// Guard recovers a panic in the current goroutine and reports it. Defer it as
// the first statement inside a spawned goroutine:
//
//	go func() {
//		defer wg.Done()
//		defer panicsafe.Guard("worker: process job")
//		...
//	}()
//
// Deferred functions run last-in-first-out, so Guard (deferred after wg.Done)
// runs first on a panic — it recovers, then wg.Done still runs, so a panic
// never leaves a WaitGroup un-Done.
func Guard(where string) {
	if r := recover(); r != nil {
		Report(where, r)
	}
}
