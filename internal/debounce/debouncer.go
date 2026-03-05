package debounce

import "time"

const defaultDebounceDelay = 3 * time.Second

// Mode defines debounce scheduling behavior.
type Mode int

const (
	// ModeLeading schedules on first trigger in a burst.
	ModeLeading Mode = iota
	// ModeTrailing schedules on each trigger, superseding older schedules.
	ModeTrailing
)

// TriggerResult describes scheduling decision for a trigger.
type TriggerResult struct {
	Schedule bool
	Seq      int
	Delay    time.Duration
}

// State exposes debouncer runtime state for inspection/tests.
type State struct {
	Pending  bool
	Armed    bool
	InFlight bool
	Seq      int
}

// Debouncer coordinates trigger bursts and due acceptance.
type Debouncer struct {
	delay    time.Duration
	mode     Mode
	pending  bool
	armed    bool
	inFlight bool
	seq      int
}

// New creates a Debouncer with the given delay and mode.
func New(delay time.Duration, mode Mode) Debouncer {
	if delay <= 0 {
		delay = defaultDebounceDelay
	}
	return Debouncer{
		delay: delay,
		mode:  mode,
	}
}

// Trigger marks pending work and returns whether a due schedule should be set.
func (d *Debouncer) Trigger() TriggerResult {
	if d == nil {
		return TriggerResult{}
	}
	d.pending = true
	if d.inFlight {
		return TriggerResult{}
	}

	switch d.mode {
	case ModeTrailing:
		d.seq++
		d.armed = true
		return TriggerResult{
			Schedule: true,
			Seq:      d.seq,
			Delay:    d.delay,
		}
	case ModeLeading:
		fallthrough
	default:
		if d.armed {
			return TriggerResult{}
		}
		d.seq++
		d.armed = true
		return TriggerResult{
			Schedule: true,
			Seq:      d.seq,
			Delay:    d.delay,
		}
	}
}

// AcceptDue validates a due sequence and returns whether work should start.
func (d *Debouncer) AcceptDue(seq int) bool {
	if d == nil {
		return false
	}
	if seq != d.seq || !d.armed || !d.pending || d.inFlight {
		return false
	}
	d.armed = false
	return true
}

// MarkStarted marks the work as in-flight.
func (d *Debouncer) MarkStarted() {
	if d == nil {
		return
	}
	d.inFlight = true
}

// MarkFinished clears pending/armed state after work completion.
func (d *Debouncer) MarkFinished() {
	if d == nil {
		return
	}
	d.inFlight = false
	d.pending = false
	d.armed = false
}

// State returns a copy of current debouncer state.
func (d *Debouncer) State() State {
	if d == nil {
		return State{}
	}
	return State{
		Pending:  d.pending,
		Armed:    d.armed,
		InFlight: d.inFlight,
		Seq:      d.seq,
	}
}
