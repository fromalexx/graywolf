package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestCreateAndListRemoteActionMacros(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.RemoteActionMacroRequest{
		TargetCall: "kk7xyz-9", // lowercase: server must uppercase
		Label:      "unlock front",
		ActionName: "unlock",
		ArgsString: "door=front",
	})
	req := httptest.NewRequest("POST", "/api/remote-actions/macros", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/remote-actions/macros?target=KK7XYZ-9", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d", rr.Code)
	}
	var got []dto.RemoteActionMacro
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got) != 1 || got[0].TargetCall != "KK7XYZ-9" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestCreateMacroRejectsInvalidActionName(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.RemoteActionMacroRequest{
		TargetCall: "K", Label: "x", ActionName: "bad/name",
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/remote-actions/macros", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestReorderMacros(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	for i, lbl := range []string{"a", "b", "c"} {
		body, _ := json.Marshal(dto.RemoteActionMacroRequest{
			TargetCall: "K", Label: lbl, ActionName: "x", Position: i,
		})
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/remote-actions/macros", bytes.NewReader(body))
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d", i, rr.Code)
		}
	}
	body, _ := json.Marshal(dto.RemoteActionMacroReorderRequest{TargetCall: "K", IDs: []uint{3, 2, 1}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/remote-actions/macros/reorder", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("reorder: %d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/remote-actions/macros?target=K", nil)
	mux.ServeHTTP(rr, req)
	var got []dto.RemoteActionMacro
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got[0].ID != 3 || got[2].ID != 1 {
		t.Fatalf("reorder did not stick: %+v", got)
	}
}
