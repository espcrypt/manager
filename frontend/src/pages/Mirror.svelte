<script>
  import { onMount } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime.js'
  import { app, Go, run, ask, refreshStatus } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let ms = $state({ configured: false })
  let busy = $state('')
  let step = $state(0)
  let mainId = $derived(app.devices.find((d) => d.active)?.id ?? '')
  let backupId = $state('')
  let others = $derived(app.devices.filter((d) => d.id !== mainId))
  let backupDev = $derived(app.devices.find((d) => d.id === backupId))
  let mainDev = $derived(app.devices.find((d) => d.id === mainId))
  const short = (d) => (d ? `${d.chip} · ${d.serial ? d.serial.slice(-6) : d.port}` : '')

  async function load() {
    ms = (await Go.GetMirrorStatus().catch(() => null)) ?? { configured: false }
  }
  onMount(() => {
    load()
    const offs = [EventsOn('mirror:changed', load), EventsOn('devices:changed', load)]
    const t = setInterval(load, 3000)
    return () => { offs.forEach((o) => o()); clearInterval(t) }
  })
  $effect(() => {
    if (backupId && !others.some((d) => d.id === backupId)) backupId = ''
    if (!backupId) backupId = (others.find((d) => d.second) ?? (others.length === 1 ? others[0] : null))?.id ?? ''
  })

  async function withBusy(name, fn, ok) {
    busy = name
    const r = await run(fn, ok)
    busy = ''
    await load()
    await refreshStatus()
    return r
  }

  async function start() {
    if (!(await ask({ title: `Make ${short(backupDev)} a mirror?`, danger: true, confirmText: 'Overwrite backup and start', typeToConfirm: 'MIRROR',
      message: `Everything on ${short(backupDev)} is replaced with the data of ${short(mainDev)}: keyfiles, SSH keys and passkeys. The backup then opens with the main ESP32's passphrase. From then on, changes on either ESP32 are copied to the other one automatically whenever both are connected.` }))) return
    const ok = await withBusy('start', () => Go.StartMirror(mainId, backupId), 'Mirror set up: the backup now holds the same data')
    if (ok) step = 0
  }

  async function stop() {
    if (!(await ask({ title: 'Stop mirroring?', confirmText: 'Stop mirroring', message: 'Both ESP32s keep their data, but changes are no longer copied between them.' }))) return
    await withBusy('stop', () => Go.StopMirror(), 'Mirror mode stopped')
  }

  async function resolve(keep) {
    const keepDev = keep === 'main' ? ms.main : ms.backup
    const loseDev = keep === 'main' ? ms.backup : ms.main
    if (!(await ask({ title: `Keep ${keepDev.label}?`, danger: true, confirmText: `Keep ${keep}`,
      message: `The changes made on ${loseDev.label} since the last sync are overwritten with the data of ${keepDev.label}.` }))) return
    await withBusy('resolve', () => Go.ResolveMirrorConflict(keep), 'Conflict resolved')
  }

  async function backupFile() {
    const r = await withBusy('file', () => Go.BackupToFile())
    if (r && r !== true) run(() => Promise.resolve(), `Backup saved: ${r.split(/[\\/]/).pop()}`)
  }

  async function restoreFile() {
    const active = app.devices.find((d) => d.active)
    if (!(await ask({ title: `Restore a backup to ${short(active)}?`, danger: true, confirmText: 'Choose file and restore', typeToConfirm: 'RESTORE',
      message: `Everything on ${short(active)} is replaced with the backup. Afterwards it opens with the passphrase that was used when the backup was made.` }))) return
    const r = await withBusy('restore', () => Go.RestoreFromFile())
    if (r && r !== true) run(() => Promise.resolve(), r)
  }

  const badge = (s) => s === 'sync' ? (ms.message === 'In sync' ? 'badge-ok' : 'badge-warn') : s === 'conflict' || s === 'error' ? 'badge-err' : 'badge-warn'
</script>

