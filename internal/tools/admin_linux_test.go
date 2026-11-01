package tools

import (
	"slices"
	"strings"
	"testing"
)

func TestJournalArgs(t *testing.T) {
	argv, err := journalArgs(LogQueryInput{Lines: 10, Unit: "ssh", User: true, Log: "kernel", Priority: "error"}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"journalctl", "--no-pager", "-o", "json", "-n", "200", "--user", "--user-unit", "ssh", "-k", "-p", "err"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	for _, in := range []LogQueryInput{{Lines: 1, EventID: 4624}, {Lines: 1, Log: "Security"}, {Lines: 1, Priority: "loud"}} {
		if _, err := journalArgs(in, false); err == nil {
			t.Errorf("%+v should be rejected", in)
		}
	}
}

func TestJournalEntry(t *testing.T) {
	line := `{"MESSAGE":[104,105],"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_UNIT":"ssh.service","SYSLOG_IDENTIFIER":"sshd","_PID":"42"}`
	e, ok := journalEntry([]byte(line))
	if !ok || e.Message != "hi" || e.Priority != "err" || e.PID != 42 || e.Source != "sshd ssh.service" || !strings.HasPrefix(e.Time, "2023-11-1") {
		t.Errorf("entry = %+v, %v", e, ok)
	}
	if _, ok := journalEntry([]byte("not json")); ok {
		t.Error("invalid JSON should be skipped")
	}
}
