<script>
  // MacroTile -- one row in the macro list. Single tap fires; the
  // cooldown overlay (driven by the parent's per-step boundary)
  // disables until the next TOTP step.
  //
  // Props:
  //   macro       -- RemoteActionMacro
  //   cooldownSec -- seconds remaining; 0 = enabled
  //   onFire      -- () => void
  import { Icon } from '@chrissnell/chonky-ui';

  let { macro, cooldownSec = 0, onFire = () => {} } = $props();

  const disabled = $derived(cooldownSec > 0);
  const argsPreview = $derived(macro.args_string ? ` ${macro.args_string}` : '');
</script>

<button
  type="button"
  class="tile"
  class:disabled
  onclick={() => !disabled && onFire()}
  aria-label={`Fire macro: ${macro.label}`}
  data-testid="macro-tile"
>
  <Icon name="zap" size="sm" />
  <span class="label">{macro.label}</span>
  <span class="cmd">{macro.action_name}{argsPreview}</span>
  {#if disabled}
    <span class="cooldown">next OTP in {cooldownSec}s</span>
  {/if}
</button>

<style>
  .tile {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface-raised);
    color: var(--color-text);
    font: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 0.15s, border-color 0.15s;
  }
  .tile:hover:not(.disabled) { border-color: var(--color-primary); }
  .label { font-weight: 600; }
  .cmd { font-family: var(--font-mono); font-size: 0.8125rem; color: var(--color-text-muted); }
  .cooldown {
    grid-column: 1 / -1;
    font-size: 0.75rem;
    color: var(--color-text-muted);
    margin-top: 2px;
  }
  .disabled { opacity: 0.5; cursor: not-allowed; }
</style>
