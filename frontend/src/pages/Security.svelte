<script>
  import { app, Go, run, refreshStatus } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'
  import StorageBar from '../lib/StorageBar.svelte'
  import { onMount } from 'svelte'

  let settings = $state(null)
  onMount(async () => { settings = await Go.GetSettings() })
  async function saveSettings() { await run(() => Go.SaveSettings($state.snapshot(settings)), 'Settings saved') }

  let pin = $state({ first: '', repeat: '' })
  let change = $state({ old: '', first: '', repeat: '' })
  let st = $derived(app.status)
  let needsStorage = $derived(st.connected && !st.vaultStorageReady)
  let needsPIN = $derived(st.vaultStorageReady && st.pin?.notInitialized)
  const byteLength = (value) => new TextEncoder().encode(value).length

  async function provision() {
    if (await run(() => Go.InitializeVaultStorage(), 'Vault storage initialized')) await refreshStatus()
  }

  async function setupPIN(e) {
    e.preventDefault()
    if (pin.first !== pin.repeat) return run(() => Promise.reject('The passphrases do not match'))
    if (await run(() => Go.SetupVaultPIN(pin.first), 'Vault passphrase set')) {
      pin = { first: '', repeat: '' }
      await refreshStatus()
    }
  }

  async function setMode(mode) {
    if (mode === app.keyMode) return
    if (await run(() => Go.SetKeyMode(mode), mode === 2 ? '2-ESP mode on' : '1-ESP mode on')) await refreshStatus()
  }

  async function changePIN(e) {
    e.preventDefault()
    if (change.first !== change.repeat) return run(() => Promise.reject('The new passphrases do not match'))
    if (await run(() => Go.ChangePIN(change.old, change.first), 'Vault passphrase changed')) {
      change = { old: '', first: '', repeat: '' }
      await refreshStatus()
    }
  }
</script>

