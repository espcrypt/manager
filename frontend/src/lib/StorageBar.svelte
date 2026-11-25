<script>
  let { storage, items = [], title = 'Flash storage' } = $props()

  const kb = (n) => (n < 1024 ? `${n} B` : `${(n / 1024).toFixed(n < 100 * 1024 ? 1 : 0)} KB`)
  let entry = $derived(storage?.entryBytes || 32)
  let used = $derived((storage?.usedEntries ?? 0) * entry)
  let free = $derived((storage?.freeEntries ?? 0) * entry)
  let pct = $derived(used + free ? used / (used + free) : 0)
  const tone = (p) => (p >= 0.95 ? 'bg-rose-400' : p >= 0.8 ? 'bg-amber-400' : 'bg-violet-400')
  const fmt = (it) => (it.bytes ? `${kb(it.used)} of ${kb(it.max)}` : `${it.used} of ${it.max}`)
</script>

{#if storage?.totalEntries}
  <div class="space-y-3">
    <div>
      <div class="mb-1 flex justify-between text-xs"><span class="text-neutral-400">{title}</span><span class="text-neutral-300">{kb(used)} used · {kb(free)} free</span></div>
      <div class="h-1.5 overflow-hidden rounded-full bg-neutral-700/60"><div class="h-full rounded-full {tone(pct)}" style="width: {Math.max(2, pct * 100)}%"></div></div>
    </div>
    {#each items.filter((it) => it.max) as it}
      {@const p = Math.min(1, it.used / it.max)}
      <div>
        <div class="mb-1 flex justify-between text-xs"><span class="text-neutral-400">{it.label}</span><span class="text-neutral-300">{fmt(it)}</span></div>
        <div class="h-1.5 overflow-hidden rounded-full bg-neutral-700/60"><div class="h-full rounded-full {tone(p)}" style="width: {Math.max(2, p * 100)}%"></div></div>
      </div>
    {/each}
    {#if pct >= 0.9}<p class="text-xs text-amber-300">Storage is almost full. Delete keyfiles, SSH keys or passkeys you no longer need before adding new ones.</p>{/if}
  </div>
{/if}
