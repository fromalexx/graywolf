package flareschema

// AudioDevices is the top-level shape emitted by
// `graywolf-modem --list-audio`. It is also the schema attached to the
// flare's audio_devices section.
//
// Multi-host: cpal exposes one Host per audio API on the platform (ALSA,
// PulseAudio, JACK on Linux; CoreAudio on macOS; WASAPI on Windows). We
// iterate available_hosts() so a JACK-on-Linux user's report carries
// both ALSA and JACK devices, not whichever host the modem picked at
// runtime.
type AudioDevices struct {
	Hosts  []AudioHost      `json:"hosts"`
	Issues []CollectorIssue `json:"issues,omitempty"`
}

// AudioHost is one cpal host. ID is the cpal-internal host identifier
// (e.g. "alsa", "jack", "coreaudio"); Name is the display name from the
// host trait. IsDefault marks the host cpal::default_host() picked.
type AudioHost struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	IsDefault bool          `json:"is_default"`
	Devices   []AudioDevice `json:"devices"`
}

// AudioDevice is one cpal device under a host. Direction is "input" or
// "output". IsDefault marks the host's default-input / default-output
// pick (each direction has its own default).
//
// Recommended is the flare-side string heuristic only: set when the
// device's PCM identifier is a stable `plughw:CARD=<name>` form.
//
// For capture devices this intentionally does NOT match the live web
// picker. The runtime picker probes each device and
// recommends the PCM form that actually streams (cheap USB chips fail
// on plughw: and only work via raw hw:). `--list-audio` runs as a
// separate short-lived process and cannot probe safely, so its
// Recommended stays a cheap triage hint; the live picker is
// authoritative. Keep in sync with the doc on graywolf-modem
// is_recommended_pcm_id and the convergence-test note.
type AudioDevice struct {
	Name             string                   `json:"name"`
	Direction        string                   `json:"direction"`
	IsDefault        bool                     `json:"is_default"`
	Recommended      bool                     `json:"recommended,omitempty"`
	SupportedConfigs []AudioStreamConfigRange `json:"supported_configs,omitempty"`
}

// AudioStreamConfigRange flattens cpal::SupportedStreamConfigRange into
// a JSON-friendly shape. Channels is fixed (cpal reports per-config
// channel count), but sample rate is a range. SampleFormat is one of
// "i16", "u16", "f32" (the cpal SampleFormat variants).
type AudioStreamConfigRange struct {
	Channels        uint16 `json:"channels"`
	MinSampleRateHz uint32 `json:"min_sample_rate_hz"`
	MaxSampleRateHz uint32 `json:"max_sample_rate_hz"`
	SampleFormat    string `json:"sample_format"`
}