<section class="card mb-5 max-w-3xl">
  <div class="card-title"><Icon name="key" /> Key mode</div>
  <p class="mb-4 text-sm text-neutral-400">Choose how many ESP Crypts protect new vaults and SSH keys. Existing vaults keep the mode they were created with.</p>
  <div class="grid gap-3 sm:grid-cols-2">
    <button type="button" class="rounded-xl border p-4 text-left transition {app.keyMode === 1 ? 'border-violet-500/60 bg-violet-500/10' : 'border-neutral-700/50 hover:border-neutral-500/60'}" onclick={() => setMode(1)}>
      <div class="flex items-center gap-2 font-medium"><Icon name="chip" class="h-4 w-4 text-violet-300" /> 1 ESP</div>
      <p class="mt-1 text-xs text-neutral-400">The active chip alone opens vaults and signs SSH logins.</p>
    </button>
    <button type="button" class="rounded-xl border p-4 text-left transition {app.keyMode === 2 ? 'border-violet-500/60 bg-violet-500/10' : 'border-neutral-700/50 hover:border-neutral-500/60'}" onclick={() => setMode(2)}>
      <div class="flex items-center gap-2 font-medium"><Icon name="chip" class="h-4 w-4 text-violet-300" /><Icon name="chip" class="-ml-1.5 h-4 w-4 text-violet-300" /> 2 ESPs</div>
      <p class="mt-1 text-xs text-neutral-400">New vaults get a keyfile on each of two chips and open only while both are connected and unlocked. New SSH keys are made on both chips.</p>
    </button>
  </div>
  {#if app.keyMode === 2}
    <p class="hint mt-3">Choose the second chip under the active chip in the sidebar ({app.devices.find((d) => d.second)?.label ?? 'no second chip connected yet'}). Locking, switching or unplugging either chip locks its 2-ESP vaults.</p>
  {/if}
</section>

{#if settings}
  <section class="card mb-5 max-w-3xl">
    <div class="card-title"><Icon name="lock" /> Vaults</div>
    <label class="check items-start"><input type="checkbox" class="mt-0.5 shrink-0" bind:checked={settings.lockOnRemove} onchange={saveSettings} />
      <span><span class="font-medium">Lock vaults when the ESP Crypt is unplugged</span><span class="hint block">Open vaults stay open while the board, or its mirror board over USB, stays connected. Vaults never lock on a timer.</span></span></label>
  </section>
{/if}

{#if !st.connected}
  <div class="card text-sm text-neutral-400">Connect your custom ESP Crypt from the ESP32 page first.</div>
{:else}
  {#if st.storage?.totalEntries}
    <section class="card mb-5 max-w-3xl">
      <div class="card-title"><Icon name="database" /> Storage on this board</div>
      <StorageBar storage={st.storage} items={[
      { label: 'Encrypted container (keyfiles, SSH keys)', used: app.status.storage?.containerBytes ?? 0, max: app.status.storage?.maxContainerBytes ?? 0, bytes: true },
      ...(app.status.storage?.objects !== undefined && app.status.pin?.verified ? [{ label: 'Keyfiles and SSH keys', used: app.status.storage.objects, max: app.status.storage.maxObjects }] : []),
    ]} />
      <p class="hint mt-3">{st.passkey ? 'This board runs Vault + Passkey: the security key\'s passkeys use the same flash storage.' : 'Each keyfile or SSH key takes a few hundred bytes of the encrypted container.'}{st.pin?.verified ? '' : ' Unlock the Vault to see how many keyfiles and SSH keys are stored.'}</p>
    </section>
  {/if}

  <section class="card mb-5">
    <div class="card-title"><Icon name="shield" /> Reflashable device profile</div>
    <p class="mb-4 text-sm text-neutral-300">This firmware does not enable hardware Secure Boot or flash encryption and does not write eFuses. Current device state: Secure Boot {st.secureBoot ? 'already active' : 'off'}, flash encryption {st.flashEncrypted ? 'already active' : 'off'}. Existing hardware security settings can restrict ROM reflashing.</p>
    <dl class="kv">
      <dt>Secure Boot</dt><dd class="text-amber-200">{st.secureBoot ? 'Already active on this chip' : 'Off by design'}</dd>
      <dt>Flash encryption</dt><dd class="text-amber-200">{st.flashEncrypted ? 'Already active on this chip' : 'Off by design'}</dd>
      <dt>eFuse programming</dt><dd>None · not used by this firmware</dd>
      <dt>Vault storage</dt><dd class="font-sans">{st.vaultStorageReady ? 'NVS ready; keyfile container is encrypted with your passphrase' : 'Not initialized'}</dd>
      <dt>Vault passphrase</dt><dd class="font-sans">{st.pin?.text || 'Unknown'}</dd>
    </dl>
  </section>

  <section class="card mb-5 max-w-3xl border-amber-500/30">
    <div class="card-title text-amber-100"><Icon name="warning" /> What this protects</div>
    <p class="text-sm text-neutral-300">Keyfiles are stored inside an AES-GCM encrypted container derived from your passphrase. The rest of flash is not hardware-encrypted. Someone with physical access can replace the reflashable firmware and could capture a passphrase entered later, so use a long, unique passphrase and keep control of the board.</p>
  </section>

  {#if needsStorage}
    <section class="card max-w-2xl">
      <div class="card-title"><Icon name="database" /> Initialize Vault storage</div>
      <p class="mb-4 text-sm text-neutral-400">This initializes the NVS partition without erasing it or changing eFuses.</p>
      <button class="btn btn-primary" onclick={provision}>Initialize storage</button>
    </section>
  {:else if needsPIN}
    <form class="card max-w-xl space-y-3" onsubmit={setupPIN}>
      <div class="card-title"><Icon name="shield" /> Set Vault passphrase</div>
      <p class="text-sm text-neutral-400">Use a PIN or passphrase of 4–64 bytes. It encrypts the keyfile container; longer is much harder to brute-force if someone takes the chip. There is no recovery.</p>
      <input class="input" type="password" autocomplete="new-password" placeholder="PIN or passphrase (4–64 bytes)" bind:value={pin.first} />
      <input class="input" type="password" autocomplete="new-password" placeholder="Repeat passphrase" bind:value={pin.repeat} />
      <button class="btn btn-primary" disabled={byteLength(pin.first) < 4 || byteLength(pin.first) > 64}>Set passphrase</button>
    </form>
  {:else}
    <form class="card max-w-xl space-y-3" onsubmit={changePIN}>
      <div class="card-title"><Icon name="shield" /> Change Vault passphrase</div>
      <p class="text-sm text-neutral-400">The keyfile container will be encrypted with the new passphrase.</p>
      <input class="input" type="password" autocomplete="current-password" placeholder="Current passphrase" bind:value={change.old} />
      <input class="input" type="password" autocomplete="new-password" placeholder="New PIN or passphrase (4–64 bytes)" bind:value={change.first} />
      <input class="input" type="password" autocomplete="new-password" placeholder="Repeat new passphrase" bind:value={change.repeat} />
      <button class="btn btn-primary" disabled={!change.old || byteLength(change.first) < 4 || byteLength(change.first) > 64}>Change passphrase</button>
    </form>
  {/if}
{/if}
