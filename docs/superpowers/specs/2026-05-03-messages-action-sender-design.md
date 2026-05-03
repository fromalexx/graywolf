# Messages -> Action Sender (Outbound Actions)

**Date:** 2026-05-03
**Author:** brainstorm with operator
**Status:** Design accepted; ready for implementation plan

## 1. Problem

Graywolf 0.13.0 shipped the inbound Actions feature: messages of the form
`@@<otp>#<action> [k=v ...]` arriving from the air or APRS-IS divert into
the runner, execute a local command or webhook, and reply on-air. That
covers half the loop. There is no operator-facing way to *send* such a
message at another station from inside Graywolf -- the only options today
are typing the line into the regular Messages compose bar (and computing
the TOTP yourself) or running a third-party authenticator app on the
side.

This design adds an in-Messages affordance for firing Actions at remote
stations, with stored macros and an optional shared TOTP credential
store so the operator never has to retype an OTP.

## 2. Goals and non-goals

### Goals

- Operator can fire `@@<otp>#cmd args=` invocations at any callsign from
  inside a DM thread without leaving Messages.
- Per-peer reusable macros: "open garage" tile under the KK7XYZ-9 thread.
- Optional shared TOTP credential store: one base32 secret per remote
  station, generic name like `NW5W OTP`, reused by every macro that
  targets that station.
- OTP auto-injection at fire time when a credential is bound; manual
  one-shot OTP entry when no credential exists.
- Cooldown until the next TOTP step so the operator cannot accidentally
  fire two requests inside the same 30 s window and hit the receiver's
  replay ring.
- Replies arriving in the thread are visually badged so operators can
  scan past chat for the Action result.
- Reuse the existing `POST /messages` send path so an outbound Action is
  indistinguishable on the wire from a hand-typed line.

### Non-goals

- No target-side `arg_schema` discovery. We never know the remote
  station's argument schema, so all validation is on the receiver.
- No macro sharing across operators or import / export bundles.
- No support for tactical / broadcast addressees as Action targets.
  Firing a single OTP-protected command at multiple receivers is an
  operational footgun.
- No relocation of the existing Delete pill in the Messages toolbar
  (separately tracked as a follow-up; the new toolbar entry point may
  influence the layout, so the relocation is deferred to that work).
- No rewrite of the existing inbound `pkg/actions/` package. Outbound
  lives next to it as a sibling, not on top of it.

## 3. Glossary

| Term | Meaning |
|---|---|
| Outbound Actions | This feature. Operator -> remote station. |
| Inbound Actions | Existing 0.13.0 feature. Remote station -> operator. |
| Macro | A saved (target, action, args, optional credential) tuple shown as a tile in the drawer. |
| Remote OTP credential | A named base32 TOTP secret stored locally; used to compute one-time codes when firing macros. Generic name (e.g. `NW5W OTP`). |
| Drawer | The slide-in panel anchored to the right (desktop) or bottom (mobile) of an open DM thread. |

## 4. UX

### 4.1 Entry point

`MessageThread` headers gain a small zap icon (lightning) next to the
existing thread controls. Visible only when `thread.kind === 'dm'`.
Click toggles the drawer.

In tactical threads the icon is omitted entirely (not just disabled).
Multi-recipient Action fire is out of scope and the missing affordance
is the right signal.

### 4.2 Drawer at rest (fire mode)

```
+----------------------------------+
| MACROS . KK7XYZ-9          [gear]|
+----------------------------------+
| [zap] unlock door=front          |
| [zap] status                     |
| [zap] reboot                     |
+----------------------------------+
| FREE-FORM                        |
| Active OTP: [ NW5W OTP    v ]    |
| [ action [k=v ...]            ]  |
| OTP 418273 . next 14s            |
| Save as macro...                 |
| [ SEND ACTION (yellow) ]         |
+----------------------------------+
```

- Macros sorted by `position` (drag-reorder index from edit mode).
- Tap a macro tile -> immediate fire. No second click. Each macro
  carries its own credential binding from its row, so different
  macros in the same drawer can use different credentials without
  the operator switching anything.
- Free-form input accepts `action [k=v ...]` only. The `@@<otp>#`
  prefix is added by the frontend at send time. Placeholder copy:
  `unlock door=front`. Helper text below the input: "Enter command and
  optional args -- no `@@` prefix needed."
- The "Active OTP" picker above the free-form input selects which
  credential to use for free-form fires. Defaults to the credential
  most-recently used against this peer (or the first credential
  alphabetically if none has been used yet). When set to "None" the
  drawer reveals a manual OTP entry field (six-digit numeric input)
  that replaces the auto-generated code line.
