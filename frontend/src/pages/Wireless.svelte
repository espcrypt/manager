<script>
  import { onMount } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime.js'
  import { app, Go, run, ask, refreshStatus, activeDevice } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let dev = $derived(activeDevice())
  let info = $state(null)
  let loading = $state(false)
  let busy = $state('')
  let confirming = $state(false)
  let form = $state({ mirror: false, ssid: '', password: '' })
  let dirtyPassword = $state(false)
  let loadedFor = ''

  async function load(force = false) {
    if (!dev) { info = null; return }
    if (!force && loadedFor === dev.id && info) return
    loading = true
    const id = dev.id
    const r = await Go.GetWireless(id).catch((e) => { run(() => Promise.reject(e)); return null })
    if (dev?.id !== id) return
    info = r
    loadedFor = id
    loading = false
    form = { mirror: !!r?.radio?.features?.wifiMirror, ssid: r?.radio?.ssid ?? '', password: '' }
    dirtyPassword = false
  }
  $effect(() => { if (dev?.id !== loadedFor) load(true) })
  onMount(() => EventsOn('wireless:confirm', (v) => { confirming = v }))

  let radio = $derived(info?.radio)
  let changed = $derived(!!radio && (form.mirror !== !!radio.features?.wifiMirror || form.ssid.trim() !== (radio.ssid ?? '') || dirtyPassword))
  let needsNetwork = $derived(form.mirror && !form.ssid.trim())

  function reloadSoon() {
    info = null
    loadedFor = ''
    setTimeout(() => { refreshStatus(); load(true) }, 6000)
  }

  async function save() {
    const cfg = { features: { wifiMirror: form.mirror }, ssid: form.ssid, password: dirtyPassword ? form.password : null }
    busy = 'save'
    const ok = await run(() => Go.SetWireless(dev.id, cfg), 'Saved; the ESP32 restarts')
    busy = ''
    if (ok) reloadSoon()
  }

  async function newName() {
    if (!(await ask({ title: 'Give this ESP32 a new random name?', confirmText: 'New name', message: 'The name is how this ESP32 shows up on your network.' }))) return
    busy = 'name'
    const ok = await run(() => Go.SetWireless(dev.id, { features: {}, ssid: null, password: null, newName: true }), 'New name set; the ESP32 restarts')
    busy = ''
    if (ok) reloadSoon()
  }

  async function pair() {
    busy = 'pair'
    const ok = await run(() => Go.PairWireless(dev.id), 'This computer is paired')
    busy = ''
    if (ok) load(true)
  }

  async function unpair(c) {
    const self = c.kid === info.pairedAs
    if (!(await ask({ title: self ? 'Unpair this computer?' : `Remove ${c.label}?`, danger: true, confirmText: 'Remove',
      message: self ? 'This computer then reaches the ESP32 only over USB, until you pair it again.' : `${c.label} can no longer reach this ESP32 over Wi-Fi. The ESP32 restarts.` }))) return
    busy = 'unpair'
    const ok = await run(() => Go.UnpairWireless(dev.id, c.kid), 'Pairing removed')
    busy = ''
    if (ok) reloadSoon()
  }
</script>

