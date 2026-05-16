package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateClampAudioSampleRate verifies corrupt >48 kHz sample rates
// (the plughw 96000/192000 trap) are clamped to 48000, while sane rows
// are left exactly as-is. A second invocation must be a no-op.
func TestMigrateClampAudioSampleRate(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "clamp_sr.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	rows := []struct {
		id   uint32
		name string
		rate uint32
	}{
		{1, "aioc-corrupt-96k", 96000},
		{2, "digirig-corrupt-192k", 192000},
		{3, "good-48k", 48000},
		{4, "good-44k1", 44100},
		{5, "good-8k", 8000},
	}
	for _, r := range rows {
		if err := store.DB().Exec(
			`INSERT INTO audio_devices(id, name, direction, source_type, sample_rate,
			created_at, updated_at)
			VALUES (?, ?, 'input', 'soundcard', ?, datetime('now'), datetime('now'))`,
			r.id, r.name, r.rate).Error; err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
	}

	if err := migrateClampAudioSampleRate(store.DB()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	check := func(id uint32, want uint32) {
		t.Helper()
		var got uint32
		if err := store.DB().Raw(
			`SELECT sample_rate FROM audio_devices WHERE id=?`, id).Scan(&got).Error; err != nil {
			t.Fatalf("scan id=%d: %v", id, err)
		}
		if got != want {
			t.Errorf("id=%d: sample_rate=%d, want %d", id, got, want)
		}
	}

	check(1, 48000) // 96000 -> clamped
	check(2, 48000) // 192000 -> clamped
	check(3, 48000) // untouched
	check(4, 44100) // untouched
	check(5, 8000)  // untouched

	// Idempotence: second run changes nothing.
	if err := migrateClampAudioSampleRate(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
	check(1, 48000)
	check(4, 44100)
}
