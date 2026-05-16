package configstore

import "gorm.io/gorm"

// migrateClampAudioSampleRate repairs audio_devices rows whose persisted
// sample_rate exceeds the modem's operating ceiling (48 kHz).
//
// An ALSA `plughw:`/`default` PCM advertises a synthetic resample range
// (up to 192 kHz) even though the USB codec runs at 48 kHz. The Audio
// Devices form used to auto-fill the highest advertised rate, so operators
// who ran "Detect Devices" persisted sample_rate=96000. At runtime the
// modem opened the stream at the inflated rate while the hardware ran
// 48 kHz; the demodulator clocked bit timing against the wrong rate and
// every frame failed FCS -- RX went silent with no error.
//
// 48 kHz serves every Graywolf modem mode, so clamping is lossless.
// Idempotent: rows already at or below 48 kHz are left untouched.
func migrateClampAudioSampleRate(tx *gorm.DB) error {
	return tx.Exec(
		"UPDATE audio_devices SET sample_rate = 48000 WHERE sample_rate > 48000").Error
}
