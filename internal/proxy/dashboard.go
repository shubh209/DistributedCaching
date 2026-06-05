package proxy

// dashboardHTML is the single-page dashboard served at GET /.
// Pure HTML/CSS/JS — no framework, no build step.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Distributed Cache Dashboard</title>
<style>
  :root {
    --bg: #0f1117;
    --surface: #1a1d27;
    --surface2: #22263a;
    --border: #2e3350;
    --text: #e2e8f0;
    --muted: #8892a4;
    --green: #10b981;
    --red: #ef4444;
    --yellow: #f59e0b;
    --blue: #3b82f6;
    --purple: #8b5cf6;
    --hit: #10b981;
    --miss: #f59e0b;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: var(--bg); color: var(--text); font-family: 'Inter', system-ui, sans-serif; font-size: 14px; line-height: 1.5; }
  a { color: var(--blue); text-decoration: none; }

  /* Layout */
  .layout { display: grid; grid-template-columns: 1fr 320px; gap: 20px; padding: 20px; max-width: 1400px; margin: 0 auto; }
  .main { min-width: 0; }
  .sidebar { position: sticky; top: 20px; height: fit-content; }

  /* Header */
  header { padding: 16px 20px; border-bottom: 1px solid var(--border); display: flex; align-items: center; gap: 12px; }
  header h1 { font-size: 18px; font-weight: 600; }
  header .subtitle { color: var(--muted); font-size: 13px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .4; } }

  /* Cards */
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 16px; margin-bottom: 16px; }
  .card-title { font-size: 13px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; margin-bottom: 12px; }

  /* Badges */
  .badge { display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; border-radius: 9999px; font-size: 11px; font-weight: 700; letter-spacing: .05em; }
  .badge-hit { background: rgba(16,185,129,.15); color: var(--green); border: 1px solid rgba(16,185,129,.3); }
  .badge-miss { background: rgba(245,158,11,.15); color: var(--yellow); border: 1px solid rgba(245,158,11,.3); }
  .badge-new { background: rgba(59,130,246,.15); color: var(--blue); border: 1px solid rgba(59,130,246,.3); }
  .badge-node { background: rgba(139,92,246,.15); color: var(--purple); border: 1px solid rgba(139,92,246,.3); font-size: 11px; }

  /* Product cards */
  .product-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
  .product-card { background: var(--surface2); border: 1px solid var(--border); border-radius: 8px; padding: 14px; transition: border-color .2s; }
  .product-card:hover { border-color: var(--blue); }
  .product-card .name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
  .product-card .desc { color: var(--muted); font-size: 12px; margin-bottom: 8px; }
  .product-card .price { font-size: 20px; font-weight: 700; color: var(--green); }
  .product-card .meta { display: flex; align-items: center; justify-content: space-between; margin-top: 10px; }
  .product-card .category { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: .05em; }
  .product-card .actions { display: flex; gap: 6px; margin-top: 10px; }

  /* Buttons */
  .btn { display: inline-flex; align-items: center; gap: 6px; padding: 6px 12px; border-radius: 6px; border: none; cursor: pointer; font-size: 13px; font-weight: 500; transition: opacity .15s; }
  .btn:hover { opacity: .85; }
  .btn-primary { background: var(--blue); color: #fff; }
  .btn-secondary { background: var(--surface2); color: var(--text); border: 1px solid var(--border); }
  .btn-danger { background: rgba(239,68,68,.15); color: var(--red); border: 1px solid rgba(239,68,68,.3); }
  .btn-sm { padding: 4px 8px; font-size: 12px; }
  .btn-green { background: var(--green); color: #fff; }

  /* Forms */
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .form-group { display: flex; flex-direction: column; gap: 4px; }
  .form-group.full { grid-column: 1 / -1; }
  label { font-size: 12px; color: var(--muted); font-weight: 500; }
  input, select { background: var(--bg); border: 1px solid var(--border); color: var(--text); border-radius: 6px; padding: 8px 10px; font-size: 13px; width: 100%; outline: none; transition: border-color .2s; }
  input:focus, select:focus { border-color: var(--blue); }

  /* Cluster panel */
  .node-list { display: flex; flex-direction: column; gap: 10px; }
  .node-item { display: flex; align-items: center; gap: 10px; padding: 10px 12px; background: var(--surface2); border-radius: 8px; border: 1px solid var(--border); }
  .node-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
  .node-dot.healthy { background: var(--green); box-shadow: 0 0 6px var(--green); }
  .node-dot.unhealthy { background: var(--red); }
  .node-dot.coordinator { background: var(--yellow); box-shadow: 0 0 6px var(--yellow); }
  .node-info { flex: 1; min-width: 0; }
  .node-name { font-weight: 600; font-size: 13px; display: flex; align-items: center; gap: 6px; }
  .node-stats { font-size: 11px; color: var(--muted); margin-top: 2px; }

  /* Debug panel */
  .debug-box { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 12px; font-family: monospace; font-size: 12px; color: var(--text); white-space: pre-wrap; word-break: break-all; min-height: 80px; }

  /* Fetch result */
  .fetch-result { border: 1px solid var(--border); border-radius: 8px; padding: 12px; margin-top: 12px; }
  .fetch-result .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
  .fetch-result .json { background: var(--bg); border-radius: 6px; padding: 10px; font-family: monospace; font-size: 12px; white-space: pre-wrap; overflow-x: auto; }

  /* Divider */
  .divider { height: 1px; background: var(--border); margin: 16px 0; }

  /* Toast */
  #toast { position: fixed; bottom: 20px; right: 20px; padding: 10px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; opacity: 0; transition: opacity .3s; z-index: 100; pointer-events: none; }
  #toast.show { opacity: 1; }
  #toast.success { background: rgba(16,185,129,.9); color: #fff; }
  #toast.error { background: rgba(239,68,68,.9); color: #fff; }

  /* Tabs */
  .tabs { display: flex; gap: 2px; margin-bottom: 16px; background: var(--surface2); padding: 3px; border-radius: 8px; border: 1px solid var(--border); }
  .tab { flex: 1; text-align: center; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; font-weight: 500; color: var(--muted); transition: all .2s; }
  .tab.active { background: var(--surface); color: var(--text); }

  /* Inline edit */
  .inline-edit { display: none; margin-top: 8px; }
  .inline-edit.open { display: flex; gap: 6px; align-items: center; }
  .inline-edit input { width: 90px; }

  /* Loading */
  .loading { color: var(--muted); font-size: 13px; text-align: center; padding: 20px; }

  /* Stats bar */
  .stats-bar { display: flex; gap: 16px; margin-bottom: 16px; }
  .stat { flex: 1; background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 12px; text-align: center; }
  .stat .value { font-size: 24px; font-weight: 700; }
  .stat .label { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; margin-top: 2px; }

  @media (max-width: 900px) {
    .layout { grid-template-columns: 1fr; }
    .sidebar { position: static; }
    .form-grid { grid-template-columns: 1fr; }
  }
</style>
</head>
<body>

<header>
  <div class="dot"></div>
  <div>
    <h1>Distributed Cache Dashboard</h1>
    <div class="subtitle">Product catalog · Cache cluster · Real-time metrics</div>
  </div>
</header>

<div class="layout">
<div class="main">

  <!-- Stats bar -->
  <div class="stats-bar">
    <div class="stat">
      <div class="value" id="stat-total">—</div>
      <div class="label">Products</div>
    </div>
    <div class="stat">
      <div class="value" style="color:var(--green)" id="stat-hits">—</div>
      <div class="label">Cache Hits</div>
    </div>
    <div class="stat">
      <div class="value" style="color:var(--yellow)" id="stat-misses">—</div>
      <div class="label">Cache Misses</div>
    </div>
    <div class="stat">
      <div class="value" style="color:var(--purple)" id="stat-nodes">—</div>
      <div class="label">Nodes Online</div>
    </div>
  </div>

  <!-- Tabs -->
  <div class="tabs">
    <div class="tab active" onclick="showTab('list')">Product List</div>
    <div class="tab" onclick="showTab('fetch')">Fetch by ID</div>
    <div class="tab" onclick="showTab('add')">Add Product</div>
  </div>

  <!-- Tab: Product List -->
  <div id="tab-list">
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
      <span id="product-count" style="color:var(--muted); font-size:13px;"></span>
      <button class="btn btn-secondary btn-sm" onclick="loadProducts()">↺ Refresh</button>
    </div>
    <div id="product-grid" class="product-grid">
      <div class="loading">Loading products…</div>
    </div>
  </div>

  <!-- Tab: Fetch by ID -->
  <div id="tab-fetch" style="display:none;">
    <div class="card">
      <div class="card-title">Fetch Product by ID</div>
      <div style="display:flex; gap:8px; align-items:center;">
        <input id="fetch-id" placeholder="e.g. prod-001" style="max-width:200px;" onkeydown="if(event.key==='Enter') fetchProduct()">
        <button class="btn btn-primary" onclick="fetchProduct()">Fetch</button>
      </div>
      <div id="fetch-result"></div>
    </div>
  </div>

  <!-- Tab: Add Product -->
  <div id="tab-add" style="display:none;">
    <div class="card">
      <div class="card-title">Add New Product</div>
      <div class="form-grid">
        <div class="form-group">
          <label>Product ID *</label>
          <input id="new-id" placeholder="prod-011">
        </div>
        <div class="form-group">
          <label>Name *</label>
          <input id="new-name" placeholder="Product name">
        </div>
        <div class="form-group full">
          <label>Description</label>
          <input id="new-desc" placeholder="Short description">
        </div>
        <div class="form-group">
          <label>Price (USD)</label>
          <input id="new-price" type="number" step="0.01" placeholder="29.99">
        </div>
        <div class="form-group">
          <label>Category</label>
          <input id="new-cat" placeholder="electronics">
        </div>
      </div>
      <div style="margin-top:12px; display:flex; gap:8px;">
        <button class="btn btn-green" onclick="addProduct()">Add Product</button>
        <button class="btn btn-secondary" onclick="clearForm()">Clear</button>
      </div>
      <div id="add-result" style="margin-top:10px;"></div>
    </div>
  </div>

</div><!-- /main -->

<div class="sidebar">

  <!-- Cluster status -->
  <div class="card">
    <div class="card-title" style="display:flex; justify-content:space-between; align-items:center;">
      Cluster Status
      <span style="font-size:11px; color:var(--muted); font-weight:400; text-transform:none; letter-spacing:0;">auto-refresh 2s</span>
    </div>
    <div id="cluster-nodes" class="node-list">
      <div class="loading">Loading…</div>
    </div>
  </div>

  <!-- Cache debug -->
  <div class="card">
    <div class="card-title">Cache Debug</div>
    <div style="display:flex; gap:6px; margin-bottom:10px;">
      <input id="debug-key" placeholder="product:prod-001" style="font-family:monospace; font-size:12px;">
      <button class="btn btn-secondary btn-sm" onclick="debugKey()">Debug</button>
    </div>
    <div id="debug-result" class="debug-box">Enter a cache key above to inspect routing…</div>
  </div>

  <!-- Links -->
  <div class="card">
    <div class="card-title">Observability</div>
    <div style="display:flex; flex-direction:column; gap:8px;">
      <a href="http://localhost:3000" target="_blank" class="btn btn-secondary" style="justify-content:center;">📊 Grafana Dashboards</a>
      <a href="http://localhost:9090" target="_blank" class="btn btn-secondary" style="justify-content:center;">📈 Prometheus</a>
    </div>
  </div>

</div><!-- /sidebar -->
</div><!-- /layout -->

<div id="toast"></div>

<script>
// ── State ──────────────────────────────────────────────────────────────────
let hits = 0, misses = 0;

// ── Tabs ───────────────────────────────────────────────────────────────────
function showTab(name) {
  ['list','fetch','add'].forEach(t => {
    document.getElementById('tab-'+t).style.display = t===name ? '' : 'none';
    document.querySelectorAll('.tab')[['list','fetch','add'].indexOf(t)].classList.toggle('active', t===name);
  });
  if (name==='list') loadProducts();
}

// ── Toast ──────────────────────────────────────────────────────────────────
function toast(msg, type='success') {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = 'show '+type;
  setTimeout(() => { el.className = ''; }, 2500);
}

// ── Products ───────────────────────────────────────────────────────────────
async function loadProducts() {
  try {
    const r = await fetch('/products?pageSize=50');
    const data = await r.json();
    const grid = document.getElementById('product-grid');
    document.getElementById('stat-total').textContent = data.total_count || 0;
    document.getElementById('product-count').textContent = data.total_count + ' products total';

    if (!data.products || data.products.length === 0) {
      grid.innerHTML = '<div class="loading">No products found. Add one!</div>';
      return;
    }

    grid.innerHTML = data.products.map(p => productCard(p, 'list')).join('');
  } catch(e) {
    document.getElementById('product-grid').innerHTML = '<div class="loading" style="color:var(--red)">Failed to load: ' + e.message + '</div>';
  }
}

function productCard(p, context) {
  return ` + "`" + `
  <div class="product-card" id="card-${p.id}">
    <div class="meta">
      <span class="category">${p.category}</span>
      <span class="badge badge-node">${ownerNode(p.id)}</span>
    </div>
    <div class="name">${p.name}</div>
    <div class="desc">${p.description || '—'}</div>
    <div class="price">$${p.price_usd.toFixed(2)}</div>
    <div class="actions">
      <button class="btn btn-secondary btn-sm" onclick="inspectProduct('${p.id}')">🔍 Inspect</button>
      <button class="btn btn-secondary btn-sm" onclick="toggleEdit('${p.id}')">✏️ Edit Price</button>
    </div>
    <div class="inline-edit" id="edit-${p.id}">
      <input type="number" step="0.01" id="price-${p.id}" placeholder="${p.price_usd}" style="width:90px;">
      <button class="btn btn-green btn-sm" onclick="updatePrice('${p.id}')">Save</button>
      <button class="btn btn-secondary btn-sm" onclick="toggleEdit('${p.id}')">✕</button>
    </div>
  </div>
  ` + "`" + `;
}

// Best-effort cache key → node lookup using the cluster state
let clusterState = { nodes: [], coordinator: '' };
function ownerNode(productId) {
  // We derive the cache key the same way the API does
  return '~' + (clusterState.coordinator || '?');
}

function toggleEdit(id) {
  const el = document.getElementById('edit-'+id);
  el.classList.toggle('open');
}

async function inspectProduct(id) {
  showTab('fetch');
  document.getElementById('fetch-id').value = id;
  await fetchProduct();
}

async function fetchProduct() {
  const id = document.getElementById('fetch-id').value.trim();
  if (!id) return;

  const r = await fetch('/products/' + id);
  const cacheStatus = r.headers.get('X-Cache') || r.headers.get('x-cache') || 'UNKNOWN';
  const data = await r.json();

  if (cacheStatus === 'HIT') hits++;
  else if (cacheStatus === 'MISS') misses++;
  updateHitStats();

  // Also fetch debug info
  let debugInfo = {};
  try {
    const dr = await fetch('/cache/debug/product:' + id);
    debugInfo = await dr.json();
  } catch(e) {}

  const badgeClass = cacheStatus === 'HIT' ? 'badge-hit' : cacheStatus === 'MISS' ? 'badge-miss' : 'badge-new';
  document.getElementById('fetch-result').innerHTML = ` + "`" + `
    <div class="fetch-result">
      <div class="header">
        <span class="badge ${badgeClass}">${cacheStatus}</span>
        ${debugInfo.owner_node ? '<span class="badge badge-node">Node: ' + debugInfo.owner_node + '</span>' : ''}
        ${debugInfo.in_cache !== undefined ? '<span style="color:var(--muted); font-size:12px;">' + (debugInfo.in_cache ? '✓ In cache' : '○ Not in cache') + '</span>' : ''}
      </div>
      <div class="json">${JSON.stringify(r.status === 200 ? data : {error: data.error}, null, 2)}</div>
    </div>
  ` + "`" + `;
}

async function updatePrice(id) {
  const input = document.getElementById('price-'+id);
  const price = parseFloat(input.value);
  if (isNaN(price) || price < 0) { toast('Invalid price', 'error'); return; }

  const r = await fetch('/products/'+id, {
    method: 'PUT',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({price_usd: price})
  });
  const data = await r.json();

  if (r.ok) {
    toast('Price updated → $' + price.toFixed(2));
    toggleEdit(id);
    loadProducts();
    // Debug the key to show what happened in the cluster
    document.getElementById('debug-key').value = 'product:'+id;
    await debugKey();
  } else {
    toast(data.error || 'Update failed', 'error');
  }
}

async function addProduct() {
  const id = document.getElementById('new-id').value.trim();
  const name = document.getElementById('new-name').value.trim();
  if (!id || !name) { toast('ID and name are required', 'error'); return; }

  const body = {
    id,
    name,
    description: document.getElementById('new-desc').value.trim(),
    price_usd: parseFloat(document.getElementById('new-price').value) || 0,
    category: document.getElementById('new-cat').value.trim() || 'general',
  };

  const r = await fetch('/products', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify(body)
  });
  const data = await r.json();

  const resultEl = document.getElementById('add-result');
  if (r.ok) {
    toast('Product created: ' + name);
    clearForm();
    resultEl.innerHTML = '<div style="color:var(--green); font-size:13px;">✓ Created: ' + name + ' (ID: ' + id + ')</div>';
  } else {
    resultEl.innerHTML = '<div style="color:var(--red); font-size:13px;">✗ ' + (data.error || 'Failed') + '</div>';
    toast(data.error || 'Failed to create', 'error');
  }
}

function clearForm() {
  ['new-id','new-name','new-desc','new-price','new-cat'].forEach(id => {
    document.getElementById(id).value = '';
  });
  document.getElementById('add-result').innerHTML = '';
}

// ── Cluster Status ─────────────────────────────────────────────────────────
async function refreshCluster() {
  try {
    const r = await fetch('/cluster/status');
    const data = await r.json();
    clusterState = data;

    const nodesEl = document.getElementById('cluster-nodes');
    const online = (data.nodes || []).filter(n => n.healthy).length;
    document.getElementById('stat-nodes').textContent = online + '/' + (data.nodes || []).length;

    nodesEl.innerHTML = (data.nodes || []).map(n => {
      const isCoord = n.node_id === data.coordinator;
      const dotClass = !n.healthy ? 'unhealthy' : isCoord ? 'coordinator' : 'healthy';
      const role = isCoord ? ' 👑 Coordinator' : '';
      const hitRate = n.hit_count + n.miss_count > 0
        ? ((n.hit_count / (n.hit_count + n.miss_count)) * 100).toFixed(1) + '% hit'
        : 'no traffic';
      return ` + "`" + `
        <div class="node-item">
          <div class="node-dot ${dotClass}"></div>
          <div class="node-info">
            <div class="node-name">${n.node_id}${role}</div>
            <div class="node-stats">
              ${n.healthy ? n.entry_count + ' entries · ' + hitRate : '<span style="color:var(--red)">unreachable</span>'}
              ${n.error ? ' · ' + n.error : ''}
            </div>
          </div>
        </div>
      ` + "`" + `;
    }).join('');
  } catch(e) {
    document.getElementById('cluster-nodes').innerHTML = '<div class="loading" style="color:var(--red)">Cluster unreachable</div>';
  }
}

// ── Cache Debug ────────────────────────────────────────────────────────────
async function debugKey() {
  const key = document.getElementById('debug-key').value.trim();
  if (!key) return;

  try {
    const r = await fetch('/cache/debug/' + encodeURIComponent(key));
    const data = await r.json();
    const cacheColor = data.in_cache ? 'var(--green)' : 'var(--yellow)';
    document.getElementById('debug-result').innerHTML =
      '<span style="color:var(--muted)">key:</span>          ' + data.key + '\n' +
      '<span style="color:var(--muted)">owner_node:</span>   <strong>' + data.owner_node + '</strong>\n' +
      '<span style="color:var(--muted)">cache_status:</span> <span style="color:' + cacheColor + '">' + data.cache_status + '</span>\n' +
      '<span style="color:var(--muted)">in_cache:</span>     ' + data.in_cache + '\n' +
      '<span style="color:var(--muted)">is_coordinator:</span> ' + data.is_coordinator;
  } catch(e) {
    document.getElementById('debug-result').textContent = 'Error: ' + e.message;
  }
}

// ── Stats ──────────────────────────────────────────────────────────────────
function updateHitStats() {
  document.getElementById('stat-hits').textContent = hits;
  document.getElementById('stat-misses').textContent = misses;
}

// ── Init ───────────────────────────────────────────────────────────────────
loadProducts();
refreshCluster();
setInterval(refreshCluster, 2000);
</script>
</body>
</html>`
