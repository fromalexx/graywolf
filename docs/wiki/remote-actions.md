# Remote Actions (outbound) subsystem

Operator-curated affordance that turns the same `@@<otp>#<action>` wire
grammar into outbound traffic: the operator picks a remote station in
Messages, fires a saved macro (or types a free-form action), and the
Messages send path delivers `@@<otp>#<action> [k=v ...]` to that
station. The remote side runs the inbound [Actions
subsystem](actions.md); this side just composes and sends.

Lives in [`../../pkg/remoteactions/`](../../pkg/remoteactions/) with
persistence in [`../../pkg/configstore/`](../../pkg/configstore/) and
wiring in [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go). REST
handlers under
[`../../pkg/webapi/remote_actions_*.go`](../../pkg/webapi/). UI lives
under `web/src/components/messages/remote_actions/`.

## Sibling, not fork

`pkg/remoteactions/` is a **sibling** of `pkg/actions/`, not a fork.
The two share **only the wire grammar** — `pkg/actions/parser.go`
exports `ValidActionName` and `MaxActionNameLen`, which
`pkg/remoteactions/validate.go` consumes so outbound macro creation
rejects names the receiver would refuse. There is no runtime coupling:
- Inbound classifier never reads remote credentials or macros.
- Outbound macro fires never enter the inbound classifier.

## Wire grammar (recap)

```
@@<otp>#<action> [k=v ...]
```

Same grammar as inbound. `<otp>` is six digits or empty when the
remote Action has `otp_required=false`. The outbound side does not
know the remote Action's `otp_required` value, so the operator picks
"manual OTP" (omit) vs. "use credential" (six digits stamped from a
TOTP secret) per macro.

## Hot path

```
operator clicks macro tile
  -> pkg/remoteactions store loads macro + (optional) credential
  -> POST /api/remote-actions/otp/{credId} -> RFC 6238 TOTP code
  -> frontend assembles "@@<digits>#<name> [args]"
  -> POST /api/messages (existing send path; identical to typed lines)
  -> pkg/messages.Service.SendMessage -> RF/IS as configured
```

The macro fire is **indistinguishable on the wire from a hand-typed
line**. There is no `Source: macro` flag in the messages table; the
auditor sees one outbound message of kind `text`.

## Schema (migration 16)

`pkg/configstore/migrate_remote_actions.go`. Two tables, raw SQL so
the FK and the index can be expressed precisely:

| Table | Columns | Notes |
|---|---|---|
| `remote_otp_credentials` | `id`, `name` (UNIQUE), `secret_b32`, `algorithm`, `digits`, `period`, `created_at`, `last_used_at` | TOTP secret stored plaintext per the single-user-station design. `secret_b32` is never echoed back after Create. |
| `remote_action_macros` | `id`, `target_call`, `label`, `action_name`, `args_string`, `remote_otp_credential_id` (nullable FK), `position`, `created_at`, `updated_at` | `target_call` indexed for per-thread lookup. FK has `ON DELETE SET NULL` — deleting a credential demotes its macros to "manual OTP" instead of orphaning them. |

The two matching Go models in
[`../../pkg/remoteactions/models.go`](../../pkg/remoteactions/models.go)
are intentionally **NOT** registered with AutoMigrate; migration 16 is
the single source of truth.

Migration version 16 follows version 15 (inbound `actions_tables`).
See [`code-map.md`](code-map.md#configstore) for the full migration
ledger.

## REST surface

All routes under `/api/remote-actions/`. Op IDs in
[`../../pkg/webapi/docs/op_ids.go`](../../pkg/webapi/docs/op_ids.go);
swagger tag `remote-actions`.

| Method | Path | Op ID | Notes |
|---|---|---|---|
| GET | `/api/remote-actions/credentials` | `listRemoteOTPCredentials` | Wire shape strips `secret_b32`. `used_by[]` carries distinct target callsigns of bound macros (single scan via `CredStore.UsedBy`). |
| POST | `/api/remote-actions/credentials` | `createRemoteOTPCredential` | Validates base32 (case-insensitive, whitespace tolerated); 409 on UNIQUE name collision. |
| PUT | `/api/remote-actions/credentials/{id}` | `updateRemoteOTPCredential` | Empty `secret_b32` leaves stored secret untouched. 404 when row gone. 409 on name collision. |
| DELETE | `/api/remote-actions/credentials/{id}` | `deleteRemoteOTPCredential` | 409 when `len(used_by) > 0`. |
| GET | `/api/remote-actions/macros?target=CALL` | `listRemoteActionMacros` | Target uppercased server-side. Sorted by `position ASC, id ASC`. |
| POST | `/api/remote-actions/macros` | `createRemoteActionMacro` | Validates target callsign + action name + args length. |
| PUT | `/api/remote-actions/macros/{id}` | `updateRemoteActionMacro` | Partial update with mixed semantics — see DTO doc on `RemoteActionMacroRequest`. **Position is ignored on PUT**; `/macros/reorder` is the sole owner of ordering. |
| DELETE | `/api/remote-actions/macros/{id}` | `deleteRemoteActionMacro` | 204 on success. |
| POST | `/api/remote-actions/macros/reorder` | `reorderRemoteActionMacros` | Body `{target_call, ids[]}`. Each id must resolve to a macro for that target; an unknown id rolls the transaction back and returns 400. |
| POST | `/api/remote-actions/otp/{credId}` | `generateRemoteOTPCode` | Returns `{code, expires_at}` (RFC3339 UTC, inclusive upper edge of current TOTP step). Bumps `last_used_at` (non-fatal on error). |

## Wiring

`pkg/app/wiring.go`:

1. `wireRemoteActions(ctx)` — runs after `wireActions`. Failure is
   non-fatal: logs and returns nil so the rest of the app boots. The
   webapi handlers fall back to 503 via `requireRemoteActions` when
   `App.remoteActions` is nil.
2. `apiSrv.SetRemoteActions(a.remoteActions)` — runs before
   `apiSrv.RegisterRoutes(apiMux)` so handlers see the service on
   the first request.

The composition root (`pkg/remoteactions/Service`) holds two stores
(`CredStore`, `MacroStore`) and a logger. There is no goroutine, no
scheduler, no inbound hook — outbound fires are synchronous and
piggy-back on `pkg/messages`.

## Cross-references

- Inbound counterpart: [`actions.md`](actions.md)
- Messages send path: see [`code-map.md`](code-map.md) row `messages`
- Single-user-station design rationale (plaintext secrets, no rate
  limit on OTP generate): consistent with inbound `otp_credentials`
  table.
- Original design intent: `docs/superpowers/specs/2026-05-03-messages-action-sender-design.md`,
  Phase A plan `docs/superpowers/plans/2026-05-03-messages-action-sender.md`.
