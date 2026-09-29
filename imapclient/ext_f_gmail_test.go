package imapclient

import (
	"strings"
	"testing"

	"github.com/kiliant/go-imap"
)

// The X-GM-EXT-1 label store: the item name with its operation and
// .SILENT suffix, and label values encoded as astrings (quoted when they
// are not atoms) rather than bare atoms, which is the whole point — Gmail
// user labels may contain spaces, quotes and non-ASCII characters, so the
// encoding follows the mailbox-name rules.
func TestStoreUIDGmailLabels(t *testing.T) {
	tests := []struct {
		name   string
		set    imap.UIDSet
		op     StoreFlagsOp
		labels []string
		silent bool
		want   string
	}{
		{
			name:   "add atom label",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsAdd,
			labels: []string{"receipts"},
			want:   `UID STORE 7 +X-GM-LABELS (receipts)`,
		},
		{
			name:   "quoted label with space",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsAdd,
			labels: []string{"my receipts"},
			want:   `UID STORE 7 +X-GM-LABELS ("my receipts")`,
		},
		{
			name:   "remove system and user labels",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsRemove,
			labels: []string{`\Inbox`, "work stuff"},
			want:   `UID STORE 7 -X-GM-LABELS (\Inbox "work stuff")`,
		},
		{
			name:   "replace clears to an empty list",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsSet,
			labels: nil,
			want:   `UID STORE 7 X-GM-LABELS ()`,
		},
		{
			name:   "silent rides on the item name",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsRemove,
			labels: []string{"receipts"},
			silent: true,
			want:   `UID STORE 7 -X-GM-LABELS.SILENT (receipts)`,
		},
		{
			name:   "uid set form",
			set:    imap.UIDSetNum(1, 3, 9),
			op:     StoreFlagsAdd,
			labels: []string{"receipts"},
			want:   `UID STORE 1,3,9 +X-GM-LABELS (receipts)`,
		},
		{
			name:   "quote escaping",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsAdd,
			labels: []string{`say "hi"`},
			want:   `UID STORE 7 +X-GM-LABELS ("say \"hi\"")`,
		},
		{
			name:   "multiple labels in one command",
			set:    imap.UIDSetNum(7),
			op:     StoreFlagsAdd,
			labels: []string{"a", "b c", `\Starred`},
			want:   `UID STORE 7 +X-GM-LABELS (a "b c" \Starred)`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, server := extBDial(t, func(tag, line string) string {
				return tag + " OK stored\r\n"
			})
			extBReady(c, []string{"IMAP4rev1", "UIDPLUS", "X-GM-EXT-1"}, nil, true)
			opts := &StoreOptions{Silent: tc.silent}
			if err := c.StoreUIDGmailLabels(tc.set, tc.op, tc.labels, opts).
				Wait(extBContext(t)); err != nil {
				t.Fatalf("store: %v", err)
			}
			if got := server.LastLine(); !strings.HasSuffix(got, tc.want) {
				t.Fatalf("command line = %q, want suffix %q", got, tc.want)
			}
		})
	}
}

// The second command's set argument exercises the compact UID set form
// (7 alone, not "7,7").
func TestStoreUIDGmailLabelsEmptySet(t *testing.T) {
	c, server := extBDial(t, func(tag, line string) string {
		return tag + " BAD nope\r\n"
	})
	extBReady(c, []string{"IMAP4rev1", "X-GM-EXT-1"}, nil, true)
	var empty imap.UIDSet
	err := c.StoreUIDGmailLabels(empty, StoreFlagsAdd, []string{"x"}, nil).Wait(extBContext(t))
	if err == nil {
		t.Fatal("expected empty-set rejection, got nil")
	}
	if got := server.LastLine(); got != "" {
		t.Fatalf("rejected command wrote %q to the connection", got)
	}
}

// Without X-GM-EXT-1 the command fails without writing to the connection
// — the capability gate is checked before any encoder work.
func TestStoreUIDGmailLabelsNotAdvertised(t *testing.T) {
	c, server := extBDial(t, func(tag, line string) string {
		return tag + " OK reached\r\n"
	})
	extBReady(c, []string{"IMAP4rev1"}, nil, true)
	err := c.StoreUIDGmailLabels(imap.UIDSetNum(7), StoreFlagsAdd, []string{"x"}, nil).Wait(extBContext(t))
	if err == nil {
		t.Fatal("expected capability error, got nil")
	}
	if got := server.LastLine(); got != "" {
		t.Fatalf("gated command wrote %q to the connection", got)
	}
}

// A non-silent store makes Gmail report the new label list in an untagged
// FETCH; the value is an unmodelled parenthesised list, so it must arrive
// preserved as FetchDataRaw under the exact item name the server sent.
func TestStoreUIDGmailLabelsFetchUpdate(t *testing.T) {
	c, _ := extBDial(t, func(tag, line string) string {
		return "* 1 FETCH (UID 7 X-GM-LABELS (\\Inbox \"my receipts\"))\r\n" +
			tag + " OK stored\r\n"
	})
	extBReady(c, []string{"IMAP4rev1", "X-GM-EXT-1"}, nil, true)
	cmd := c.FetchUID(imap.UIDSetNum(7), nil, imap.FetchItemKeyword("X-GM-LABELS"))
	data, err := cmd.Next(extBContext(t))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	items, ok := data.Items[imap.FetchDataKey("X-GM-LABELS")]
	if !ok || len(items) == 0 {
		t.Fatalf("X-GM-LABELS missing from %#v", data.Items)
	}
	raw, ok := items[0].(*imap.FetchDataRaw)
	if !ok {
		t.Fatalf("X-GM-LABELS value is %T, want *imap.FetchDataRaw", items[0])
	}
	buf := make([]byte, 64)
	n, _ := raw.Reader.Read(buf)
	if got := string(buf[:n]); got != `(\Inbox "my receipts")` {
		t.Fatalf("raw label value = %q", got)
	}
}

// Gmail answers "OK [THROTTLED]" for commands it did not complete; the
// code must survive on the Command so callers can treat it as failure
// even though Wait returns nil.
func TestRespCodeSurvivesQualifiedOK(t *testing.T) {
	c, _ := extBDial(t, func(tag, line string) string {
		return tag + " OK [THROTTLED] not completed\r\n"
	})
	extBReady(c, []string{"IMAP4rev1", "X-GM-EXT-1"}, nil, true)
	cmd := c.StoreUIDGmailLabels(imap.UIDSetNum(7), StoreFlagsAdd, []string{"x"}, nil)
	if err := cmd.Wait(extBContext(t)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := cmd.RespCode(); got != "THROTTLED" {
		t.Fatalf("RespCode = %q, want THROTTLED", got)
	}
}

func TestRespCodeEmptyOnPlainOK(t *testing.T) {
	c, _ := extBDial(t, func(tag, line string) string {
		return tag + " OK done\r\n"
	})
	extBReady(c, []string{"IMAP4rev1", "X-GM-EXT-1"}, nil, true)
	cmd := c.StoreUIDGmailLabels(imap.UIDSetNum(7), StoreFlagsAdd, []string{"x"}, nil)
	if err := cmd.Wait(extBContext(t)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := cmd.RespCode(); got != "" {
		t.Fatalf("RespCode = %q, want empty", got)
	}
}
