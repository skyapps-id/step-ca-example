<script>
  let caStatus = $state(null)
  let tab = $state('issue')
  let subject = $state('')
  let sans = $state('')
  let duration = $state('720h')
  let provisioner = $state('admin')
  let issuing = $state(false)
  let error = $state('')
  let result = $state(null)
  let certs = $state([])
  let rootsPem = $state('')
  let provisioners = $state([])
  let newProvName = $state('')
  let newProvDefault = $state('24h')
  let newProvMax = $state('720h')
  let provBusy = $state(false)
  let provError = $state('')
  let renewMsg = $state('')
  let deployMode = $state('tls')

  const modeMeta = {
    'tls': { label: 'TLS', title: 'TLS — one-way authentication' },
    'mtls-server': { label: 'mTLS · Server', title: 'mTLS — server side' },
    'mtls-client': { label: 'mTLS · Client', title: 'mTLS — client side' },
  }

  // mermaid diagrams per connection type ("you" node is highlighted purple, keys are red)
  const diagrams = {
    tls: [
      'flowchart LR',
      '  K["🔑 server.key"] -.-> S',
      '  C["💻 Client"] -- "① TLS handshake" --> S["🖥️ Server"]',
      '  S -- "② server.crt" --> C',
      '  R["root_ca.crt"] -. "verify server" .-> C',
      '  classDef store fill:#121216,stroke:#232329,color:#ececf1',
      '  classDef secret fill:#121216,stroke:#f87171,stroke-dasharray:4 3,color:#ececf1',
      '  classDef you stroke:#8b5cf6,stroke-width:2px',
      '  class R store',
      '  class K secret',
      '  class S you',
    ].join('\n'),
    'mtls-server': [
      'flowchart LR',
      '  K["🔑 server.key"] -.-> S',
      '  C["💻 Client"] -- "① client.crt" --> S["🖥️ Server"]',
      '  S -- "② server.crt" --> C',
      '  R1["ca-chain.crt"] -. "verify client" .-> S',
      '  R2["root_ca.crt"] -. "verify server" .-> C',
      '  classDef store fill:#121216,stroke:#232329,color:#ececf1',
      '  classDef secret fill:#121216,stroke:#f87171,stroke-dasharray:4 3,color:#ececf1',
      '  classDef you stroke:#8b5cf6,stroke-width:2px',
      '  class R1 store',
      '  class R2 store',
      '  class K secret',
      '  class S you',
    ].join('\n'),
    'mtls-client': [
      'flowchart LR',
      '  K["🔑 client.key"] -.-> C',
      '  C["💻 Client"] -- "① client.crt" --> S["🖥️ Server"]',
      '  S -- "② server.crt" --> C',
      '  R1["ca-chain.crt"] -. "verify client" .-> S',
      '  R2["root_ca.crt"] -. "verify server" .-> C',
      '  classDef store fill:#121216,stroke:#232329,color:#ececf1',
      '  classDef secret fill:#121216,stroke:#f87171,stroke-dasharray:4 3,color:#ececf1',
      '  classDef you stroke:#8b5cf6,stroke-width:2px',
      '  class R1 store',
      '  class R2 store',
      '  class K secret',
      '  class C you',
    ].join('\n'),
  }

  // svelte action: render mermaid source into a node (lazy-loads mermaid)
  let mmdSeq = 0
  async function diagram(node, code) {
    const { default: mermaid } = await import('mermaid')
    mermaid.initialize({
      startOnLoad: false,
      theme: 'base',
      themeVariables: {
        background: 'transparent',
        primaryColor: '#17171c',
        primaryTextColor: '#ececf1',
        primaryBorderColor: '#232329',
        lineColor: '#8b8d98',
        edgeLabelBackground: '#121216',
        fontFamily: 'ui-sans-serif, system-ui, sans-serif',
        fontSize: '14px',
      },
      flowchart: { curve: 'basis', padding: 10 },
    })
    const render = async (src) => {
      try {
        const { svg } = await mermaid.render('mmd-' + (++mmdSeq), src)
        node.innerHTML = svg
      } catch {
        node.textContent = src
      }
    }
    await render(code)
    return { update: render }
  }

  async function renewCert(serial) {
    renewMsg = `Renewing ${serial.slice(0, 12)}...`
    try {
      const r = await fetch(`/api/certificates/${serial}/renew`, { method: 'POST' })
      const data = await r.json()
      if (!r.ok) throw new Error(data.error || 'renew failed')
      renewMsg = `✓ ${serial.slice(0, 12)} renewed — new expiry: ${new Date(data.notAfter).toLocaleString()}`
      await refreshList()
    } catch (err) {
      renewMsg = '✗ ' + err.message
    }
  }

  async function fetchRoots() {
    try {
      const r = await fetch('/api/ca/roots')
      const data = await r.json()
      rootsPem = (data.crts || []).join('\n')
    } catch { /* ignore */ }
  }

  async function refreshHealth() {
    try {
      const r = await fetch('/api/health')
      caStatus = await r.json()
    } catch {
      caStatus = { ca: 'down' }
    }
  }

  async function issueCert(e) {
    e.preventDefault()
    issuing = true
    error = ''
    result = null
    try {
      const body = {
        subject,
        sans: sans.split(',').map((s) => s.trim()).filter(Boolean),
        duration,
        provisioner,
      }
      const r = await fetch('/api/certificates', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await r.json()
      if (!r.ok) throw new Error(data.error || 'failed to issue certificate')
      result = data
      subject = ''
      sans = ''
      await refreshList()
    } catch (err) {
      error = err.message
    } finally {
      issuing = false
    }
  }

  async function refreshList() {
    try {
      const r = await fetch('/api/certificates')
      certs = await r.json()
    } catch { /* ignore */ }
  }

  async function fetchProvisioners() {
    try {
      const r = await fetch('/api/admin/provisioners')
      if (r.ok) provisioners = await r.json()
    } catch { /* ignore */ }
  }

  async function createProvisioner(e) {
    e.preventDefault()
    provBusy = true
    provError = ''
    try {
      const r = await fetch('/api/admin/provisioners', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newProvName,
          defaultDuration: newProvDefault,
          maxDuration: newProvMax,
        }),
      })
      const data = await r.json()
      if (!r.ok) throw new Error(data.error || 'failed to create provisioner')
      newProvName = ''
      await fetchProvisioners()
    } catch (err) {
      provError = err.message
    } finally {
      provBusy = false
    }
  }

  async function deleteProvisioner(name) {
    if (!confirm(`Delete provisioner '${name}'? This cannot be undone.`)) return
    provBusy = true
    provError = ''
    try {
      const r = await fetch(`/api/admin/provisioners/${name}`, { method: 'DELETE' })
      const data = await r.json()
      if (!r.ok) throw new Error(data.error || 'failed to delete provisioner')
      await fetchProvisioners()
    } catch (err) {
      provError = err.message
    } finally {
      provBusy = false
    }
  }

  function download(filename, content) {
    const blob = new Blob([content], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = filename
    a.click()
    URL.revokeObjectURL(a.href)
  }

  function fmtDate(iso) {
    return iso ? new Date(iso).toLocaleString() : '-'
  }

  function serverBundle() {
    return result.crt + '\n' + result.ca
  }

  function caChain() {
    return result.ca + '\n' + rootsPem
  }

  refreshHealth()
  refreshList()
  fetchRoots()
  fetchProvisioners()
  setInterval(refreshHealth, 10000)
</script>

<div class="app">
  <!-- ============ SIDEBAR ============ -->
  <aside>
    <div class="brand">
      <div class="logo">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 2l7 4v6c0 5-3.5 8.5-7 10-3.5-1.5-7-5-7-10V6l7-4z" stroke-linejoin="round"/>
          <path d="M9 12l2 2 4-4" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      </div>
      <div>
        <h1>step-ca</h1>
        <p>Dashboard</p>
      </div>
    </div>

    <nav>
      <button class:active={tab === 'issue'} onclick={() => (tab = 'issue')}>
        <span class="ico">⚡</span> Issue
      </button>
      <button class:active={tab === 'provisioners'} onclick={() => (tab = 'provisioners')}>
        <span class="ico">🔑</span> Provisioners
      </button>
      <button class:active={tab === 'issued'} onclick={() => (tab = 'issued')}>
        <span class="ico">📜</span> Certificates
        {#if certs.length}<span class="count">{certs.length}</span>{/if}
      </button>
    </nav>

    <div class="side-bottom">
      <div class="status" class:ok={caStatus?.ca === 'ok'} class:down={caStatus?.ca !== 'ok'}>
        <span class="dot"></span>
        <div>
          {#if caStatus}
            <strong>CA {caStatus.ca}</strong>
            {#if caStatus.version}<small>v{caStatus.version}</small>{/if}
          {:else}
            <strong>connecting…</strong>
          {/if}
        </div>
      </div>
    </div>
  </aside>

  <!-- ============ CONTENT ============ -->
  <main>
    {#if caStatus?.ca !== 'ok'}
      <div class="banner warn">
        CA unreachable at <code>{caStatus?.url || 'https://localhost:9000'}</code> —
        make sure <code>docker compose up -d</code> is running.
      </div>
    {/if}

    <!-- ===== TAB: ISSUE ===== -->
    {#if tab === 'issue'}
      <section class="card">
        <div class="card-head">
          <h2>Issue Certificate</h2>
          <p>Certificates are issued directly by the CA through the Admin API.</p>
        </div>
        <form onsubmit={issueCert} class="grid-2">
          <label class="span-2">
            <span>Connection Type <em>choose first — it drives the deployment guide below</em></span>
            <select bind:value={deployMode}>
              <option value="tls">TLS — server only</option>
              <option value="mtls-server">mTLS — Server</option>
              <option value="mtls-client">mTLS — Client</option>
            </select>
          </label>
          <label class="span-2">
            <span>Subject (CN)</span>
            <input bind:value={subject} placeholder="iot-sensor-02.local" required />
          </label>
          <label class="span-2">
            <span>Additional SANs <em>comma separated</em></span>
            <input bind:value={sans} placeholder="sensor2.lan, 10.0.0.5" />
          </label>
          <label>
            <span>Provisioner</span>
            <select bind:value={provisioner}>
              {#each provisioners.filter((p) => p.isLocal) as p}
                <option value={p.name}>{p.name}</option>
              {/each}
              {#if !provisioners.some((p) => p.name === provisioner)}
                <option value={provisioner}>{provisioner}</option>
              {/if}
            </select>
          </label>
          <label>
            <span>Validity</span>
            <select bind:value={duration}>
              <option value="1h">1 hour</option>
              <option value="24h">24 hours</option>
              <option value="168h">7 days</option>
              <option value="720h">30 days</option>
              <option value="8760h">1 year</option>
            </select>
          </label>
          <button type="submit" class="btn primary span-2" disabled={issuing || !subject}>
            {issuing ? 'Issuing…' : '⚡ Issue Certificate'}
          </button>
        </form>

        {#if error}<p class="msg err">{error}</p>{/if}

        {#if result}
          <div class="result">
            <div class="result-head">
              <span class="ok-badge">✓ Issued</span>
              <strong>{result.subject}</strong>
              <span class="dim">serial {result.serial.slice(0, 16)}… · exp {fmtDate(result.notAfter)}</span>
            </div>
            <p class="dim" style="margin:6px 0 0; font-size:0.8rem">
              Download files are available in the <strong>Deployment Guide</strong> cards below.
            </p>
            <details>
              <summary>certificate PEM</summary>
              <pre>{result.crt}</pre>
            </details>
          </div>
        {/if}
      </section>

      <section class="card">
        <div class="card-head guide-head">
          <div>
            <h2>Deployment Guide</h2>
            <p>Follows the Connection Type selected in the Issue form.</p>
          </div>
          <span class="badge mode-badge">{modeMeta[deployMode].label}</span>
        </div>

        {#if deployMode === 'tls'}
          <div class="explain">
            <h3>{modeMeta[deployMode].title}</h3>
            <p>
              Only the <strong>server</strong> proves its identity. The client verifies the
              server certificate against the root CA (trust anchor); the server never checks
              who the client is. Anyone may connect — the traffic is just encrypted.
            </p>
            <div class="diagram" use:diagram={diagrams.tls}></div>
            <p class="use"><strong>Best for:</strong> web apps, public REST APIs, dashboards — anything that only needs encryption + server authenticity.</p>
          </div>
          <div class="roles">
            <div class="role">
              <h4>🖥️ Server side</h4>
              <p>Certificate + private key installed on the web server (Nginx, Go, etc).</p>
              <div class="dl-list">
                <button disabled={!result} onclick={() => download(`${result.subject}-server.crt`, serverBundle())}>server.crt <small>leaf + intermediate</small></button>
                <button disabled={!result} onclick={() => download(`${result.subject}-server.key`, result.key)}>server.key <small>private key</small></button>
              </div>
            </div>
            <div class="role">
              <h4>💻 Client side</h4>
              <p>Only needs the root CA to verify the server — no certificate of its own.</p>
              <div class="dl-list">
                <button disabled={!rootsPem} onclick={() => download('root_ca.crt', rootsPem)}>root_ca.crt <small>trust anchor</small></button>
              </div>
            </div>
          </div>
        {:else if deployMode === 'mtls-server'}
          <div class="explain">
            <h3>{modeMeta[deployMode].title}</h3>
            <p>
              The server <strong>proves its identity</strong> to clients and simultaneously
              <strong>verifies incoming clients</strong> using <code>ca-chain.crt</code>.
              Connections are rejected unless the client presents a certificate signed by the same CA.
            </p>
            <div class="diagram" use:diagram={diagrams['mtls-server']}></div>
            <p class="use"><strong>Best for:</strong> internal APIs, backend services that accept connections only from trusted devices/services.</p>
          </div>
          <div class="roles">
            <div class="role">
              <h4>🖥️ Server side</h4>
              <p>Server certificate + chain used to verify incoming clients.</p>
              <div class="dl-list">
                <button disabled={!result} onclick={() => download(`${result.subject}-server.crt`, serverBundle())}>server.crt <small>leaf + intermediate</small></button>
                <button disabled={!result} onclick={() => download(`${result.subject}-server.key`, result.key)}>server.key <small>private key</small></button>
                <button disabled={!result || !rootsPem} onclick={() => download('ca-chain.crt', caChain())}>ca-chain.crt <small>intermediate + root</small></button>
              </div>
            </div>
          </div>
          <p class="hint">The client side needs <code>client.crt</code> + <code>client.key</code> + <code>root_ca.crt</code> — issue another one using the <strong>mTLS — Client</strong> mode.</p>
        {:else}
          <div class="explain">
            <h3>{modeMeta[deployMode].title}</h3>
            <p>
              The client <strong>proves its identity</strong> to the server using
              <code>client.crt</code> and <strong>verifies the server</strong> using
              <code>root_ca.crt</code>. The server only accepts clients whose certificates
              are signed by the same CA.
            </p>
            <div class="diagram" use:diagram={diagrams['mtls-client']}></div>
            <p class="use"><strong>Best for:</strong> IoT devices, agents, CLI/app clients accessing internal services.</p>
          </div>
          <div class="roles">
            <div class="role">
              <h4>📱 Client side</h4>
              <p>Client certificate as identity + root CA to verify the server.</p>
              <div class="dl-list">
                <button disabled={!result} onclick={() => download(`${result.subject}-client.crt`, result.crt)}>client.crt <small>client identity</small></button>
                <button disabled={!result} onclick={() => download(`${result.subject}-client.key`, result.key)}>client.key <small>private key</small></button>
                <button disabled={!rootsPem} onclick={() => download('root_ca.crt', rootsPem)}>root_ca.crt <small>trust anchor</small></button>
              </div>
            </div>
          </div>
          <p class="hint">The server side needs <code>server.crt</code> + <code>server.key</code> + <code>ca-chain.crt</code> — issue another one using the <strong>mTLS — Server</strong> mode.</p>
        {/if}
        {#if !result}<p class="hint">Downloads unlock once a certificate is issued in the form above.</p>{/if}
      </section>

    <!-- ===== TAB: PROVISIONERS ===== -->
    {:else if tab === 'provisioners'}
      <section class="card">
        <div class="card-head">
          <h2>Provisioners</h2>
          <p>Issuance entry points. ✓ = private key stored on this server.</p>
        </div>

        <form onsubmit={createProvisioner} class="prov-form">
          <input bind:value={newProvName} placeholder="new provisioner name" required
                 pattern="[a-zA-Z0-9][a-zA-Z0-9_-]*" />
          <select bind:value={newProvDefault} title="default validity">
            <option value="1h">def 1h</option>
            <option value="24h">def 24h</option>
            <option value="720h">def 30d</option>
            <option value="8760h">def 1y</option>
          </select>
          <select bind:value={newProvMax} title="max validity">
            <option value="720h">max 30d</option>
            <option value="8760h">max 1y</option>
          </select>
          <button class="btn primary" type="submit" disabled={provBusy || !newProvName}>+ Add</button>
        </form>
        {#if provError}<p class="msg err">{provError}</p>{/if}

        <table>
          <thead><tr><th>Name</th><th>Type</th><th>Key</th><th></th></tr></thead>
          <tbody>
            {#each provisioners as p}
              <tr>
                <td><strong>{p.name}</strong></td>
                <td><span class="badge">{p.type}</span></td>
                <td>
                  {#if p.isLocal}<span class="badge green">✓ local</span>{:else}<span class="badge dimb">—</span>{/if}
                </td>
                <td class="right">
                  {#if p.name !== 'admin'}
                    <button class="btn danger sm" onclick={() => deleteProvisioner(p.name)} disabled={provBusy}>Delete</button>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="4" class="empty">no data</td></tr>
            {/each}
          </tbody>
        </table>
        <p class="hint">“Key” shows whether the provisioner private key is stored on this dashboard (usable for issuing).</p>
      </section>

    <!-- ===== TAB: ISSUED ===== -->
    {:else}
      <section class="card">
        <div class="card-head">
          <h2>Issued Certificates</h2>
          {#if renewMsg}<p class="mono">{renewMsg}</p>{/if}
        </div>
        {#if certs.length === 0}
          <p class="empty">No certificates issued from this dashboard yet.</p>
        {:else}
          <div class="table-wrap">
            <table>
              <thead>
                <tr><th>Subject</th><th>Prov</th><th>Serial</th><th>Issuer</th><th>Expires</th><th></th></tr>
              </thead>
              <tbody>
                {#each certs as c}
                  <tr>
                    <td><strong>{c.subject}</strong><br /><small class="dim">{c.sans.filter((s) => s !== c.subject).join(', ') || '—'}</small></td>
                    <td><span class="badge">{c.provisioner || 'admin'}</span></td>
                    <td><code>{c.serial.slice(0, 12)}…</code></td>
                    <td class="dim">{c.issuer}</td>
                    <td>{fmtDate(c.notAfter)}</td>
                    <td class="right">
                      <div class="row-actions">
                        <a class="icon-btn" title="Download certificate (fullchain)" href={`/api/certificates/${c.serial}/download/crt`} download>⬇</a>
                        <a class="icon-btn" title="Download private key" href={`/api/certificates/${c.serial}/download/key`} download>🔑</a>
                        <button class="icon-btn" title="Renew certificate" onclick={() => renewCert(c.serial)}>⟳</button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </section>
    {/if}
  </main>
</div>

<style>
  :global(:root) {
    --bg: #09090b;
    --surface: #121216;
    --surface-2: #17171c;
    --border: #232329;
    --text: #ececf1;
    --muted: #8b8d98;
    --accent: #8b5cf6;
    --accent-2: #6366f1;
    --green: #34d399;
    --red: #f87171;
  }
  :global(body) {
    margin: 0;
    background: var(--bg);
    color: var(--text);
    font-family: ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif;
    min-height: 100vh;
  }
  :global(code), :global(pre), :global(.mono) {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
  }

  /* layout: sidebar + full-width content */
  .app { display: flex; min-height: 100vh; }

  aside {
    width: 240px; flex-shrink: 0;
    display: flex; flex-direction: column;
    background: var(--surface);
    border-right: 1px solid var(--border);
    position: sticky; top: 0; height: 100vh;
  }
  .brand { display: flex; align-items: center; gap: 12px; padding: 20px 18px; }
  .logo {
    width: 38px; height: 38px; border-radius: 11px;
    display: grid; place-items: center; flex-shrink: 0;
    background: linear-gradient(135deg, var(--accent), var(--accent-2));
    color: white;
    box-shadow: 0 4px 18px rgba(139, 92, 246, 0.35);
  }
  .logo svg { width: 20px; height: 20px; }
  .brand h1 { margin: 0; font-size: 0.95rem; }
  .brand p { margin: 1px 0 0; font-size: 0.72rem; color: var(--muted); }

  nav { display: flex; flex-direction: column; gap: 4px; padding: 8px 12px; flex: 1; }
  nav button {
    display: flex; align-items: center; gap: 10px;
    padding: 10px 12px; border-radius: 9px;
    background: transparent; color: var(--muted);
    font-size: 0.86rem; font-weight: 500; text-align: left;
    border: 1px solid transparent;
  }
  nav button:hover { background: var(--surface-2); color: var(--text); }
  nav button.active {
    background: var(--surface-2); color: var(--text);
    border-color: var(--border);
    box-shadow: inset 3px 0 0 var(--accent);
  }
  .ico { font-size: 0.95rem; }
  .count {
    margin-left: auto; min-width: 20px; text-align: center;
    background: rgba(139, 92, 246, 0.15); color: var(--accent);
    border-radius: 999px; padding: 1px 6px; font-size: 0.7rem; font-weight: 700;
  }

  .side-bottom { padding: 14px; border-top: 1px solid var(--border); }
  .status {
    display: flex; align-items: center; gap: 10px;
    padding: 10px 12px; border-radius: 10px;
    background: var(--surface-2); border: 1px solid var(--border);
    font-size: 0.8rem;
  }
  .status small { display: block; color: var(--muted); font-size: 0.7rem; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); flex-shrink: 0; }
  .status.ok .dot { background: var(--green); box-shadow: 0 0 8px var(--green); animation: pulse 2s infinite; }
  .status.down .dot { background: var(--red); }
  @keyframes pulse { 50% { opacity: 0.4; } }

  /* content: full width */
  main { flex: 1; padding: 24px 28px; min-width: 0; }

  .banner {
    margin: 0 0 16px; padding: 10px 14px; border-radius: 10px; font-size: 0.85rem;
    background: rgba(251, 191, 36, 0.08); border: 1px solid rgba(251, 191, 36, 0.25);
    color: #fcd34d;
  }
  .banner code { color: #fde68a; }

  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 14px;
    padding: 22px;
    margin-bottom: 18px;
  }
  .card-head { margin-bottom: 16px; }
  .card-head h2 { margin: 0 0 4px; font-size: 1rem; font-weight: 600; }
  .card-head p { margin: 0; font-size: 0.82rem; color: var(--muted); }
  .mono { font-family: ui-monospace, Menlo, monospace; font-size: 0.8rem; color: var(--muted); }

  .grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .span-2 { grid-column: span 2; }
  label { display: flex; flex-direction: column; gap: 5px; font-size: 0.8rem; color: var(--muted); }
  label span em { font-style: normal; opacity: 0.6; font-size: 0.72rem; }
  input, select {
    padding: 9px 12px; border-radius: 9px;
    border: 1px solid var(--border); background: var(--surface-2); color: var(--text);
    font-size: 0.88rem; outline: none; transition: border 0.15s, box-shadow 0.15s;
    width: 100%; box-sizing: border-box;
  }
  input:focus, select:focus { border-color: var(--accent); box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.15); }
  select {
    cursor: pointer; appearance: none; padding-right: 30px;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M0 0l5 6 5-6z' fill='%238b8d98'/%3E%3C/svg%3E");
    background-repeat: no-repeat; background-position: right 12px center;
  }
  select option { background: var(--surface-2); }

  button {
    font: inherit; cursor: pointer; border: none; border-radius: 9px;
    transition: transform 0.1s, opacity 0.15s, background 0.15s, border-color 0.15s;
  }
  button:disabled { opacity: 0.45; cursor: not-allowed; }
  .btn.primary {
    padding: 10px 18px; font-weight: 600; color: white;
    background: linear-gradient(135deg, var(--accent), var(--accent-2));
    box-shadow: 0 4px 16px rgba(139, 92, 246, 0.3);
  }
  .btn.primary:not(:disabled):hover { transform: translateY(-1px); }
  .btn.ghost {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); padding: 8px 12px; font-size: 0.8rem;
  }
  .btn.ghost:not(:disabled):hover { border-color: var(--accent); }
  .btn.sm { padding: 5px 10px; font-size: 0.75rem; }
  .btn.danger { background: rgba(248, 113, 113, 0.12); color: var(--red); border: 1px solid rgba(248, 113, 113, 0.3); }

  .msg { font-size: 0.85rem; padding: 8px 12px; border-radius: 8px; }
  .msg.err { background: rgba(248, 113, 113, 0.1); color: var(--red); border: 1px solid rgba(248, 113, 113, 0.25); }
  .hint { font-size: 0.72rem; color: var(--muted); margin: 10px 0 0; }
  .empty { color: var(--muted); font-size: 0.85rem; }
  .dim { color: var(--muted); }

  .result {
    margin-top: 16px; padding: 16px;
    border: 1px solid rgba(52, 211, 153, 0.3);
    background: rgba(52, 211, 153, 0.04); border-radius: 12px;
  }
  .result-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: 0.9rem; }
  .ok-badge {
    background: rgba(52, 211, 153, 0.15); color: var(--green);
    font-weight: 700; font-size: 0.72rem; padding: 3px 10px; border-radius: 999px;
  }
  .dl-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 12px; }
  details { margin-top: 10px; }
  summary { cursor: pointer; font-size: 0.8rem; color: var(--muted); }
  pre {
    background: var(--bg); padding: 12px; border-radius: 8px;
    overflow: auto; font-size: 0.7rem; max-height: 300px;
  }

  .prov-form { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 16px; }
  .prov-form input { flex: 2; min-width: 200px; }
  .prov-form select { flex: 1; min-width: 90px; }
  .prov-form button { flex: 0 0 auto; }

  .row-actions { display: flex; gap: 6px; justify-content: flex-end; }
  .icon-btn {
    width: 30px; height: 30px; display: inline-grid; place-items: center;
    background: var(--surface-2); border: 1px solid var(--border);
    border-radius: 8px; color: var(--muted); font-size: 0.85rem;
    text-decoration: none; transition: color 0.15s, border-color 0.15s;
  }
  a.icon-btn { cursor: pointer; }
  .icon-btn:hover { color: var(--text); border-color: var(--accent); }

  .table-wrap { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid var(--border); vertical-align: top; }
  th { color: var(--muted); font-weight: 500; font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.8px; }
  tbody tr:hover { background: var(--surface-2); }
  td.right { text-align: right; }
  code { background: var(--surface-2); padding: 2px 6px; border-radius: 5px; font-size: 0.78rem; }
  .badge {
    display: inline-block; padding: 2px 9px; border-radius: 999px; font-size: 0.7rem;
    background: var(--surface-2); border: 1px solid var(--border); color: var(--muted);
  }
  .badge.green { color: var(--green); border-color: rgba(52, 211, 153, 0.3); }
  .badge.dimb { opacity: 0.5; }

  .guide-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .mode-badge {
    font-size: 0.75rem; font-weight: 700; color: var(--accent);
    border-color: rgba(139, 92, 246, 0.4);
  }

  .explain { margin-bottom: 18px; }
  .explain h3 { margin: 0 0 8px; font-size: 0.92rem; }
  .explain p { margin: 0 0 12px; font-size: 0.84rem; color: var(--muted); line-height: 1.55; }
  .explain p strong { color: var(--text); }
  .diagram {
    display: grid; place-items: center;
    padding: 10px; margin-bottom: 12px;
    background: var(--bg); border: 1px solid var(--border); border-radius: 10px;
  }
  .diagram svg { max-width: 100%; height: auto; }
  .use { font-size: 0.78rem !important; }
  .use strong { color: var(--accent) !important; }

  .roles { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }
  .role {
    border: 1px solid var(--border); border-radius: 12px; padding: 16px;
    background: var(--surface-2);
  }
  .role h4 { margin: 0 0 6px; font-size: 0.85rem; }
  .role > p { margin: 0 0 12px; font-size: 0.76rem; color: var(--muted); }
  .dl-list { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; }
  .dl-list button {
    position: relative;
    display: flex; flex-direction: column; align-items: flex-start; gap: 2px;
    width: 100%; text-align: left;
    background: var(--bg); color: var(--text);
    border: 1px solid var(--border);
    padding: 8px 10px; font-size: 0.76rem; border-radius: 8px;
  }
  .dl-list button small { color: var(--muted); font-size: 0.64rem; padding-right: 18px; }
  .dl-list button::after {
    content: '⬇'; font-size: 0.7rem; color: var(--accent);
    position: absolute; top: 8px; right: 8px;
  }
  .dl-list button:disabled { color: var(--muted); }
  .dl-list button:disabled::after { content: '·'; color: var(--border); }
  .dl-list button:not(:disabled):hover {
    border-color: var(--accent);
    background: rgba(139, 92, 246, 0.08);
  }
</style>
