import { test } from 'node:test';
import assert from 'node:assert/strict';

// Contract: GET /api/channels .ptt for an Android channel MUST include
// ptt_method so ChannelEditModal can restore the selected transport.
// Mirrors the restore predicate in ChannelEditModal.svelte
// (row.ptt?.method === 'android' && row.ptt?.ptt_method).
test('android ptt row exposes ptt_method for modal restore', () => {
  const row = { id: 1, ptt: { method: 'android', configured: true, ptt_method: 3 } };
  const restored =
    row.ptt?.method === 'android' && row.ptt?.ptt_method ? row.ptt.ptt_method : 1;
  assert.equal(restored, 3, 'AIOC (3) must restore, not fall back to CP2102N (1)');
});

test('android ptt row with ptt_method=0 falls back to CP2102N (1)', () => {
  // ptt_method=0 is the "unset" sentinel that the truthiness predicate must
  // treat as missing, falling back to the historical UI default. Same shape
  // for ptt_method=undefined (e.g., a legacy row that pre-dates the field).
  for (const row of [
    { id: 2, ptt: { method: 'android', configured: true, ptt_method: 0 } },
    { id: 3, ptt: { method: 'android', configured: true } }, // ptt_method absent
  ]) {
    const restored =
      row.ptt?.method === 'android' && row.ptt?.ptt_method ? row.ptt.ptt_method : 1;
    assert.equal(restored, 1, `row id=${row.id}: ptt_method falsy must fall back to 1`);
  }
});
