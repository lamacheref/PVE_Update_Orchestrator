async function api(p) { const r = await fetch(p); if (!r.ok) throw new Error(p + ' ' + r.status); return r.json(); }
function pill(t) {
  if (t.hold) return '<span class="pill hold">HOLD</span>';
  return t.ok ? '<span class="pill ok">OK</span>' : '<span class="pill fail">FAIL</span>';
}
async function loadRuns() {
  const runs = await api('/api/runs');
  document.getElementById('status').textContent = runs.length + ' run(s)';
  const box = document.getElementById('runs');
  box.innerHTML = runs.length ? '' : '<p>Aucun run — lancez <code>pve-orchestrator run</code>.</p>';
  for (const r of runs) {
    const d = document.createElement('div');
    d.className = 'card';
    d.innerHTML = `<b>${r.id}</b>${r.dry_run ? ' <i>dry-run</i>' : ''}<br>✅${r.ok} ❌${r.fail} ⏸️${r.hold} <small>${r.modified}</small>`;
    d.onclick = () => loadDetail(r.id);
    box.appendChild(d);
  }
  if (runs.length) loadDetail(runs[0].id);
}
async function loadDetail(id) {
  document.getElementById('runid').textContent = id;
  const run = await api('/api/runs/' + encodeURIComponent(id));
  const box = document.getElementById('detail');
  const names = Object.keys(run.targets || {}).sort();
  box.innerHTML = names.length ? '' : '<p>Run vide.</p>';
  for (const n of names) {
    const t = run.targets[n];
    const log = (t.log || []).slice(-4).join('\n');
    const err = t.error || '';
    box.innerHTML += `<div class="target">${pill(t)} <b>${n}</b> <small>${t.duration || ''}</small>` +
      (err ? `<pre class="err">${err.replace(/</g, '&lt;')}</pre>` : '') +
      (log ? `<pre>${log.replace(/</g, '&lt;')}</pre>` : '') + `</div>`;
  }
}
setInterval(loadRuns, 5000);
loadRuns().catch(e => { document.getElementById('status').textContent = 'erreur : ' + e.message; });
