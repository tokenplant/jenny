package testutil

import (
	"fmt"
	"os"
	"testing"
)

func TestSSELine(t *testing.T) {
	got := SSELine("message_start", `{"id":"msg_1"}`)
	want := "event: message_start\ndata: {\"id\":\"msg_1\"}\n\n"
	if got != want {
		t.Errorf("SSELine() = %q, want %q", got, want)
	}
}

func TestSSELine_EmptyData(t *testing.T) {
	got := SSELine("ping", "")
	want := "event: ping\ndata: \n\n"
	if got != want {
		t.Errorf("SSELine() = %q, want %q", got, want)
	}
}

func TestCaptureStdout(t *testing.T) {
	got := CaptureStdout(t, func() {
		fmt.Fprint(os.Stdout, "hello world")
	})
	if got != "hello world" {
		t.Errorf("CaptureStdout() = %q, want %q", got, "hello world")
	}
}

func TestCaptureStdout_MultipleWrites(t *testing.T) {
	got := CaptureStdout(t, func() {
		fmt.Fprint(os.Stdout, "line1")
		fmt.Fprint(os.Stdout, "line2")
	})
	if got != "line1line2" {
		t.Errorf("CaptureStdout() = %q, want %q", got, "line1line2")
	}
}
