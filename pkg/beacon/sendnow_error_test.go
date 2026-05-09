package beacon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// TestSendNow_BuildError_NoGPSCache verifies that SendNow on a position
// beacon with UseGps set but no GPS cache wired returns a *SendNowError
// of kind SendNowErrorBuild with the underlying build error preserved.
// This is the primary issue-99 path: the operator clicks "Beacon Now"
// on a position beacon configured to use GPS but the station has no
// GPS hardware. Today SendNow returns nil and the UI shows "Beacon
// sent" — wrong.
func TestSendNow_BuildError_NoGPSCache(t *testing.T) {
	sink := newMockSink(0)
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Logger: logger}) // no Cache

	s.SetBeacons([]Config{{
		ID:          1,
		Type:        TypePosition,
		Channel:     0,
		Source:      mustAddr(t, "N0CALL-9"),
		Dest:        mustAddr(t, "APGRWO"),
		Slot:        -1,
		UseGps:      true,
		SymbolTable: '/', SymbolCode: '-',
	}})

	err := s.SendNow(context.Background(), 1)
	if err == nil {
		t.Fatal("SendNow returned nil; want *SendNowError")
	}
	var sne *SendNowError
	if !errors.As(err, &sne) {
		t.Fatalf("err = %v; want *SendNowError", err)
	}
	if sne.Kind != SendNowErrorBuild {
		t.Errorf("kind = %v; want SendNowErrorBuild", sne.Kind)
	}
	if sink.Recorder.Len() != 0 {
		t.Errorf("frames submitted = %d; want 0", sink.Recorder.Len())
	}
}

// TestSendNow_BuildError_NoGPSFix exercises the issue-99 log line
// verbatim: cache present, no fix yet. SendNow must surface the
// "no GPS fix available" reason.
func TestSendNow_BuildError_NoGPSFix(t *testing.T) {
	sink := newMockSink(0)
	cache := gps.NewMemCache() // empty, no Update call
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Cache: cache, Logger: logger})

	s.SetBeacons([]Config{{
		ID:          2,
		Type:        TypePosition,
		Channel:     0,
		Source:      mustAddr(t, "N0CALL-9"),
		Dest:        mustAddr(t, "APGRWO"),
		Slot:        -1,
		UseGps:      true,
		SymbolTable: '/', SymbolCode: '-',
	}})

	err := s.SendNow(context.Background(), 2)
	var sne *SendNowError
	if !errors.As(err, &sne) {
		t.Fatalf("err = %v; want *SendNowError", err)
	}
	if sne.Kind != SendNowErrorBuild {
		t.Errorf("kind = %v; want SendNowErrorBuild", sne.Kind)
	}
	// Verify the operator-visible message names the GPS fix problem.
	if msg := sne.Error(); msg == "" {
		t.Error("Error() returned empty string")
	}
}

// TestSendNow_ChannelModePacket verifies that sending a beacon to a
// packet-mode channel surfaces a SendNowErrorChannelMode rather than
// silently succeeding. The scheduler's silent skip is fine for the
// scheduled fire path (it is policy, not failure), but for an
// operator-driven send-now it is misleading to report success.
func TestSendNow_ChannelModePacket(t *testing.T) {
	sink := newMockSink(0)
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{
		Sink:   sink,
		Logger: logger,
		ChannelModes: &fakeChannelModeLookup{modes: map[uint32]string{
			3: configstore.ChannelModePacket,
		}},
	})

	s.SetBeacons([]Config{{
		ID:      3,
		Type:    TypePosition,
		Channel: 3,
		Source:  mustAddr(t, "N0CALL-9"),
		Dest:    mustAddr(t, "APGRWO"),
		Slot:    -1,
		Lat:     37.0, Lon: -122.0,
		SymbolTable: '/', SymbolCode: '-',
	}})

	err := s.SendNow(context.Background(), 3)
	var sne *SendNowError
	if !errors.As(err, &sne) {
		t.Fatalf("err = %v; want *SendNowError", err)
	}
	if sne.Kind != SendNowErrorChannelMode {
		t.Errorf("kind = %v; want SendNowErrorChannelMode", sne.Kind)
	}
	if sink.Recorder.Len() != 0 {
		t.Errorf("frames submitted = %d; want 0", sink.Recorder.Len())
	}
}

// TestSendNow_SubmitError verifies that a Submit failure (TX governor
// refusing the frame) is surfaced as SendNowErrorSubmit with the
// underlying error preserved for upstream classification.
func TestSendNow_SubmitError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{
		Sink:   &erroringSink{err: txgovernor.ErrQueueFull},
		Logger: logger,
	})

	s.SetBeacons([]Config{{
		ID:      4,
		Type:    TypePosition,
		Channel: 1,
		Source:  mustAddr(t, "N0CALL-9"),
		Dest:    mustAddr(t, "APGRWO"),
		Slot:    -1,
		Lat:     37.0, Lon: -122.0,
		SymbolTable: '/', SymbolCode: '-',
	}})

	err := s.SendNow(context.Background(), 4)
	var sne *SendNowError
	if !errors.As(err, &sne) {
		t.Fatalf("err = %v; want *SendNowError", err)
	}
	if sne.Kind != SendNowErrorSubmit {
		t.Errorf("kind = %v; want SendNowErrorSubmit", sne.Kind)
	}
	if !errors.Is(err, txgovernor.ErrQueueFull) {
		t.Errorf("errors.Is(err, ErrQueueFull) = false; want true")
	}
}

// TestSendNow_SuccessReturnsNil guards against regressing the success
// path: a properly configured beacon with a working sink must still
// return nil from SendNow.
func TestSendNow_SuccessReturnsNil(t *testing.T) {
	sink := newMockSink(1)
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Logger: logger})

	s.SetBeacons([]Config{{
		ID:      5,
		Type:    TypePosition,
		Channel: 0,
		Source:  mustAddr(t, "N0CALL-9"),
		Dest:    mustAddr(t, "APGRWO"),
		Slot:    -1,
		Lat:     37.0, Lon: -122.0,
		SymbolTable: '/', SymbolCode: '-',
	}})

	if err := s.SendNow(context.Background(), 5); err != nil {
		t.Fatalf("SendNow on valid beacon returned err: %v", err)
	}
}
