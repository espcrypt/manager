<script>
  let update = $state(null)
  let updating = $state(false)
  onMount(() => EventsOn('update:available', (u) => { update = u }))
  async function installUpdate() {
    updating = true
    const ok = await run(() => Go.InstallUpdate(), 'Installing; ESP Crypt restarts')
    if (!ok) updating = false
  }
  import { onMount } from 'svelte'
  import { app, Go, startEvents, refreshStatus, run, shortPort } from './lib/store.svelte.js'
  import Icon from './lib/Icon.svelte'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import Vaults from './pages/Vaults.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import Keyfiles from './pages/Keyfiles.svelte'
  import SSH from './pages/SSH.svelte'
  import Passkeys from './pages/Passkeys.svelte'
  import Mirror from './pages/Mirror.svelte'
  import Wireless from './pages/Wireless.svelte'
  import ESP32 from './pages/ESP32.svelte'
  import Security from './pages/Security.svelte'
  import Log from './pages/Log.svelte'

  const nav = [
    { id: 'dashboard', label: 'Setup & status', icon: 'dashboard', comp: Dashboard, intro: 'Connect and provision your custom ESP Crypt.' },
    { id: 'vaults', label: 'Vaults', icon: 'lock', comp: Vaults, intro: 'Create and unlock encrypted VeraCrypt containers with your ESP Crypt keyfiles.' },
    { id: 'keyfiles', label: 'VeraCrypt keyfiles', icon: 'file', comp: Keyfiles, intro: 'Manage VeraCrypt keyfiles in the passphrase-encrypted device container. Vault keyfiles are created automatically.' },
    { id: 'ssh', label: 'SSH keys', icon: 'terminal', comp: SSH, intro: 'SSH keys generated on your ESP32: ssh and git sign in with them, and the private keys never leave the chip.' },
    { id: 'passkeys', label: 'Passkeys', icon: 'key', comp: Passkeys, intro: 'ESP32-S3 security keys running the Passkey firmware: passkeys, PIN and settings. Windows, macOS and browsers use the key directly.' },
    { id: 'mirror', label: 'Backup & mirror', icon: 'database', comp: Mirror, intro: 'Keep a second ESP32 as an exact, automatically synced copy, or save an encrypted backup file.' },
    { id: 'wireless', label: 'Wireless', icon: 'wifi', comp: Wireless, intro: 'Optionally keep a mirrored ESP32 in sync over Wi-Fi when it is not plugged in. Off until you turn it on here over USB.' },
    { id: 'security', label: 'Passphrase & security', icon: 'shield', comp: Security, intro: 'Manage the Vault passphrase and review the reflashable security profile.' },
    { group: 'Device' },
    { id: 'esp32', label: 'ESP32 firmware', icon: 'chip', comp: ESP32, intro: 'Install signed custom firmware for ESP32-C3, ESP32-C6 and ESP32-S3.' },
    { id: 'log', label: 'Activity log', icon: 'list', comp: Log, intro: 'Everything the app did, with timestamps.' },
  ]
  let page = $derived(nav.find((n) => n.id === app.page) ?? nav[0])
  let Current = $derived(page.comp ?? Vaults)

  let pinInput = $state('')
  let confirmInput = $state('')
  let unlocking = $state(false)
  let activeDeviceId = $derived(app.devices.find((device) => device.active)?.id ?? '')

  async function selectDevice(e) {
    const id = e.currentTarget.value
    if (await run(() => Go.SetActiveVaultDevice(id))) await refreshStatus()
  }
  async function selectSecond(e) {
    const id = e.currentTarget.value
    if (await run(() => Go.SetSecondVaultDevice(id))) await refreshStatus()
  }
  let secondDevice = $derived(app.devices.find((d) => d.second))
  let newBoards = $derived(app.ports.filter((p) => p.likely && !p.passkey && !app.devices.some((d) => d.port === p.name)))
  let otherDevices = $derived(app.devices.filter((d) => !d.active))
  const shortLabel = (d) => `${d.chip} · ${d.serial ? d.serial.slice(-6) : d.port}`

  onMount(async () => {
    startEvents()
    app.logs = (await Go.GetLogs()) ?? []
    await refreshStatus()
    if ((!app.status.connected || app.status.pin?.notInitialized) && !app.openRequest) app.page = 'dashboard'
    const t = setInterval(refreshStatus, 4000)
    return () => clearInterval(t)
  })

  async function unlock(e) {
    e.preventDefault()
    if (unlocking || !pinInput) return
    unlocking = true
    const deviceId = app.pinPrompt?.deviceId
    const ok = await run(() => (deviceId ? Go.UnlockVaultDevice(deviceId, pinInput) : Go.Login(pinInput)))
    unlocking = false
    pinInput = ''
    await refreshStatus()
    const unlocked = deviceId ? app.devices.find((d) => d.id === deviceId)?.unlocked : app.status.pin?.verified
    if (ok && unlocked) {
      const p = app.pinPrompt
      app.pinPrompt = null
      p?.resolve?.(true)
    }
  }
  function cancelPin() {
    const p = app.pinPrompt
    app.pinPrompt = null
    p?.resolve?.(false)
  }

  const normName = (s) => (s ?? '').normalize('NFC').replace(/[‘’]/g, "'").replace(/[“”]/g, '"')
    .replace(/\s+/g, ' ').trim().toLowerCase()
  let confirmOk = $derived(!app.dialog?.typeToConfirm || normName(confirmInput) === normName(app.dialog.typeToConfirm))

  function closeDialog(result) {
    if (app.dialog && app.dialog.input !== undefined) result = result ? app.dialog.input : null
    app.dialog?.resolve(result)
    app.dialog = null
    confirmInput = ''
  }

  let st = $derived(app.status)

  let fake = $state(0)
  $effect(() => {
    if (!app.progress) { fake = 0; return }
    if (app.progress.total > 0) return
    fake = 3
    const t = setInterval(() => { fake = Math.min(95, fake + (95 - fake) * 0.06) }, 250)
    return () => clearInterval(t)
  })
  let pct = $derived(app.progress ? Math.round(app.progress.total > 0 ? Math.min(100, (app.progress.done / app.progress.total) * 100) : fake) : 0)
