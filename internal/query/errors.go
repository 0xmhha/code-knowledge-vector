package query

import "errors"

// The error model (featurelist §8.4). Six sentinels make up the contract
// between ckv and its callers (LLM agents via MCP, in-process consumers
// via pkg/ckv). Callers test with errors.Is and decide handling per
// variant; the caller-guidance for each is documented inline.
//
// Backlog B6.

// ErrIndexUnavailable signals that the on-disk index cannot be served
// by the supplied Embedder. Most commonly: the indexed model identity
// differs from the query-time model, the dimension is different, or
// the manifest is missing.
//
// Caller guidance: surface to the operator with a "run `ckv build`"
// hint. Do not retry — the index will not change without a rebuild.
var ErrIndexUnavailable = errors.New("query: index unavailable")

// ErrFreshnessStale signals the index's recorded IndexedHead is behind
// the source tree's current git HEAD. Returned by the freshness helpers
// (Engine.CheckFreshness, freshness.Check) when callers opt in to strict
// freshness; the on-the-wire Response keeps the soft Metadata.Fresh bool
// for callers that only need a hint.
//
// Caller guidance: the result set is still usable for many cases (recent
// edits typically affect a small subset of files). Schedule a reindex
// when convenient. Prompt-time freshness sensitivity is the caller's
// taste.
var ErrFreshnessStale = errors.New("query: index freshness stale")

// ErrBudgetExceeded signals SearchOptions.BudgetTokens is too small for
// the engine to render even a minimum-density response. The engine
// degrades snippets to signature-only before raising; reaching this
// error means the budget is below the floor (MinBudgetTokens).
//
// Caller guidance: raise BudgetTokens (defaults to 4000), or accept an
// empty response by passing BudgetTokens<0 to disable budgeting.
var ErrBudgetExceeded = errors.New("query: budget exceeded")

// ErrCitationNotFound signals catastrophic citation enforcement failure:
// every candidate that passed the threshold gate was dropped because its
// file could not be located under the recorded src_root. Almost always
// indicates the source tree was deleted or moved without rebuilding the
// index.
//
// Caller guidance: re-run `ckv build --src <current path>`. Until then
// the index is unusable for retrieval — every hit would be unverifiable.
var ErrCitationNotFound = errors.New("query: citation not found")

// ErrSanitizeFailed signals the sanitize pipeline (UC-V13, featurelist
// §9) rejected the response payload. The sanitize module is the
// default-deny gate between raw retrieval and outbound delivery —
// secrets, PII, oversized blobs, or policy-flagged content land here.
//
// Caller guidance: log the rejection (sanitize_report carries the
// reason), do not retry with the same intent. Defined now for
// forward-compatible callers; sanitize itself lands in S2.
var ErrSanitizeFailed = errors.New("query: sanitize failed")

// ErrPolicyError signals a policy or authorization check rejected the
// request. Typical raises: mTLS caller-cert SAN mismatch with envelope
// caller, intent flagged by content policy, or an internal-only tool
// requested through an external surface.
//
// Caller guidance: this is a hard rejection — do not retry. Surface to
// the operator. Defined now for forward-compatible callers; the policy
// gate lands with S6 (mTLS auth).
var ErrPolicyError = errors.New("query: policy error")

// MinBudgetTokens is the floor below which BudgetTokens can't even fit
// a single signature-density hit. Set so a one-line Go signature
// (~50-80 chars) rounds up to ~20 tokens with one hit; below this the
// engine returns ErrBudgetExceeded rather than truncate further.
const MinBudgetTokens = 20
