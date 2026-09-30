# PATCH NOTES

This repository is a hard fork of `github.com/kiliant/go-imap`, maintained
as `github.com/CaffeinatedTech/go-imap`. It is not proposed upstream;
this file records what diverges from upstream and why, so the fork stays
reviewable and can be re-diffed on the next upstream sync.

The fork is taken at kiliant's `main`. The root module (imapclient,
internal/imapwire) is identical to upstream v1.1.0; the patches below
touch only root-module files, so `imapserver` is unchanged from upstream
`main` apart from the module-path rename.

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

## Patch 3: `imapclient/client.go` — `Command.RespCode`

The tagged completion discarded the resp-text-code on success: an
`OK [THROTTLED]` was indistinguishable from a plain OK. Google's IMAP
servers answer `OK [THROTTLED]` when a command was **not completed**
because the account hit its rate limit — a code that qualifies the OK
rather than confirming it. `Command.RespCode()` now returns the code of
the tagged OK ("" for plain OK/failure), set before the completion
channel closes so it is readable as soon as `Wait` returns. The live
jmap-bridge Gmail gate turned this up: writes "succeeded" while Google
silently dropped them.

## Patch 4: `imapclient/ext_a_move.go` — `MoveData.RespCode`

`MoveUID` completes internally and could not surface the tagged response
code; the Gmail throttle work needed to see `OK [THROTTLED]` on a move
(a move Google did not perform). `MoveData` now carries the tagged OK's
resp-text-code, set from the completion callback.

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

## Fork maintenance

1. Upstream is not being asked to take these patches; this is a hard
   fork maintained by CaffeinatedTech.
2. Fetch-side X-GM-LABELS / X-GM-THRID requests already work through
   the open-ended `FetchItemKeyword`; only the store side needed new
   API.
3. Consumer: jmap-bridge (CaffeinatedTech) depends on
   `github.com/CaffeinatedTech/go-imap` directly.

---

# PATCH NOTES — long response lines (second, independent PR)

Found on a live commercial Dovecot (2026-09-30) while gating jmap-bridge
against a real account: a legitimate FETCH answered one 8401-octet
`BODYSTRUCTURE` line with no literal payload (a 8.9 MB message with many
attachments, all quoted parameters). `imapwire`'s default
`MaxLineLength = 8 << 10` rejected it, the reader treated it as fatal,
and the poisoned session failed every later command on that connection
with the same opaque `invalid server response`.

Neither RFC 3501 nor RFC 9051 bounds the length of an untagged response
line (the 8192 figure that motivated the default is the command
direction's recommendation; it appears in neither RFC's response
grammar). Servers in practice emit long quoted BODYSTRUCTURE lines.

## Patches

1. `internal/imapwire/options.go` — `DefaultMaxLineLength` raised to
   `1 << 20`: a memory-containment budget for one buffered line per
   connection, not an interop assumption. The command direction is
   unaffected: `imapserver` always sets its own explicit
   `MaxCommandLineBytes`.
2. `internal/imapwire/encoder.go` — `responseQuotedLineBudget` fixed at
   `4 << 10` instead of `DefaultMaxLineLength / 2`: the encoder's
   literalisation point is wire behaviour and must not drift when the
   decode budget changes. (Keeps the server module byte-for-byte as
   before; `TestEncoderResponseStringKeepsLineBounded` now sizes its
   fixture off the budget, not the default.)
3. `imapclient/client.go` — `Options.MaxLineLength` passthrough for
   callers who want a tighter cap, and `protocolError` now appends the
   wrapped cause to `Text` (the old shape hid the real parse error
   behind "invalid server response" and cost hours on the live gate).
4. `imapclient/regression_test.go` —
   `TestFetchLongResponseLineParses`: 9 KiB quoted BODYSTRUCTURE line
   parses on a default client.

Applied on `main` as its own commit, separate from the Gmail-label patch
set above.

## Testing

- `go test ./imapclient/ ./internal/imapwire/ ./internal/imapcodec/
  ./imapserver/` green (the `internal/unicodenorm` failure is the known
  toolchain-skew note above, unrelated).
- Live: full EXAMINE + batched header backfill over every folder, plus a
  body-fetch pass, clean against the server that reproduced the bug.
