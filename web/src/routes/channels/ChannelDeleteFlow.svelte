<script>
  import { untrack } from 'svelte';
  import { AlertDialog } from '@chrissnell/chonky-ui';
  import { api, ApiError } from '../../lib/api.js';
  import { toasts } from '../../lib/stores.js';
  import { groupReferrers, totalReferrers } from '../../lib/channelReferrers.js';

  // target is $bindable: parent sets it to a channel row to open the flow;
  // this component resets it to null on success or cancel.
  let { target = $bindable(), onDeleted } = $props();

  // Single-confirm delete flow.
  // A referenced channel opens the "impact" dialog, which lists the
  // referrers grouped by type and carries the final cascade-delete
  // button (the list itself is the confirmation surface). An
  // unreferenced channel opens a plain confirm dialog. Neither path
  // requires retyping the channel name.
  let deleteImpactOpen = $state(false);
  let deleteConfirmOpen = $state(false);
  let deleteReferrers = $state([]);
  let deleteInFlight = $state(false);
  let deleteGroups = $derived(groupReferrers(deleteReferrers));
  let deleteTotal = $derived(totalReferrers(deleteReferrers));

  // Edge-triggered open: fire openFlow ONCE per null→non-null transition of
  // target, mirroring the original one-shot imperative requestDelete call.
  // Only `target` is tracked; the row is read via untrack() so reassigning
  // target to another non-null ref mid-flow does NOT re-run openFlow.
  // Setting target=null (cancel/success) resets the edge guard so a later
  // reopen fires once again.
  let prevTargetNull = true;
  $effect(() => {
    const row = target; // subscribe to `target` only
    if (row != null && prevTargetNull) {
      prevTargetNull = false;
      untrack(() => { openFlow(row); });
    } else if (row == null) {
      prevTargetNull = true;
    }
  });

  // Called when target becomes non-null:
  //   1. Fetch /api/channels/{id}/referrers.
  //   2. Empty list: open the plain confirm dialog (cascade=false).
  //   3. Non-empty list: open the impact dialog, whose Delete button
  //      cascades (cascade=true). On confirm we call DELETE with or
  //      without ?cascade=true depending on the path.
  async function openFlow(row) {
    deleteReferrers = [];
    try {
      const resp = await api.get(`/channels/${row.id}/referrers`);
      const refs = Array.isArray(resp?.referrers) ? resp.referrers : [];
      deleteReferrers = refs;
      if (refs.length === 0) {
        deleteImpactOpen = false;
        deleteConfirmOpen = true;
      } else {
        deleteImpactOpen = true;
        deleteConfirmOpen = false;
      }
    } catch (err) {
      toasts.error(err.message);
      target = null;
    }
  }

  function cancelDelete() {
    deleteImpactOpen = false;
    deleteConfirmOpen = false;
    target = null;
    deleteReferrers = [];
  }

  async function executeDelete() {
    if (!target) return;
    const cascade = deleteReferrers.length > 0;
    const id = target.id;
    deleteInFlight = true;
    try {
      const path = cascade ? `/channels/${id}?cascade=true` : `/channels/${id}`;
      await api.delete(path);
      toasts.success(cascade
        ? `Channel deleted along with ${deleteTotal} reference${deleteTotal === 1 ? '' : 's'}`
        : 'Channel deleted');
      await onDeleted?.();
      deleteImpactOpen = false;
      deleteConfirmOpen = false;
      target = null;
      deleteReferrers = [];
    } catch (err) {
      // A 409 here would mean a race (referrers appeared between our
      // GET and DELETE). Surface the same error channel; the impact
      // dialog route will naturally pick them up on the next click.
      if (err instanceof ApiError && err.status === 409 && Array.isArray(err.body?.referrers)) {
        deleteReferrers = err.body.referrers;
        deleteConfirmOpen = false;
        deleteImpactOpen = true;
        toasts.error('New references appeared — review and try again');
      } else {
        toasts.error(err.message);
      }
    } finally {
      deleteInFlight = false;
    }
  }
</script>

<!-- Impact dialog (only when the channel has referrers). Lists what the
     cascade will do to each dependent row, grouped by type, so the
     operator has an informed sense of scope. The Delete button here is
     final and cascades. -->
<AlertDialog bind:open={deleteImpactOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete channel {target?.name ?? ''}?</AlertDialog.Title>
    <AlertDialog.Description>
      This channel has {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}. Deleting it will affect:
    </AlertDialog.Description>
    <ul class="referrer-groups">
      {#each deleteGroups as g (g.type)}
        <li>
          <strong>{g.items.length} {g.label}</strong>{#if g.action}<span class="referrer-action"> — {g.action}</span>{/if}{#if g.items.some((i) => i.name)}:
            <span class="referrer-items">
              {#each g.items as item, idx (item.id)}{idx > 0 ? ', ' : ''}{item.name || `#${item.id}`}{/each}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={executeDelete}
        disabled={deleteInFlight}
      >
        Delete channel and {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<!-- Plain confirm dialog for an unreferenced channel. -->
<AlertDialog bind:open={deleteConfirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete channel {target?.name ?? ''}?</AlertDialog.Title>
    <AlertDialog.Description>
      This cannot be undone.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={executeDelete}
        disabled={deleteInFlight}
      >
        Delete channel
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  /* Referrer list — scoped copy; Svelte styles are per-component.
     ChannelPutConfirm.svelte carries its own copy for the same reason. */
  .referrer-groups {
    margin: 12px 1.5rem 0 1.5rem;
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    list-style: disc inside;
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.6;
  }
  .referrer-groups li + li {
    margin-top: 2px;
  }
  .referrer-action {
    color: var(--text-secondary);
    font-style: italic;
  }
  .referrer-items {
    color: var(--text-secondary);
  }

  /* modal-footer: used by both delete stages here. */
  .modal-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1.25rem 1.5rem 1.5rem;
  }

  /* :global(.danger-action) is declared in ChannelPutConfirm.svelte; being
     :global it is document-wide and reaches this component's
     AlertDialog.Action slot too. */
</style>
