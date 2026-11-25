<script>
  import { onMount } from 'svelte'
  import { app, Go, run, refreshStatus } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let ports = $state([])
  let port = $state('')
  let vaults = $state([])
  let vcInstalled = $state(false)

  async function load() {
    ports = (await Go.ListSerialPorts().catch(() => [])) ?? []
    if (!port || !ports.some((p) => p.name === port)) port = ports.find((p) => p.likely)?.name ?? ports[0]?.name ?? ''
    vaults = (await Go.ListVaults().catch(() => [])) ?? []
    vcInstalled = (await Go.VCInfo().catch(() => ({ installed: false }))).installed
  }
  onMount(() => {
    load()
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  })

  let st = $derived(app.status)
  let storageReady = $derived(st.vaultStorageReady)
  let initialized = $derived(storageReady && st.pin && !st.pin.notInitialized)
  let steps = $derived([
    { done: st.connected, title: 'Connect custom Vault firmware', text: st.connected ? `Connected: ${st.serial || st.port}` : 'Flash the signed, reflashable C3, C6 or S3 Vault image from the ESP32 page, then connect its serial port.', page: 'esp32', action: 'Open ESP32' },
    { done: storageReady, title: 'Prepare Vault storage', text: storageReady ? 'NVS is ready; keyfiles are kept in a passphrase-encrypted container.' : 'Initialize NVS. This does not burn eFuses or erase the flash.', page: 'security', action: 'Prepare storage' },
    { done: initialized, title: 'Set a Vault passphrase', text: 'Choose a long passphrase to encrypt VeraCrypt keyfiles stored on the device.', page: 'security', action: 'Set passphrase' },
    { done: !!st.pin?.verified, title: 'Unlock the Vault', text: 'Enter your passphrase to create or use device keyfiles.', unlock: true, action: 'Unlock' },
    { done: vcInstalled, title: 'Install VeraCrypt', text: 'Install the vault engine to create encrypted containers.', page: 'vaults', action: 'Install' },
    { done: vaults.length > 0, title: 'Create your first vault', text: 'Create an encrypted VeraCrypt container protected by a keyfile on your ESP32.', page: 'vaults', action: 'Create vault' },
  ])
  let next = $derived(steps.findIndex((s) => !s.done))
  let doneCount = $derived(steps.filter((s) => s.done).length)

  async function connect() {
    if (await run(() => Go.ConnectVaultDevice(port), 'Custom Vault connected')) await refreshStatus()
  }
</script>

<section class="card mb-5">
  <div class="mb-1 flex flex-wrap items-center justify-between">
    <div class="card-title mb-0"><Icon name="check" /> Getting started</div>
    <span class="text-xs text-neutral-400">{doneCount} of {steps.length} done</span>
  </div>
  <div class="mb-4 h-1.5 overflow-hidden rounded-full bg-neutral-700/60"><div class="h-full bg-violet-500 transition-all" style="width:{(doneCount / steps.length) * 100}%"></div></div>
  <div class="grid gap-2 lg:grid-cols-2">
    {#each steps as s, i}
      <div class="step {i === next ? 'border-violet-500/40 bg-violet-500/5' : ''}">
        <div class="step-num {s.done ? 'bg-violet-500 text-neutral-950' : i === next ? 'bg-neutral-200 text-neutral-900' : 'bg-neutral-700 text-neutral-300'}">
          {#if s.done}<Icon name="check" class="h-4 w-4" />{:else}{i + 1}{/if}
        </div>
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium {s.done ? 'text-neutral-400' : ''}">{s.title}</div>
          <div class="mt-0.5 text-xs text-neutral-400">{s.text}</div>
        </div>
        {#if !s.done && i === next}
          <button class="btn btn-sm btn-primary self-center" onclick={() => (s.unlock ? (app.pinPrompt = { reason: '' }) : (app.page = s.page))}>{s.action}</button>
        {/if}
      </div>
    {/each}
  </div>
</section>

<div class="grid gap-5 xl:grid-cols-2">
  <section class="card">
    <div class="card-title"><Icon name="chip" /> Custom ESP Crypt</div>
    {#if st.connected}
      <dl class="kv">
        <dt>Device</dt><dd>{st.serial || st.deviceType}</dd>
        <dt>Firmware signing</dt><dd class="text-violet-300">Signed OTA verification enabled</dd>
        <dt>ROM reflash</dt><dd class={st.secureBoot || st.flashEncrypted ? 'text-rose-300' : 'text-violet-300'}>{st.secureBoot || st.flashEncrypted ? 'Restricted by existing hardware security' : 'Available · no eFuse lock'}</dd>
        <dt>Flash encryption</dt><dd class="text-amber-200">{st.flashEncrypted ? 'Already active' : 'Off by design'}</dd>
        <dt>Vault storage</dt><dd class={st.vaultStorageReady ? 'text-violet-300' : 'text-amber-300'}>{st.vaultStorageReady ? 'NVS ready' : 'not initialized'}</dd>
        <dt>Vault passphrase</dt><dd class="font-sans">{st.pin?.text || 'unknown'}</dd>
        <dt>Serial port</dt><dd class="text-xs">{st.port}</dd>
      </dl>
      <button class="btn btn-sm mt-5" onclick={() => run(async () => { await Go.Disconnect(); await refreshStatus() })}>Disconnect</button>
      <button class="btn btn-sm mt-2" onclick={() => (app.page = 'esp32')}>Connect or flash another board</button>
    {:else}
      <p class="mb-4 text-sm text-neutral-400">Connect the custom firmware here. A board in ROM download mode must be flashed from the ESP32 page first.</p>
      <div class="flex gap-2">
        <select class="input min-w-0 flex-1" bind:value={port}>
          <option value="">{ports.length ? 'Choose serial port' : 'No serial ports found'}</option>
          {#each ports as p}<option value={p.name}>{p.name} · {p.kind}</option>{/each}
        </select>
        <button class="btn btn-primary" disabled={!port} onclick={connect}>Connect</button>
      </div>
      <button class="btn btn-sm mt-3" onclick={() => (app.page = 'esp32')}>Flash custom firmware</button>
    {/if}
  </section>

  <section class="card">
    <div class="card-title"><Icon name="folder" /> Quick actions</div>
    <div class="grid grid-cols-2 gap-2">
      <button class="btn justify-start" onclick={() => (app.page = 'vaults')}><Icon name="lock" /> {vaults.length} vault(s)</button>
      <button class="btn justify-start" onclick={() => (app.page = 'keyfiles')}><Icon name="file" /> VeraCrypt keyfiles</button>
      <button class="btn justify-start" onclick={() => (app.page = 'ssh')}><Icon name="terminal" /> SSH keys</button>
      <button class="btn justify-start" onclick={() => (app.page = 'security')}><Icon name="shield" /> Passphrase & security</button>
      <button class="btn justify-start" onclick={() => (app.page = 'esp32')}><Icon name="chip" /> Firmware & device</button>
    </div>
    <p class="hint mt-4">The custom firmware stores VeraCrypt keyfiles and SSH keys in a passphrase-encrypted container. SSH private keys are generated on the chip and never leave it.</p>
  </section>
</div>