- "OTP <code> . next Ns" line shows the active credential's current
  code and seconds until the next step. Source: server endpoint
  `POST /api/remote-actions/otp/{credId}` returning
  `{code, expires_at}`, refreshed on a per-step ticker.
- "Save as macro..." promotes the current free-form input into the
  edit-mode form, prefilled (including the currently active
  credential).

### 4.3 Drawer in edit mode (gear pressed)

Header changes to `EDIT MACROS [Done]`. Each macro becomes a row with:

- Drag handle (==) on the left
- The macro label (tap to inline-edit: label / action+args / credential)
- Delete X on the right

A dashed `+ Add new macro` button at the bottom opens an empty inline
form. Done returns to fire mode and persists drag-reorder via
`PUT /api/remote-actions/macros/{id}` with the new position.

### 4.4 Credential picker (inside macro inline form)

```
OTP SECRET
[ NW5W OTP                  v ]
Manage secrets...
```

- Dropdown lists every credential by name plus a final
  "None (enter OTP at fire time)" option.
- "Manage secrets..." opens the credentials modal (4.5).
- Empty store -> only "None" plus the link.
- Naming convention surfaced in the credential-create modal placeholder:
  `<CALLSIGN> OTP` (e.g. `NW5W OTP`). Operators typically have one
  credential per remote station; the picker exists for the rare case
  where one station hosts multiple OTP issuers.

### 4.5 Credentials modal

Launched from the picker's "Manage secrets..." link (and also from a
top-level Messages entry point so it can be reached without opening a
macro). Implemented as a chonky `Dialog` over the Messages route, NOT
as a separate page route. Body mirrors `CredentialsTable.svelte` from
the inbound Actions page:

| Name | Algo | Created | Last used | Used by | Action |
|---|---|---|---|---|---|
| NW5W OTP | TOTP / SHA1 / 6 / 30s | 2 days ago | 11 min ago | 3 macros | Edit / Delete |

`+ New secret` opens a nested edit modal with: name (placeholder
`<CALLSIGN> OTP`), base32 secret (textarea, paste-friendly), algorithm
(default sha1), digits (default 6), period (default 30).

Delete is disabled when `used_by > 0`; the tooltip explains "Unbind
from N macro(s) first." (Same UX as the inbound credentials table.)

### 4.6 Reply badging

When an outbound message body starts with `@@`, the frontend records
`(peer, action_name, sent_at)` in a short-lived in-memory map. Inbound
from the same peer within 60 s of `sent_at` is flagged as the reply.

The `MessageBubble` for that inbound:

- Reduced background opacity (so it reads as metadata, not chat)
- A small `[zap] reply` tag in the bubble footer
- Status colour from `web/src/lib/actions/status.js#statusVariant`
  parsed from the reply text (`ok` green, `error: ...` red,
  `bad_otp:*` red, etc.)

The 60 s window starts at outbound `sent_at`, not ACK time, so an
unacked outbound still correlates with its reply.

### 4.7 Cooldown

After a fire, the source macro tile (or the free-form SEND when
free-form was used) goes disabled with the `next OTP in Ns` countdown
overlaid until the next TOTP step boundary.

The receiver's replay ring (in `pkg/actions/otp.go`) is the actual
authority. The client cooldown is UX polish that prevents the operator
from triggering visible `bad_otp:replay` replies.

Manual-OTP fires also cool down for the remainder of the current 30 s
step computed from the system clock.

## 5. Data model

Migration 16, raw SQL in `pkg/configstore/migrate_remote_actions.go`,
modelled on the existing migration 15 pattern. Models live in
`pkg/remoteactions/models.go`, NOT registered with AutoMigrate.

```sql
CREATE TABLE remote_otp_credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,            -- "NW5W OTP"
  secret_b32 TEXT NOT NULL,             -- plaintext per single-user-station
  algorithm TEXT NOT NULL DEFAULT 'sha1',
  digits INTEGER NOT NULL DEFAULT 6,
  period INTEGER NOT NULL DEFAULT 30,
  created_at DATETIME NOT NULL,
  last_used_at DATETIME
);

CREATE TABLE remote_action_macros (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  target_call TEXT NOT NULL,            -- uppercased on write
  label TEXT NOT NULL,                  -- "unlock front door"
  action_name TEXT NOT NULL,            -- "unlock"
  args_string TEXT NOT NULL DEFAULT '', -- "door=front"
  remote_otp_credential_id INTEGER,     -- FK, nullable
  position INTEGER NOT NULL DEFAULT 0,  -- drag-reorder index, low first
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (remote_otp_credential_id)
    REFERENCES remote_otp_credentials(id) ON DELETE SET NULL
);

CREATE INDEX idx_remote_action_macros_target_call
  ON remote_action_macros(target_call);
```

