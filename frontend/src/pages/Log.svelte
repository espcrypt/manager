<script>
  import { app, Go, run } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let filter = $state('all')
  let rows = $derived(app.logs.filter((l) => filter === 'all' || l.level === filter).slice().reverse())
  const color = { info: 'text-neutral-300', warn: 'text-amber-300', error: 'text-rose-300', debug: 'text-neutral-500' }
</script>

<div class="card">
  <div class="mb-4 flex flex-wrap items-center gap-2">
    {#each [['all', 'All'], ['info', 'Info'], ['warn', 'Warnings'], ['error', 'Errors'], ['debug', 'Debug']] as [id, l]}
      <button class="tab {filter === id ? 'tab-active' : ''}" onclick={() => (filter = id)}>{l}</button>
    {/each}
    <div class="ml-auto flex gap-2">
      <button class="btn btn-sm" onclick={() => run(() => Go.CopyText(app.logs.map((l) => `${l.time} [${l.level}] ${l.msg}`).join('\n')), 'Copied')}><Icon name="copy" /> Copy</button>
      <button class="btn btn-sm" onclick={async () => { await Go.ClearLogs(); app.logs = [] }}><Icon name="trash" /> Clear</button>
    </div>
  </div>
  <div class="selectable max-h-[70vh] space-y-0.5 overflow-auto font-mono text-xs">
    {#each rows as l}
      <div class="flex gap-3 rounded px-2 py-1 hover:bg-neutral-700/30">
        <span class="text-neutral-500">{l.time}</span>
        <span class="w-12 uppercase {color[l.level]}">{l.level}</span>
        <span class="flex-1 break-all {color[l.level]}">{l.msg}</span>
      </div>
    {:else}
      <p class="text-neutral-500">Nothing logged yet.</p>
    {/each}
  </div>
</div>
