package beacon

// SendNowErrorKind classifies a SendNow failure so callers (notably the
// REST API handler) can map it to an appropriate HTTP status without
// string-matching the wrapped error.
type SendNowErrorKind int

const (
	// SendNowErrorBuild is a beacon-build failure: the operator's
	// configuration is internally consistent but cannot produce a frame
	// right now (no GPS fix, fixed coords 0/0, missing PHG, etc.).
	SendNowErrorBuild SendNowErrorKind = iota
	// SendNowErrorEncode is an AX.25 frame-encode failure, almost
	// always a malformed callsign.
	SendNowErrorEncode
	// SendNowErrorChannelMode means the target channel is in
	// packet-only mode and cannot transmit APRS beacons. The scheduled
	// fire path skips silently; SendNow surfaces this as an error so
	// the operator's explicit click does not appear to succeed.
	SendNowErrorChannelMode
	// SendNowErrorSubmit means the TX governor refused the frame
	// (queue full, deadline, etc.). The wrapped Err preserves the
	// original sentinel so callers can errors.Is against
	// txgovernor.ErrQueueFull, context.DeadlineExceeded, etc.
	SendNowErrorSubmit
)

func (k SendNowErrorKind) String() string {
	switch k {
	case SendNowErrorBuild:
		return "build"
	case SendNowErrorEncode:
		return "encode"
	case SendNowErrorChannelMode:
		return "channel_mode"
	case SendNowErrorSubmit:
		return "submit"
	default:
		return "unknown"
	}
}

// SendNowError is the typed error returned by Scheduler.SendNow when
// an explicit operator-driven send fails. Kind selects the failure
// category; Err preserves the underlying cause for unwrap/errors.Is.
type SendNowError struct {
	Kind SendNowErrorKind
	Err  error
}

func (e *SendNowError) Error() string {
	if e.Err == nil {
		return e.Kind.String()
	}
	return e.Err.Error()
}

func (e *SendNowError) Unwrap() error { return e.Err }