`ON DELETE SET NULL` so deleting a credential demotes its macros to
manual-OTP rather than orphaning them.

`secret_b32` stored plaintext per the single-user-station design (same
rationale as `otp_credentials` for inbound).

`last_used_at` updated by the OTP-generate endpoint. Useful for sorting
the picker so the most-recent credential floats to the top.

## 6. Backend

### 6.1 Package layout

```
pkg/remoteactions/
  models.go         (Credential, Macro)
  service.go        (composition root, startup/shutdown)
  store_macros.go   (CRUD, position reorder)
  store_creds.go    (CRUD with used_by computation)
  otp.go            (TOTP generation, RFC 6238)
  validate.go       (action name, base32 secret)
```

Service constructed in `pkg/app/wiring.go` after `wireMessages`,
non-fatal on construction failure (matches `wireActions`).

### 6.2 HTTP endpoints

In `pkg/webapi/remote_actions.go`:

| Method | Path | Body / Query | Returns |
|---|---|---|---|
| GET | `/api/remote-actions/credentials` | -- | `[{id,name,algorithm,digits,period,created_at,last_used_at,used_by:[targetCall...]}]` |
| POST | `/api/remote-actions/credentials` | `{name,secret_b32,algorithm?,digits?,period?}` | created cred |
| PUT | `/api/remote-actions/credentials/{id}` | partial update body | updated cred |
| DELETE | `/api/remote-actions/credentials/{id}` | -- | 204 (rejected with 409 if `used_by > 0`) |
| GET | `/api/remote-actions/macros` | `?target=KK7XYZ-9` (uppercased) | `[{id,target_call,label,action_name,args_string,remote_otp_credential_id,position}]` ordered by position asc |
| POST | `/api/remote-actions/macros` | macro fields | created |
| PUT | `/api/remote-actions/macros/{id}` | partial update | updated |
| DELETE | `/api/remote-actions/macros/{id}` | -- | 204 |
| POST | `/api/remote-actions/otp/{credId}` | -- | `{code:"418273",expires_at:"2026-05-03T17:14:30Z"}` |

The send path is the **existing** `POST /messages` endpoint. The
frontend assembles `@@<otp>#<action> [args]` and posts it like any
other message. No new send route. Outbound Actions are
indistinguishable from hand-typed lines on the wire and in the
audit log, which is the property we want.

### 6.3 OTP endpoint behaviour

- Looks up credential by `credId`, computes the current TOTP for the
  current step using the configured algorithm / digits / period.
- Bumps `last_used_at` on the credential row in the same transaction.
- Response includes both the code and the next step boundary so the
  client can drive the countdown without re-polling.
- Returns 404 if credential gone.

### 6.4 Server-side concerns

- TOTP generation uses `github.com/pquerna/otp` (already a transitive
  dep via `pkg/actions/otp.go`).
- Base32 secret validation at create / update: must decode cleanly,
  length must be reasonable for the chosen algorithm.
- `target_call` uppercased + base-call validated (matches
  `messages.MatchAddressee` style) before write.
- Action name regex `^[A-Za-z0-9._-]{1,32}$` shared with inbound by
  exporting the existing constant from `pkg/actions/sanitize.go`.

## 7. Frontend

### 7.1 New components

```
web/src/components/messages/remote_actions/
  RemoteActionsDrawer.svelte      (drawer shell; fire/edit mode flip)
  MacroTile.svelte                (one macro button; cooldown overlay)
  MacroEditRow.svelte             (inline-edit row in edit mode)
  FreeFormSender.svelte           (input + countdown + SEND)
  CredentialPicker.svelte         (dropdown + Manage... link)
  CredentialsModal.svelte         (table + new-credential nested modal)
  EditCredentialModal.svelte      (single credential form)
  ReplyBubbleAdornment.svelte     (zap-reply tag + status colour)
```

### 7.2 New stores and helpers

```
web/src/lib/remote_actions/
  store.svelte.js                 (Svelte 5 singleton: macros by target,
                                   credentials, in-flight OTP map,
                                   recent-fire correlation map)
  api.js                          (openapi-fetch wrappers; mirrors
                                   web/src/lib/actions/api.js)
  otp_timer.js                    (per-credential countdown ticker;
                                   reuses expires_at from server)
  reply_match.js                  (60 s correlation map, exposes
                                   isActionReply(msg) and status helpers)
  send.js                         (assembles "@@<otp>#<action> [args]"
                                   and dispatches via existing
                                   sendMessage from api/messages.js)
```