</script>

<svelte:window onkeydown={(e) => { if (e.key !== 'Escape') return; if (app.dialog) closeDialog(false); else if (app.pinPrompt) cancelPin() }} />

<div class="flex h-full">
  <aside class="flex w-60 shrink-0 flex-col border-r border-neutral-700/40 bg-[#141414]">
    <nav class="flex-1 space-y-0.5 overflow-y-auto px-3 pt-4">
      {#each nav as n}
        {#if n.group}
          <div class="px-3 pt-5 pb-1.5 text-[10px] font-semibold tracking-widest text-neutral-500 uppercase">{n.group}</div>
        {:else}
          <button
            class="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition {app.page === n.id ? 'bg-neutral-700/70 text-white shadow-sm' : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100'}"
            onclick={() => (app.page = n.id)}>
            <Icon name={n.icon} class="h-4 w-4 {app.page === n.id ? 'text-violet-400' : ''}" />
            {n.label}
          </button>
        {/if}
      {/each}
    </nav>
    <div class="space-y-4 border-t border-neutral-700/40 px-4 pt-5 pb-4 text-xs">
      <label class="block space-y-2">
        <span class="block text-neutral-500">Active ESP Crypt · {app.devices.filter((d) => d.transport !== 'wifi').length}</span>
        <select class="input w-full text-xs" value={activeDeviceId} onchange={selectDevice} aria-label="Select active ESP Crypt" title={app.devices.find((d) => d.active)?.label ?? ''}>
          <option value="">No active chip</option>
          {#each app.devices.filter((d) => d.transport !== 'wifi') as device (device.id)}
            <option value={device.id} title={`${device.label} · ${device.port}`}>{device.chip} · {device.serial ? device.serial.slice(-6) : shortPort(device.port)}</option>
          {/each}
        </select>
      </label>
      {#if app.keyMode === 2}
        <div class="space-y-2">
          <label class="block space-y-2">
            <span class="flex items-center justify-between text-neutral-500">Second ESP Crypt <span class="badge badge-muted">2-ESP</span></span>
            <select class="input w-full text-xs" value={secondDevice?.id ?? ''} onchange={selectSecond} aria-label="Select second ESP Crypt" disabled={!otherDevices.length}>
              {#if !otherDevices.length}<option value="">Connect a second chip</option>{/if}
              {#each otherDevices as device (device.id)}
                <option value={device.id} title={`${device.label} · ${device.port}`}>{shortLabel(device)}</option>
              {/each}
            </select>
          </label>
          {#if secondDevice}
            <div class="flex items-center justify-between gap-2">
              <span class="text-neutral-500">Second chip</span>
              {#if secondDevice.unlocked}
                <button class="badge badge-ok" title="Lock the second chip" onclick={() => run(async () => { await Go.LockVaultDevice(secondDevice.id); await refreshStatus() }, `${shortLabel(secondDevice)} locked`)}>unlocked</button>
              {:else}
                <button class="badge badge-warn" title="Unlock the second chip" onclick={() => (app.pinPrompt = { reason: '', deviceId: secondDevice.id, label: secondDevice.label })}>locked · unlock</button>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
      <div class="space-y-2.5 border-t border-neutral-700/30 pt-4">
        <div class="flex items-center justify-between gap-2">
          <span class="text-neutral-500">ESP Crypt</span>
          {#if st.connected}<span class="badge badge-ok">connected</span>{:else}<span class="badge badge-err">not connected</span>{/if}
        </div>
        <div class="flex items-center justify-between gap-2">
          <span class="text-neutral-500">Passphrase</span>
          {#if st.pin?.verified}<span class="badge badge-ok">unlocked</span>{:else if st.connected}<span class="badge badge-warn">locked</span>{:else}<span class="badge badge-muted">–</span>{/if}
        </div>
      </div>
      <button class="w-full rounded-lg px-2 py-1.5 text-left text-neutral-500 hover:bg-neutral-800 hover:text-neutral-200" onclick={() => Go.QuitApp()}>Quit ESP Crypt</button>
    </div>
  </aside>

  <main class="relative flex min-w-0 flex-1 flex-col overflow-x-hidden">
    <header class="flex h-14 shrink-0 items-center justify-between border-b border-neutral-700/40 px-8" style="--wails-draggable:drag">
      <h1 class="text-lg font-semibold">{page.label}</h1>
      <div class="flex items-center gap-2" style="--wails-draggable:no-drag">
        {#if st.connected && !st.pin?.verified && !st.pin?.notInitialized}
          <button class="btn btn-sm btn-primary" onclick={() => (app.pinPrompt = { reason: '' })}><Icon name="unlock" /> Unlock Vault</button>
        {:else if st.pin?.verified}
          <button class="btn btn-sm" onclick={() => run(async () => { await Go.Logout(); await refreshStatus() }, 'Vault locked')}><Icon name="lock" /> Lock Vault</button>
        {/if}
      </div>
    </header>
    {#if update?.available}
      <div class="flex items-center gap-3 border-b border-violet-500/30 bg-violet-500/10 px-8 py-2 text-sm">
        <span class="flex-1">ESP Crypt {update.latest} is available <span class="text-neutral-400">(you have {update.current})</span></span>
        <button class="btn btn-sm btn-ghost" disabled={updating} onclick={() => (update = null)}>Later</button>
        <button class="btn btn-sm btn-primary" disabled={updating} onclick={installUpdate}>{updating ? 'Updating…' : 'Update now'}</button>
      </div>
    {/if}
    <div class="flex-1 overflow-y-auto px-8 py-6">
      {#if page.intro}<p class="page-intro">{page.intro}</p>{/if}
      {#if newBoards.length && app.page !== 'esp32'}
        <div class="callout callout-info mb-5 flex flex-wrap items-center gap-4">
          <Icon name="chip" class="h-5 w-5 shrink-0" />
          <div class="flex-1 text-sm"><span class="font-medium">{newBoards.length === 1 ? `New ESP32 board on ${newBoards[0].name}` : `${newBoards.length} new ESP32 boards (${newBoards.map((p) => p.name).join(', ')})`}.</span> {newBoards.length === 1 ? 'It does not run the Vault firmware yet; flash it to use it as an ESP Crypt.' : 'They do not run the Vault firmware yet; flash them to use them as ESP Crypts.'}</div>
          <button class="btn btn-sm btn-primary" onclick={() => { app.flashPort = newBoards[0].name; app.page = 'esp32' }}><Icon name="bolt" /> Flash Vault firmware</button>
        </div>
      {/if}
      <Current />
    </div>

    {#if app.progress}
      <div class="flex items-center gap-4 border-t border-neutral-700/40 bg-[#141414] px-8 py-3">
        <div class="h-2 flex-1 overflow-hidden rounded-full bg-neutral-800">
          <div class="h-full rounded-full bg-violet-500 transition-all duration-300" style="width:{pct}%"></div>
        </div>
        <span class="w-10 text-right font-mono text-xs text-neutral-300 tabular-nums">{pct}%</span>
      </div>
    {/if}
  </main>
</div>

<div class="pointer-events-none fixed right-5 bottom-5 z-50 flex w-96 flex-col gap-2">
  {#each app.toasts as t (t.id)}
    <div class="pointer-events-auto rounded-xl border px-4 py-3 text-sm shadow-xl backdrop-blur {t.kind === 'err' ? 'border-rose-500/40 bg-rose-950/90 text-rose-100' : 'border-violet-500/30 bg-neutral-800/95 text-neutral-100'}">
      {t.msg}
    </div>
  {/each}
</div>

{#if app.pinPrompt}
  <div class="fixed inset-0 z-40 grid place-items-center bg-black/60 backdrop-blur-sm">
    <form class="card w-96" onsubmit={unlock}>
      <div class="card-title"><Icon name="unlock" /> Unlock {app.pinPrompt.label ?? 'ESP Crypt'}</div>
      {#if app.pinPrompt.reason}<p class="hint mb-3">{app.pinPrompt.reason}</p>{/if}
      {#if !app.pinPrompt.deviceId && !st.connected}<p class="mb-3 text-sm text-amber-200">Connect your custom ESP Crypt first.</p>{/if}
      <label class="label" for="pin">Vault passphrase</label>
      <!-- svelte-ignore a11y_autofocus -->
      <input id="pin" class="input text-lg" type="password" bind:value={pinInput} autofocus autocomplete="off"
        onkeydown={(e) => { if (e.key === 'Enter' && pinInput && !unlocking) unlock(e) }} />
      {#if !app.pinPrompt.deviceId && st.pin?.retries >= 0}<p class="hint mt-2">{st.pin.retries} attempts left.</p>{/if}
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" onclick={cancelPin}>Cancel</button>
        <button type="submit" class="btn btn-primary" disabled={!pinInput || unlocking || (!app.pinPrompt.deviceId && !st.connected)}>{unlocking ? 'Unlocking…' : 'Unlock'}</button>
      </div>
    </form>
  </div>
{/if}

{#if app.dialog}
  <div class="fixed inset-0 z-50 grid place-items-center bg-black/60 backdrop-blur-sm">
    <form class="card w-[28rem]" onsubmit={(e) => { e.preventDefault(); if (confirmOk) closeDialog(true) }}>
      <div class="card-title">{app.dialog.title}</div>
      {#if app.dialog.message}<p class="mb-4 text-sm whitespace-pre-line text-neutral-300">{app.dialog.message}</p>{/if}
      {#if app.dialog.input !== undefined}
        <!-- svelte-ignore a11y_autofocus -->
        <input class="input mb-4" type={app.dialog.secret ? 'password' : 'text'} bind:value={app.dialog.input} autofocus />
      {/if}
      {#if app.dialog.typeToConfirm}
        <label class="label" for="cf">Type <span class="font-mono normal-case tracking-normal text-rose-300">{app.dialog.typeToConfirm}</span> to confirm</label>
        <input id="cf" class="input mb-4" bind:value={confirmInput} autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" />
      {/if}
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" onclick={() => closeDialog(false)}>Cancel</button>
        <button type="submit" class="btn {app.dialog.danger ? 'btn-danger' : 'btn-primary'}"
          disabled={!confirmOk}>{app.dialog.confirmText}</button>
      </div>
    </form>
  </div>
{/if}
