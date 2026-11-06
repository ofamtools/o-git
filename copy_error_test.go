package main

import (
	"errors"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

type errSink struct{ status []string }

func (s *errSink) Notify(ochazuke.NoticeID, string) {}
func (s *errSink) ClearNotice(ochazuke.NoticeID)    {}
func (s *errSink) Status(t string)                  { s.status = append(s.status, t) }
func (s *errSink) Info(string)                      {}

// A copy whose register write fails says so on the status line and runs no
// feedback; one that works yanks the text and runs it.
func TestCopyReportsRegisterError(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	sink := &errSink{}
	a.r.UI().Attach(sink)
	fed := 0
	a.finishCopy("abc", func(string) { fed++ })
	if got := a.r.Registers().Named["\""].Yank.Text; got != "abc" || fed != 1 || len(sink.status) != 0 {
		t.Fatalf("register %q, feedback %d, errors %v", got, fed, sink.status)
	}
	a.yank = func(string) error { return errors.New("register is read-only") }
	a.finishCopy("def", func(string) { fed++ })
	if fed != 1 || len(sink.status) != 1 || !strings.Contains(sink.status[0], "register is read-only") {
		t.Fatalf("feedback %d, errors %v", fed, sink.status)
	}
	if got := a.r.Registers().Named["\""].Yank.Text; got != "abc" {
		t.Fatalf("register changed to %q", got)
	}
}