### 7.3 Touched components

- `web/src/components/messages/ThreadHeader.svelte`: adds the zap
  toggle (DM threads only).
- `web/src/components/messages/MessageThread.svelte`: mounts
  `RemoteActionsDrawer` when toggled.
- `web/src/components/messages/MessageBubble.svelte`: optional
  `actionReply` adornment slot (only renders when `reply_match`
  flags the bubble).
- `web/src/routes/Messages.svelte`: a single Messages-level entry to
  open `CredentialsModal` directly (e.g. an item in the existing
  toolbar's overflow), so credentials can be managed without opening
  a macro form.

### 7.4 Validation

Before assembling the wire string the frontend checks:

- `action_name` matches `^[A-Za-z0-9._-]{1,32}$`
- The whole assembled `@@<otp>#<action> [args]` length fits the active
  APRS limit (67 default; 200 in long mode if enabled in
  `messages-preferences-store`).

If the line would not fit, the SEND button disables with a tooltip
explaining the budget overflow. We do not split Action lines into
multi-part messages -- the receiver expects a single frame.

## 8. Wiki and handbook

In the same change set as the implementation:

- `docs/wiki/remote-actions.md` (new): topology, drawer placement,
  the "send is a normal message" invariant, data flow.
- `docs/wiki/code-map.md`: add `pkg/remoteactions/` and the new
  frontend tree.
- `docs/wiki/actions.md`: cross-link to the new outbound page so
  someone landing on either side finds the other.
- `docs/wiki/messages.md`: short paragraph noting the drawer affordance
  and that Action replies appear in the thread with badging.
- `docs/handbook/remote-actions.html`: operator-facing setup guide.
  Covers credential creation (with `NW5W OTP` naming guidance), macro
  creation, free-form sender, and how to read badged replies.
  Screenshots taken from a real session, same style as the inbound
  Actions handbook page.

## 9. Testing

### 9.1 Backend

- Table-driven CRUD tests for credentials and macros
  (`pkg/remoteactions/store_test.go`).
- TOTP endpoint tested against RFC 6238 vectors plus a fixed-clock
  helper.
- Validation: malformed base32, oversize action name, lowercase target
  call (must uppercase on write), FK SET NULL behaviour on credential
  delete.
- Migration 16 round-trip: fresh DB + upgrade-from-15.

### 9.2 Frontend

- Smoke tests on `RemoteActionsDrawer` open / close / mode flip.
- Macro fire path with mocked `sendMessage` and a fake OTP endpoint.
- Cooldown disable + countdown decrement.
- Reply correlation: outbound `@@`, inbound text within 60 s, badge
  appears with correct status colour.
- Playwright e2e: open DM thread -> open drawer -> create macro -> fire
  -> badged reply lands in thread.

## 10. Release note

`pkg/releasenotes/notes.yaml` entry, plain ASCII, operator voice. Draft
at release time with the operator. Likely `info` style. Sketch:

> Outbound Actions: send a remote command from inside Messages. Open a
> direct-message thread, click the zap icon, and either tap a saved
> macro or type the command and arguments. If you save a one-time
> password secret for the remote station, Graywolf fills the OTP for
> you and waits for the next time slot before letting you fire again.
> Replies appear in the same thread with a small zap tag.

## 11. Risks and open questions

- **Credentials modal placement.** Launched from the per-thread drawer
  AND from a Messages-level entry. The Messages-level entry's exact
  location is not pinned -- could be the existing toolbar overflow,
  could be a new icon. Decided during implementation review.
- **Drag-reorder on mobile.** Touch drag is finicky inside a
  bottom-sheet. If it proves janky, fall back to up / down arrows on
  each row in edit mode.
- **Reply correlation false positives.** A 60 s window can match
  unrelated inbound chat from the same peer. Mitigation: only match
  when the inbound starts with `ok` / `error:` / `bad_` / `denied` /
  `unknown` / `disabled` / `busy` / `rate_limited` / `timeout` (the
  status prefixes from `pkg/actions/types.go`). Falls back to no badge
  on ambiguous replies, which is the safe direction.
- **Long-mode budget.** A long-mode 200 char message comfortably fits
  `@@123456#cmd ` plus generous args, but a worst-case macro with many
  k=v pairs can still overflow. The pre-flight length check is the
  gate; we surface the overflow before the operator hits SEND.

## 12. Out-of-scope follow-ups

- Delete-pill relocation in the Messages toolbar (separate change).
- Macro export / import bundles.
- Sharing credentials or macros across multiple Graywolf instances.
- Discovering remote `arg_schema`s.
