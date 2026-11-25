<script>
  import { onMount } from 'svelte'
  import { app, Go, run, ask, promptText } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'
  import StorageBar from '../lib/StorageBar.svelte'

  let objs = $state([])
  let vaults = $state([])
  let label = $state('')
  let size = $state(64)
  let showNew = $state(false)

  async function load() {
    if (!app.status.connected) { objs = []; return }
    objs = (await Go.ListDataObjects().catch(() => [])) ?? []
    vaults = (await Go.ListVaults().catch(() => [])) ?? []
  }
  onMount(load)
  $effect(() => { app.status.connected; load() })
  let locked = $derived(!app.status.pin?.verified)
  let initialized = $derived(app.status.vaultStorageReady && app.status.pin && !app.status.pin.notInitialized)
  const usedBy = (o) => vaults.filter((v) => v.keyfileFid === o.fid).map((v) => v.name)

  async function create(e) {
    e.preventDefault()
    const id = await run(() => Go.CreateKeyfile(label, +size), 'Keyfile created')
    if (id) { label = ''; showNew = false; load() }
  }
  async function importFile() {
    const id = await run(() => Go.ImportKeyfile(label), 'Keyfile imported')
    if (id) { label = ''; showNew = false; load() }
  }
  async function exportObj(o) {
    if (!(await ask({ title: 'Export keyfile', confirmText: 'Export…',
      message: 'An exported copy is outside the passphrase-encrypted device container. Keep it as an offline backup, preferably on encrypted removable storage.' }))) return
    run(() => Go.ExportDataObject(o.id, o.label), 'Exported')
  }
  const vaultFromLabel = (l) => (l.match(/^Vault:\s*(.+?)(\s*\(key \d of 2\))?$/i) ?? [])[1]
  async function del(o) {
    const vs = usedBy(o)
    const named = vaultFromLabel(o.label)
    let message = 'Anything protected with this keyfile can never be opened again without a backup.'
    if (vs.length) message = `This keyfile opens: ${vs.join(', ')}.\nWithout it (or a backup) these vaults can NEVER be opened again.`
    else if (named) message = `This keyfile was created for the vault "${named}". That vault may be set up on another computer, so it is not listed here, but this keyfile can be its ONLY key.\nDelete it only if you no longer need "${named}" or have a backup of this keyfile.`
    if (!(await ask({ title: 'Delete keyfile', danger: true, confirmText: 'Delete forever', typeToConfirm: o.label, message }))) return
    await run(() => Go.DeleteDataObject(o.id), 'Deleted')
    load()
  }
  async function rename(o) {
    const n = await promptText('Rename keyfile', o.label)
    if (n && n !== o.label) { await run(() => Go.RenameDataObject(o.id, n)); load() }
  }
</script>

{#if !app.status.connected}
  <div class="card text-sm text-neutral-400">Connect your custom ESP Crypt to see its keyfiles.</div>
{:else if !initialized}
  <section class="card max-w-2xl border-amber-500/30">
    <div class="card-title text-amber-100"><Icon name="shield" /> Vault storage is not initialized</div>
    <p class="mb-4 text-sm text-neutral-300">Prepare NVS and set a Vault passphrase to create the encrypted keyfile container.</p>
    <button class="btn btn-primary" onclick={() => (app.page = 'security')}>Open passphrase setup</button>
  </section>
{:else}
  <div class="callout callout-info mb-5 flex items-center gap-4">
    <Icon name="lock" class="h-5 w-5 shrink-0" />
      <div class="flex-1 text-sm">You normally don't need this page: every vault gets its own keyfile automatically. Use it to import an existing VeraCrypt keyfile or make an offline backup.</div>
    <button class="btn btn-sm" onclick={() => (app.page = 'vaults')}>Go to vaults</button>
  </div>
  {#if app.status.storage?.totalEntries}
    <section class="card mb-5">
      <StorageBar storage={app.status.storage} items={[
      { label: 'Encrypted container (keyfiles, SSH keys)', used: app.status.storage?.containerBytes ?? 0, max: app.status.storage?.maxContainerBytes ?? 0, bytes: true },
      ...(app.status.storage?.objects !== undefined && app.status.pin?.verified ? [{ label: 'Keyfiles and SSH keys', used: app.status.storage.objects, max: app.status.storage.maxObjects }] : []),
    ]} />
    </section>
  {/if}
  <section class="card">
    <div class="mb-4 flex flex-wrap items-center justify-between">
      <div class="card-title mb-0"><Icon name="file" /> Keyfiles on your ESP Crypt</div>
      <button class="btn btn-sm" disabled={locked} onclick={() => (showNew = !showNew)}><Icon name="plus" /> Add keyfile</button>
    </div>
    {#if showNew}
      <form class="mb-4 grid gap-3 rounded-xl border border-neutral-700/50 bg-neutral-900/40 p-4 md:grid-cols-[1fr_180px_auto_auto]" onsubmit={create}>
        <input class="input" bind:value={label} placeholder="Name" />
        <select class="input" bind:value={size}><option value={64}>64 bytes (standard)</option><option value={256}>256 bytes</option><option value={1024}>1 KB</option></select>
        <button class="btn btn-primary" disabled={!label}>Generate</button>
        <button type="button" class="btn" onclick={importFile}><Icon name="upload" /> Import file…</button>
      </form>
    {/if}
    <div class="divide-y divide-neutral-700/50">
      {#each objs as o (o.fid)}
        <div class="flex items-center gap-4 py-3">
          <div class="grid h-9 w-9 place-items-center rounded-lg bg-neutral-700/60 {o.private ? 'text-violet-300' : 'text-neutral-400'}"><Icon name={o.private ? 'lock' : 'file'} /></div>
          <div class="min-w-0 flex-1">
            <div class="text-sm font-medium">{o.label}</div>
            <div class="text-xs text-neutral-500">Passphrase-encrypted · {o.size} bytes{usedBy(o).length ? ` · opens ${usedBy(o).join(', ')}` : ''}</div>
          </div>
          <button class="btn btn-sm btn-ghost" title="Rename" onclick={() => rename(o)}><Icon name="edit" /></button>
          <button class="btn btn-sm btn-ghost" title="Export" disabled={locked && o.private} onclick={() => exportObj(o)}><Icon name="download" /></button>
          <button class="btn btn-sm btn-ghost text-rose-300" title="Delete" onclick={() => del(o)}><Icon name="trash" /></button>
        </div>
      {:else}
        <p class="py-6 text-center text-sm text-neutral-500">No keyfiles yet. Creating a vault adds one automatically.</p>
      {/each}
    </div>
    {#if locked}<p class="hint mt-3">Unlock the ESP Crypt to add or export keyfiles.</p>{/if}
  </section>
{/if}
