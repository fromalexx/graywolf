// web/src/lib/remote_actions/reply_match.js
//
// In-memory correlation between outbound `@@<otp>#cmd` fires and the
// inbound replies they elicit. Replies arriving from the same peer
// within REPLY_WINDOW_MS of an outbound fire are flagged so the
// MessageBubble can render the zap-reply adornment.
//
// Mitigation against false positives: a candidate reply is only
// flagged when its body starts with one of STATUS_PREFIXES -- the same
// status enum the receiver runner emits (pkg/actions/types.go). This
// trades a small recall miss (a free-form remark from the peer that
// happens to start with "ok") for the safer direction.
//
// State is a per-tab Map keyed by (peer, action_name); each value is
// the latest sent_at_ms. The map is bounded loosely -- keys self-expire
// once their entries are older than REPLY_WINDOW_MS the next time
// isActionReply runs against them, so we don't grow unboundedly.

export const REPLY_WINDOW_MS = 60_000;

export const STATUS_PREFIXES = [
  'ok',
  'error:',
  'bad_otp',
  'bad_arg',
  'denied',
  'unknown',
  'disabled',
  'busy',
  'rate_limited',
  'timeout',
  'no_credential',
];

const fires = new Map(); // key: peer, value: { lastSentAtMs }

export function recordOutboundFire(peer, actionName, sentAtMs = Date.now()) {
  if (!peer) return;
  fires.set(peer.toUpperCase(), { lastSentAtMs: sentAtMs, actionName });
}

export function isActionReply(msg) {
  if (!msg || msg.direction !== 'in') return false;
  const peer = (msg.from_call || '').toUpperCase();
  const entry = fires.get(peer);
  if (!entry) return false;
  const replyMs = msg.created_at ? new Date(msg.created_at).getTime() : Date.now();
  if (replyMs - entry.lastSentAtMs > REPLY_WINDOW_MS) {
    fires.delete(peer);
    return false;
  }
  const text = (msg.text || '').trim().toLowerCase();
  return STATUS_PREFIXES.some((p) => text.startsWith(p));
}

// statusFromText returns the matching prefix (lower-cased) for use by
// the badge color. Falls back to 'ok' when isActionReply returned true
// but the prefix can't be determined (defensive).
export function statusFromText(text) {
  const t = (text || '').trim().toLowerCase();
  for (const p of STATUS_PREFIXES) {
    if (t.startsWith(p)) return p.replace(':', '');
  }
  return 'ok';
}
