# PATCH NOTES — Gmail label store (temporary doc for the upstream PR)

This checkout is a fork of `github.com/kiliant/go-imap` carrying two
patches jmap-bridge needs for Gmail label writes (X-GM-EXT-1). The
patches are deliberately small and self-contained so they can go
upstream as one PR; this file is a temporary home for the reasoning
that belongs in that PR, and should be deleted once it is merged.

Branch: `feat/gmail-labels-store`, cut from `main` (the root module —
imapclient, imapwire — is identical to v1.1.0; main only adds
imapserver work, which these patches do not touch).

## Background

Gmail models labels, not folders. A message's label list is changed
with:

```
UID STORE <set> [+|-]X-GM-LABELS (<label>...)
```

Label values are Gmail label names — the same strings LIST reports for
the label's mailbox. System labels are flag-form (`\Inbox`, `\Sent`,
`\Trash`, …); user labels are astrings and may contain spaces, quotes
and non-ASCII characters (`my receipts`, `say "hi"`, `réunions`).

## Patch 1: `imapclient/ext_f_gmail.go` (+ its test)

New capability-gated command `Client.StoreUIDGmailLabels(set, op,
labels, options)`. `op` reuses `StoreFlagsOp` (`FLAGS` / `+FLAGS` /
`-FLAGS` map onto `X-GM-LABELS` / `+X-GM-LABELS` / `-X-GM-LABELS`), and
`StoreOptions.Silent` produces the `.SILENT` form, matching the flag
STORE encoding.

Label encoding follows the two shapes Gmail itself sends:

- a value beginning with `\` is a system label and is written with the
  flag production (`\Inbox`, unquoted);
- every other value is a mailbox-name astring (`enc.Mailbox`): quoted
  when it is not an atom, UTF-7 encoded unless UTF8=ACCEPT is enabled —
  exactly the rules LIST mailbox names follow, because label names and
  mailbox names are the same strings.

Without this, labels containing spaces cannot be stored at all through
the typed API: the existing `StoreUID` writes every item through
`Encoder.Atom`, which refuses non-atom characters, and there is no
raw-command escape in the public API.

A non-silent store makes Gmail report the new list in an untagged
FETCH; the value is an unmodelled parenthesised list and arrives as
`*imap.FetchDataRaw` under the `X-GM-LABELS` key (see the FetchDataRaw
contract). The test covers that round trip, including a system label in
the value.

## Patch 2: `internal/imapwire/decoder.go` — flag-form values

`Decoder.DiscardValue` (the capture primitive behind unmodelled FETCH
response values) had no case for a value beginning with `\`: a
backslash is not an astring character, so `X-GM-LABELS (\Inbox …)` —
which is what Gmail sends after any label store — failed the parse
instead of being preserved. The new case consumes the backslash and the
atom that follows it; because capture is byte-level (`consume()` feeds
`captureBytes`), the raw bytes still reach `FetchDataRaw` unchanged.

The same gap would bite any future extension whose unmodelled values
are flag-form, so the fix lives in the decoder, not in Gmail-specific
code.

## Testing

- `go test ./imapclient/ ./internal/imapwire/ -race` green, including
  the new `TestStoreUIDGmailLabels*` cases (wire form per table,
  capability gate writes nothing, .SILENT placement, empty-set and
  invalid-op rejection, FetchDataRaw round trip).
- Full `go test ./...` green except `internal/unicodenorm`, which is a
  pre-existing toolchain skew on this machine (Go 1.25's
  `unicode.Version` is 17.0.0, the shipped tables were generated from
  UCD 15.0.0) and is unrelated to these patches.

## PR plan

1. One PR, two commits: the decoder fix, then the command + tests.
2. Mention in the PR that fetch-side X-GM-LABELS / X-GM-THRID requests
   already work through the open-ended `FetchItemKeyword`; only the
   store side needed new API.
3. Consumer: jmap-bridge (CaffeinatedTech) uses this via a `replace`
   directive until the PR lands, then reverts to the upstream version.
