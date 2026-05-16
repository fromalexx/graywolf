import test from 'node:test';
import assert from 'node:assert/strict';
import { pickDefaultSampleRate } from './sampleRate.js';

test('prefers 48000 when offered', () => {
  assert.equal(pickDefaultSampleRate([8000, 11025, 16000, 22050, 44100, 48000]), 48000);
  // Even when a higher (corrupt) rate is present, never pick above 48k.
  assert.equal(pickDefaultSampleRate([48000, 96000]), 48000);
});

test('falls back to 44100 when 48000 absent', () => {
  assert.equal(pickDefaultSampleRate([8000, 44100]), 44100);
});

test('otherwise picks the highest rate at or below 48000', () => {
  assert.equal(pickDefaultSampleRate([8000, 16000, 32000]), 32000);
});

test('never returns a rate above 48000; defaults safely', () => {
  assert.equal(pickDefaultSampleRate([96000, 192000]), 48000);
  assert.equal(pickDefaultSampleRate([]), 48000);
  assert.equal(pickDefaultSampleRate(undefined), 48000);
  assert.equal(pickDefaultSampleRate(null), 48000);
});
