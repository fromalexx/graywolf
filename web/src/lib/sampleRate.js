// Pure helper: choose a safe default sample rate for the Audio Devices
// form. Kept Svelte-free so node --test can exercise it.
//
// Never returns a rate above 48 kHz. A corrupt persisted 96000 (from an
// ALSA plughw device that advertises a synthetic resample range while the
// codec really runs 48 kHz) desyncs the modem demodulator — every frame
// fails FCS and RX goes silent. 48 kHz serves every Graywolf modem mode.
const MAX = 48000;

export function pickDefaultSampleRate(rates) {
  const list = Array.isArray(rates) ? rates.filter((r) => Number.isFinite(r)) : [];
  if (list.includes(MAX)) return MAX;
  if (list.includes(44100)) return 44100;
  const atOrBelow = list.filter((r) => r <= MAX);
  if (atOrBelow.length > 0) return Math.max(...atOrBelow);
  return MAX;
}