{#if ms.configured}
  <section class="card mb-5">
    <div class="mb-4 flex flex-wrap items-center gap-3">
      <div class="card-title mb-0"><Icon name="database" /> Mirror</div>
      <span class="badge {badge(ms.state)}">{ms.message}</span>
      <span class="ml-auto text-xs text-neutral-500">{ms.lastSync ? `Last sync ${ms.lastSync}` : ''}</span>
    </div>
    <div class="grid items-center gap-3 md:grid-cols-[1fr_auto_1fr]">
      {#each [['Main', ms.main], ['Backup', ms.backup]] as [role, b], i}
        {#if i === 1}<div class="text-center text-2xl text-violet-300">⇄</div>{/if}
        <div class="rounded-xl border border-neutral-700/50 p-4">
          <div class="text-[11px] font-semibold tracking-wider text-neutral-500 uppercase">{role}</div>
          <div class="mt-1 font-medium">{b.label}</div>
          <div class="mt-2 flex flex-wrap gap-2 text-xs">
            {#if b.connected}<span class="badge badge-ok">{b.transport === 'wifi' ? 'over Wi-Fi' : 'over USB'}</span>{:else}<span class="badge badge-muted">not connected</span>{/if}
            {#if b.connected}{#if b.unlocked}<span class="badge badge-ok">unlocked</span>{:else}<span class="badge badge-warn">locked</span>{/if}{/if}
            {#if b.passkey}<span class="badge badge-muted">+ passkeys</span>{/if}
          </div>
          {#if b.connected}<div class="mt-2 text-xs text-neutral-500">Revision {b.rev}{b.passkey ? ` · passkeys ${b.passkeyRev}` : ''}</div>{/if}
          {#if b.connected && !b.unlocked && ms.state === 'unlock'}
            <button class="btn btn-sm mt-3" onclick={() => (app.pinPrompt = { reason: 'Unlock to sync the passkeys between both ESP32s.', deviceId: b.id, label: b.label })}><Icon name="unlock" /> Unlock</button>
          {/if}
        </div>
      {/each}
    </div>
    {#if ms.state === 'error'}
      <div class="callout callout-warn mt-4 text-sm">{ms.error}. The app keeps retrying every few seconds; nothing is lost.</div>
    {/if}
    {#if ms.state === 'conflict'}
      <div class="callout callout-warn mt-4 flex flex-wrap items-center gap-3">
        <div class="flex-1 text-sm">Both ESP32s were changed separately. Pick the one with the data you want to keep; the other is overwritten.</div>
        <button class="btn btn-sm" disabled={!!busy} onclick={() => resolve('main')}>Keep main</button>
        <button class="btn btn-sm" disabled={!!busy} onclick={() => resolve('backup')}>Keep backup</button>
      </div>
    {/if}
    <div class="mt-4 flex flex-wrap gap-2">
      <button class="btn btn-sm" disabled={!!busy} onclick={() => withBusy('sync', () => Go.SyncMirrorNow())}><Icon name="bolt" /> Sync now</button>
      <button class="btn btn-sm" disabled={!!busy} onclick={() => withBusy('swap', () => Go.SwapMirror(), 'Main and backup swapped')}>Swap main and backup</button>
      <button class="btn btn-sm btn-ghost text-rose-300" disabled={!!busy} onclick={stop}>Stop mirroring</button>
    </div>
    <p class="hint mt-3">Changes are copied automatically, in both directions, whenever both ESP32s are connected. The data stays encrypted while it is copied: the app never sees keyfiles, SSH keys or passkeys.{ms.main.passkey && ms.backup.passkey ? ' Passkeys are copied while both ESP32s are unlocked.' : ''}</p>
  </section>
{:else}
  <section class="card mb-5">
    <div class="card-title"><Icon name="database" /> Mirror: two ESP32s with the same data</div>
    {#if step === 0}
      <p class="mb-4 max-w-2xl text-sm text-neutral-400">A second ESP32 becomes an exact copy of your main one: keyfiles, SSH keys and passkeys. Lose or break one, and the other just keeps working. Every change on either ESP32 is copied to the other automatically while both are connected.</p>
      <button class="btn btn-primary" disabled={!mainId} onclick={() => (step = 2)}><Icon name="plus" /> Set up a mirror</button>
      {#if !mainId}<p class="hint mt-2">Choose the main ESP Crypt in the sidebar first.</p>{:else}<p class="hint mt-2">Main: {short(mainDev)} (the active ESP32 in the sidebar).</p>{/if}
    {:else}
      <ol class="mb-5 flex gap-2 text-xs">
        {#each ['Connect the backup', 'Copy'] as s, i}
          <li class="rounded-full px-3 py-1 {step >= i + 2 ? 'bg-violet-500/20 text-violet-200' : 'bg-neutral-800 text-neutral-500'}">{i + 1}. {s}</li>
        {/each}
      </ol>
      {#if step === 2}
        {#if !others.length}
          <div class="mb-4 flex items-center gap-4 rounded-xl border border-dashed border-neutral-600 p-6">
            <div class="h-3 w-3 animate-pulse rounded-full bg-violet-400"></div>
            <div class="text-sm"><div class="font-medium">Connect your second ESP32</div><div class="text-neutral-400">Plug it in by USB. It needs the Vault firmware; a new board can be flashed on the ESP32 firmware page.</div></div>
            <button class="btn btn-sm ml-auto" onclick={() => (app.page = 'esp32')}>Flash firmware</button>
          </div>
        {:else}
          {#if backupDev}
            <p class="mb-4 text-sm text-neutral-400">{short(backupDev)} becomes the backup of {short(mainDev)}. Its current data is overwritten.</p>
          {:else}
            <p class="mb-3 text-sm text-neutral-400">Several other ESP32s are connected. Which one becomes the backup of {short(mainDev)}? Its current data is overwritten.</p>
            <select class="input mb-4 max-w-sm" bind:value={backupId} aria-label="Backup ESP32">
              <option value="">Choose the backup ESP32</option>
              {#each others as d (d.id)}<option value={d.id}>{short(d)}</option>{/each}
            </select>
          {/if}

        {/if}
        <div class="flex gap-2">
          <button class="btn btn-ghost" onclick={() => (step = 0)}>Cancel</button>
          <button class="btn btn-primary" disabled={!backupId || !!busy} onclick={start}>{busy === 'start' ? 'Copying…' : 'Copy to backup and start mirroring'}</button>
        </div>
      {/if}
    {/if}
  </section>
{/if}

<section class="card">
  <div class="card-title"><Icon name="file" /> Backup file</div>
  <p class="mb-4 max-w-2xl text-sm text-neutral-400">Save the active ESP32's data to a file, for example on a USB stick or in your cloud storage. The file only holds encrypted data: it opens with your Vault passphrase{app.status.passkey ? ', and passkeys are only included while the ESP32 is unlocked' : ''}.</p>
  <div class="flex flex-wrap gap-2">
    <button class="btn" disabled={!app.status.connected || !!busy} onclick={backupFile}><Icon name="download" /> {busy === 'file' ? 'Saving…' : 'Back up to file…'}</button>
    <button class="btn" disabled={!app.status.connected || !!busy} onclick={restoreFile}><Icon name="upload" /> {busy === 'restore' ? 'Restoring…' : 'Restore from file…'}</button>
  </div>
</section>
