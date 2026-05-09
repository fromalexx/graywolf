package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// seedBeaconForSend creates a minimal beacon row through the API so the
// /send route's existence check (s.store.GetBeacon) finds it.
func seedBeaconForSend(t *testing.T, mux *http.ServeMux) uint32 {
	t.Helper()
	body := `{
		"type":"position",
		"channel":1,
		"callsign":"N0CAL",
		"destination":"APGRWO",
		"path":"WIDE1-1",
		"latitude":37.5,
		"longitude":-122.0,
		"symbol_table":"/",
		"symbol":">",
		"interval":1800,
		"enabled":true
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID uint32 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.ID
}

// TestBeaconSend_BuildErrorReturns422 verifies that when the beacon
// scheduler reports a build failure (e.g. issue-99: use_gps without a
// fix), the /send handler returns 422 Unprocessable Entity with the
// underlying reason in the JSON error body. Today this returns 200
// "sent" — the UI then toasts a misleading success.
func TestBeaconSend_BuildErrorReturns422(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedBeaconForSend(t, mux)

	srv.SetBeaconSendNow(func(context.Context, uint32) error {
		return &beacon.SendNowError{
			Kind: beacon.SendNowErrorBuild,
			Err:  errors.New("position beacon: use_gps set but no GPS fix available"),
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/beacons/"+strconv.FormatUint(uint64(id), 10)+"/send", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(er.Error, "GPS fix") {
		t.Errorf("error body = %q; want substring %q", er.Error, "GPS fix")
	}
}

// TestBeaconSend_EncodeErrorReturns422 verifies that an AX.25 encode
// failure (malformed callsign) is also 422 — the operator's
// configuration is the cause and they need to see the reason.
func TestBeaconSend_EncodeErrorReturns422(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedBeaconForSend(t, mux)

	srv.SetBeaconSendNow(func(context.Context, uint32) error {
		return &beacon.SendNowError{
			Kind: beacon.SendNowErrorEncode,
			Err:  errors.New("ax25: invalid callsign \"BAD!\""),
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/beacons/"+strconv.FormatUint(uint64(id), 10)+"/send", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBeaconSend_ChannelModeReturns409 verifies that attempting to
// beacon on a packet-mode channel returns 409 Conflict with a clear
// reason — channel state conflicts with the request.
func TestBeaconSend_ChannelModeReturns409(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedBeaconForSend(t, mux)

	srv.SetBeaconSendNow(func(context.Context, uint32) error {
		return &beacon.SendNowError{
			Kind: beacon.SendNowErrorChannelMode,
			Err:  errors.New("channel 1 is in packet mode and cannot transmit APRS beacons"),
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/beacons/"+strconv.FormatUint(uint64(id), 10)+"/send", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&er)
	if !strings.Contains(er.Error, "packet mode") {
		t.Errorf("error body = %q; want substring %q", er.Error, "packet mode")
	}
}

// TestBeaconSend_SubmitErrorReturns503 verifies that a TX governor
// failure (queue full, etc.) is 503 — the failure is transient and
// retrying may succeed once the queue drains.
func TestBeaconSend_SubmitErrorReturns503(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedBeaconForSend(t, mux)

	srv.SetBeaconSendNow(func(context.Context, uint32) error {
		return &beacon.SendNowError{
			Kind: beacon.SendNowErrorSubmit,
			Err:  txgovernor.ErrQueueFull,
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/beacons/"+strconv.FormatUint(uint64(id), 10)+"/send", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBeaconSend_SuccessReturns200 guards the happy path.
func TestBeaconSend_SuccessReturns200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedBeaconForSend(t, mux)

	srv.SetBeaconSendNow(func(context.Context, uint32) error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/api/beacons/"+strconv.FormatUint(uint64(id), 10)+"/send", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

