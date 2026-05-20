package platformsvc

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestSchemaVersionMatchesKotlin parses GraywolfService.kt for its
// `schemaVersion = N` literal and asserts it equals the Go-side
// SchemaVersion constant. The platform-service handshake fails fast on
// mismatch (ERROR_SCHEMA_MISMATCH); this test fires earlier, at build
// time, so a forgotten Kotlin bump can't slip into a release.
func TestSchemaVersionMatchesKotlin(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)
	repoRoot := filepath.Dir(filepath.Dir(pkgDir))

	kotlinPath := filepath.Join(repoRoot, "android", "app", "src", "main", "kotlin",
		"com", "nw5w", "graywolf", "GraywolfService.kt")
	content, err := os.ReadFile(kotlinPath)
	if err != nil {
		t.Fatalf("read %s: %v", kotlinPath, err)
	}

	// PlatformServer(... schemaVersion = N, ...) — the value passed at
	// PlatformServer construction is the source of truth on the Kotlin side.
	pattern := regexp.MustCompile(`schemaVersion\s*=\s*(\d+)`)
	matches := pattern.FindAllStringSubmatch(string(content), -1)
	if len(matches) == 0 {
		t.Fatalf("no `schemaVersion = N` literal found in %s", kotlinPath)
	}

	for _, m := range matches {
		var v uint32
		if _, err := fmt.Sscanf(m[1], "%d", &v); err != nil {
			t.Fatalf("parse %q: %v", m[1], err)
		}
		if v != SchemaVersion {
			t.Fatalf("Kotlin schemaVersion = %d, Go SchemaVersion = %d (in %s).\n"+
				"Bump both sides together when changing the wire schema.",
				v, SchemaVersion, kotlinPath)
		}
	}
}
