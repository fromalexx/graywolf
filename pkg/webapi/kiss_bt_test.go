package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// fakeBtSource lets the bonded-BT handler tests control the source's
// response without standing up the platformsvc stack.
type fakeBtSource struct {
	devs []BondedBtDevice
	err  error
}

func (f *fakeBtSource) BondedBtDevices(_ context.Context) ([]BondedBtDevice, error) {
	return f.devs, f.err
}

// TestGetBondedBtDevices_Android_ReturnsList covers the happy path:
// SetBtSource installed, handler returns 200 with the bonded list.
func TestGetBondedBtDevices_Android_ReturnsList(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetBtSource(&fakeBtSource{devs: []BondedBtDevice{
		{MAC: "AA:BB:CC:00:00:01", Name: "Mobilinkd TNC4"},
	}})

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/bonded-bt-devices", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var resp BondedBtDevicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Devices) != 1 || resp.Devices[0].Name != "Mobilinkd TNC4" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Devices[0].MAC != "AA:BB:CC:00:00:01" {
		t.Fatalf("unexpected MAC: %q", resp.Devices[0].MAC)
	}
}

// TestGetBondedBtDevices_NonAndroid_Returns501 verifies that desktop /
// non-Android builds (no SetBtSource call) return 501 Not Implemented
// with a JSON webtypes.ErrorResponse body — not a 500 with a nil-deref
// or an empty list that pretends Bluetooth is supported.
func TestGetBondedBtDevices_NonAndroid_Returns501(t *testing.T) {
	srv, _ := newTestServer(t) // no SetBtSource call
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/bonded-bt-devices", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json content-type, got %q", ct)
	}
	var errResp webtypes.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error body: %v body=%s", err, rec.Body)
	}
	if errResp.Error == "" {
		t.Fatalf("expected non-empty error field in 501 body, got %s", rec.Body)
	}
}

// TestGetBondedBtDevices_SourceError_Returns500 confirms that errors from
// the source bubble up as 500 with a JSON webtypes.ErrorResponse body.
// internalError deliberately strips the raw error from the response (and
// logs it server-side instead) to avoid leaking driver/stack strings to
// callers, so we assert only that the body is JSON-shaped with a
// non-empty sanitized error message.
func TestGetBondedBtDevices_SourceError_Returns500(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetBtSource(&fakeBtSource{err: errors.New("boom")})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/bonded-bt-devices", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json content-type, got %q", ct)
	}
	var errResp webtypes.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error body: %v body=%s", err, rec.Body)
	}
	if errResp.Error == "" {
		t.Fatalf("expected non-empty error field in 500 body, got %s", rec.Body)
	}
	// internalError must NOT echo the raw underlying error to the client
	// (it logs it server-side instead). Guard against accidental regression.
	if strings.Contains(errResp.Error, "boom") {
		t.Fatalf("500 body leaked raw source error: %q", errResp.Error)
	}
}

// TestGetBondedBtDevices_EmptyList_ReturnsJSONArray pins the
// never-serialize-null contract: an Android device with zero bonds must
// return [] so the UI's array-iteration code doesn't have to special-case
// null.
func TestGetBondedBtDevices_EmptyList_ReturnsJSONArray(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetBtSource(&fakeBtSource{}) // nil slice from source
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/bonded-bt-devices", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	// Raw-body check: must contain "devices":[] and must NOT contain
	// "devices":null. json.Unmarshal happily accepts both, so we have to
	// inspect the wire shape directly.
	body := rec.Body.String()
	if strings.Contains(body, `"devices":null`) {
		t.Fatalf("expected empty array on wire, got null: %s", body)
	}
	if !strings.Contains(body, `"devices":[]`) {
		t.Fatalf("expected empty array on wire, got: %s", body)
	}
	var resp BondedBtDevicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Devices == nil {
		t.Fatal("expected empty slice, got nil — never serialize null")
	}
	if len(resp.Devices) != 0 {
		t.Fatalf("expected zero devices, got %d", len(resp.Devices))
	}
}