{#if !dev}
  <div class="card text-sm text-neutral-400">Choose an ESP Crypt in the sidebar.</div>
{:else if loading && !info}
  <div class="card text-sm text-neutral-400">Reading the settings of {dev.label}…</div>
{:else if info && !info.supported}
  <div class="callout callout-warn">
    <div class="font-medium">This firmware cannot mirror over Wi-Fi yet</div>
    <div class="mt-1 text-amber-100/80">Update {dev.label} on the ESP32 firmware page; keyfiles, SSH keys and passkeys are kept.</div>
    <button class="btn btn-sm mt-3" onclick={() => (app.page = 'esp32')}><Icon name="chip" /> ESP32 firmware</button>
  </div>
{:else if info && radio}
  <section class="card mb-5">
    <div class="flex flex-wrap items-center gap-3">
      <div class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-violet-500/15 text-violet-300"><Icon name="wifi" class="h-5 w-5" /></div>
      <div class="min-w-48 flex-1">
        <div class="font-medium">{radio.name}</div>
        <div class="text-xs text-neutral-400">{dev.label}{radio.wifiConnected ? ` · on ${radio.ssid} (${radio.ip})` : radio.features?.wifiMirror ? ' · not on the network yet' : ' · Wi-Fi off'}</div>
      </div>
      {#if info.paired}<span class="badge badge-ok">this computer is paired</span>{:else}<span class="badge badge-muted">not paired</span>{/if}
      <button class="btn btn-sm btn-ghost" disabled={!!busy} onclick={newName} title="Give this ESP32 a new random name"><Icon name="refresh" /> New name</button>
    </div>
    {#if radio.safeMode}
      <p class="callout callout-warn mt-4">Wi-Fi stopped the ESP32 twice in a row, so it now starts without it. Save again to retry.</p>
    {/if}
  </section>

  <section class="card mb-5">
    <div class="card-title"><Icon name="database" /> Mirror over Wi-Fi</div>
    <label class="check items-start"><input type="checkbox" class="mt-0.5 shrink-0" disabled={!!busy} bind:checked={form.mirror} />
      <span><span class="font-medium">Keep a mirror in sync over Wi-Fi</span><span class="hint block">A mirrored ESP32 that is not plugged in, for example the backup elsewhere in the house, stays in sync while it is on the same network. Only mirror sync works over Wi-Fi: vaults, keyfiles, SSH and passkeys always need USB.</span></span></label>
    <div class="mt-4 grid gap-3 sm:grid-cols-2">
      <div><label class="label" for="ssid">Network name</label>
        <input id="ssid" class="input" disabled={!!busy} bind:value={form.ssid} placeholder="Wi-Fi name (2.4 GHz)" autocomplete="off" spellcheck="false" /></div>
      <div><label class="label" for="wpass">Password</label>
        <input id="wpass" class="input" type="password" disabled={!!busy} bind:value={form.password} oninput={() => (dirtyPassword = true)}
          placeholder={radio.ssid ? 'unchanged' : 'Wi-Fi password'} autocomplete="off" /></div>
    </div>
    {#if needsNetwork}<p class="mt-2 text-xs text-amber-300">Enter the Wi-Fi network the ESP32 should join.</p>{/if}
    <ul class="hint mt-4 list-disc space-y-1 pl-5">
      <li>Turn this on for both ESP32s of the mirror, and pair this computer with both.</li>
      <li>The mirrored data stays encrypted with your passphrase. Only paired apps can connect (encrypted, X25519 + AES-256-GCM), and turning Wi-Fi on or pairing needs a press of BOOT.</li>
      <li>The Wi-Fi password is stored on the ESP32 unencrypted, like on any Wi-Fi device.</li>
    </ul>
  </section>

  <section class="card">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="card-title mb-0"><Icon name="key" /> Paired computers</div>
      <button class="btn btn-sm {info.paired ? '' : 'btn-primary'}" disabled={!!busy} onclick={pair}><Icon name="plus" /> {busy === 'pair' ? 'Press BOOT…' : info.paired ? 'Pair this computer again' : 'Pair this computer'}</button>
    </div>
    {#if radio.clients?.length}
      <div class="divide-y divide-neutral-700/50">
        {#each radio.clients as c (c.kid)}
          <div class="flex items-center gap-3 py-2.5">
            <div class="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-neutral-700/60 text-violet-300"><Icon name="key" /></div>
            <div class="min-w-0 flex-1"><div class="truncate text-sm">{c.label}{c.kid === info.pairedAs ? ' (this computer)' : ''}</div><div class="font-mono text-xs text-neutral-500">{c.kid}</div></div>
            <button class="btn btn-sm btn-ghost text-rose-300" disabled={!!busy} onclick={() => unpair(c)}>Remove</button>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-sm text-neutral-400">No computer is paired yet. Pair this one so it can sync the mirror over Wi-Fi.</p>
    {/if}
  </section>

  <div class="sticky bottom-0 z-10 -mx-8 -mb-6 mt-5 flex flex-wrap items-center gap-3 border-t border-neutral-700/50 bg-[#1a1a1a]/95 px-8 py-3 backdrop-blur">
    <div class="min-w-0 flex-1 text-sm">
      {#if needsNetwork}<span class="text-amber-300">Enter the Wi-Fi network first.</span>
      {:else if changed}<span class="font-medium text-neutral-100">Unsaved changes.</span> <span class="text-neutral-400">Saving asks for a press of BOOT on the ESP32, which then restarts.</span>
      {:else}<span class="text-neutral-500">No changes.</span>{/if}
    </div>
    {#if changed}<button class="btn btn-ghost" disabled={!!busy} onclick={() => load(true)}>Undo</button>{/if}
    <button class="btn btn-primary" disabled={!changed || needsNetwork || !!busy} onclick={save}><Icon name="check" /> {busy === 'save' ? 'Saving…' : 'Save'}</button>
  </div>
{/if}

{#if confirming}
  <div class="fixed inset-0 z-40 grid place-items-center bg-black/60 backdrop-blur-sm">
    <div class="card w-96 text-center">
      <div class="mx-auto mb-3 h-3 w-3 animate-pulse rounded-full bg-white"></div>
      <div class="font-medium">Press BOOT on the ESP32</div>
      <p class="hint mt-2">Its LED flashes white while it waits (30 seconds), like a security key waiting for a touch.</p>
    </div>
  </div>
{/if}
