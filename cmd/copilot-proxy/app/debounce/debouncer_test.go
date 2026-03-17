package debounce

import (
	"testing"
	"time"
)

func TestLeading_FirstTriggerSchedulesOnce(t *testing.T) {
	d := New(3*time.Second, ModeLeading)
	first := d.Trigger()
	if !first.Schedule {
		t.Fatalf("expected first leading trigger to schedule")
	}
	if first.Seq != 1 {
		t.Fatalf("expected first leading seq=1, got %d", first.Seq)
	}
	if first.Delay != 3*time.Second {
		t.Fatalf("expected trigger delay 3s, got %v", first.Delay)
	}

	second := d.Trigger()
	if second.Schedule {
		t.Fatalf("expected second leading trigger not to reschedule")
	}
	if second.Seq != 0 {
		t.Fatalf("expected no seq on ignored trigger, got %d", second.Seq)
	}
	if d.State().Seq != 1 {
		t.Fatalf("expected leading debouncer seq to remain 1, got %d", d.State().Seq)
	}
}

func TestTrailing_AdditionalTriggerReschedulesAndBumpsSeq(t *testing.T) {
	d := New(3*time.Second, ModeTrailing)

	first := d.Trigger()
	if !first.Schedule || first.Seq != 1 {
		t.Fatalf("expected first trailing trigger schedule seq=1, got %+v", first)
	}

	second := d.Trigger()
	if !second.Schedule || second.Seq != 2 {
		t.Fatalf("expected second trailing trigger reschedule seq=2, got %+v", second)
	}
}

func TestAcceptDue_RejectsStaleSeq(t *testing.T) {
	d := New(3*time.Second, ModeLeading)
	trigger := d.Trigger()
	if !trigger.Schedule {
		t.Fatalf("expected trigger to schedule")
	}

	if d.AcceptDue(trigger.Seq + 1) {
		t.Fatalf("expected stale seq to be rejected")
	}
}

func TestAcceptDue_AllowsValidSeqWhenPending(t *testing.T) {
	d := New(3*time.Second, ModeLeading)
	trigger := d.Trigger()
	if !trigger.Schedule {
		t.Fatalf("expected trigger to schedule")
	}

	if !d.AcceptDue(trigger.Seq) {
		t.Fatalf("expected current seq to be accepted")
	}
	if d.State().Armed {
		t.Fatalf("expected armed=false after due acceptance")
	}
}

func TestMarkFinished_ClearsPendingAndArmed(t *testing.T) {
	d := New(3*time.Second, ModeLeading)
	trigger := d.Trigger()
	if !trigger.Schedule {
		t.Fatalf("expected trigger to schedule")
	}
	if !d.AcceptDue(trigger.Seq) {
		t.Fatalf("expected due acceptance")
	}
	d.MarkStarted()
	d.MarkFinished()

	state := d.State()
	if state.Pending || state.Armed || state.InFlight {
		t.Fatalf("expected cleared state after finish, got %+v", state)
	}
}

func TestTrigger_WhenInFlight_NoSchedule(t *testing.T) {
	d := New(3*time.Second, ModeLeading)
	trigger := d.Trigger()
	if !trigger.Schedule {
		t.Fatalf("expected trigger to schedule")
	}
	if !d.AcceptDue(trigger.Seq) {
		t.Fatalf("expected due acceptance")
	}
	d.MarkStarted()

	next := d.Trigger()
	if next.Schedule {
		t.Fatalf("expected in-flight trigger not to schedule")
	}
	if !d.State().Pending {
		t.Fatalf("expected pending=true while in-flight and retriggered")
	}
}
