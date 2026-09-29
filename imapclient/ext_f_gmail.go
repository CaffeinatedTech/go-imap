package imapclient

import (
	"github.com/kiliant/go-imap"
	"strings"

	"github.com/kiliant/go-imap/internal/imapwire"
)

// This file implements the Gmail-specific label store (X-GM-EXT-1). Gmail
// models labels, not folders: the label list of a message is changed with
// UID STORE using the X-GM-LABELS item, and every label add or remove is
// visible in every folder that holds the message. Label values are Gmail
// label names — the same strings LIST reports for the label's mailbox —
// and they are astrings: user labels may contain spaces, quotes and
// non-ASCII characters, so they must be encoded with the mailbox-name
// rules, not as bare atoms.
//
// Responses: a successful STORE without .SILENT causes the server to send
// an untagged FETCH carrying the new X-GM-LABELS value. The value is a
// parenthesised list of astrings, which this client does not model as a
// typed item; it is delivered as [*imap.FetchDataRaw] under the
// X-GM-LABELS key, preserving the exact wire bytes (see the FetchDataRaw
// contract for the reader's lifetime).

// gmailLabelItem maps a STORE flag operation onto the X-GM-LABELS item
// name Gmail defines for it.
func gmailLabelItem(op StoreFlagsOp) (string, bool) {
	switch op {
	case StoreFlagsSet:
		return "X-GM-LABELS", true
	case StoreFlagsAdd:
		return "+X-GM-LABELS", true
	case StoreFlagsRemove:
		return "-X-GM-LABELS", true
	default:
		return "", false
	}
}

// StoreUIDGmailLabels changes the Gmail labels of the messages in set
// (X-GM-EXT-1). op selects whether labels replaces the message's entire
// label list, adds to it, or removes from it, exactly as the matching
// flag operations of [Client.StoreUID] do for flags. A nil options selects
// the non-silent form, which lets the server report the new label list in
// an untagged FETCH; pass StoreOptions{Silent: true} to suppress it.
//
// The set may use "*". An empty label list with StoreFlagsSet clears every
// label from the messages. This command is gated on the X-GM-EXT-1
// capability and returns an [imap.Error] wrapping
// [ErrCapabilityNotAdvertised] without writing anything to the connection
// when the server does not advertise it.
func (c *Client) StoreUIDGmailLabels(set imap.UIDSet, op StoreFlagsOp, labels []string, options *StoreOptions) *Command {
	const name = "UID STORE"
	if !c.hasCapability("X-GM-EXT-1") {
		return unsupportedCommand(name, "X-GM-EXT-1")
	}
	if set.String() == "" {
		return rejectedCommand(c, name, "STORE requires a non-empty set")
	}
	item, ok := gmailLabelItem(op)
	if !ok {
		return rejectedCommand(c, name, "invalid STORE flag operation")
	}
	silent := options != nil && options.Silent
	if silent {
		// The .SILENT suffix rides on the item name, matching the flag
		// STORE encoding.
		item += ".SILENT"
	}
	return c.beginCommand(name, stateSelected, func(enc *imapwire.Encoder) {
		enc.SP()
		writeNumSet(enc, set.String())
		enc.SP().Atom(item).SP().List(len(labels), func(i int) {
			// System labels arrive as flags ("\Inbox") and user labels as
			// mailbox names, which may be quoted ("my receipts"): encode
			// each in the form Gmail itself sends, so a value that is a
			// flag stays a flag and everything else follows the
			// mailbox-name astring rules.
			if strings.HasPrefix(labels[i], `\`) {
				enc.Flag(labels[i])
			} else {
				enc.Mailbox(labels[i])
			}
		})
	}, nil)
}
