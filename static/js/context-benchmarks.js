/* Stored context/decode curves. Displaying a graph does not run an inference. */
const benchmarkColors = ['#22d3ee', '#fb923c', '#a78bfa', '#4ade80', '#f472b6', '#facc15', '#60a5fa', '#cbd5e1'];
const benchmarkLightColors = ['#0079b8', '#df6b13', '#7544ae', '#278544', '#be387b', '#997700', '#2563eb', '#475569'];
const benchmarkEscape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const benchmarkNumber = value => Number(value).toLocaleString('en-US', {maximumFractionDigits: 1});

function benchmarkSeries(doc, selected) {
  return (doc.models || []).filter(m => selected.has(m.id) && Array.isArray(m.points) && m.points.length)
    .map(m => ({...m, points: m.points.filter(p => p.input_tokens > 0 && p.decode_tps > 0).sort((a,b) => a.input_tokens-b.input_tokens)}));
}

function benchmarkInputTicks(maxInput) {
  if (maxInput <= 246272) return [0,20480,65536,131072,196608,245760].filter(n => n <= maxInput+512);
  const step = maxInput > 524288 ? 131072 : 65536;
  const ticks = Array.from({length:Math.floor((maxInput+512)/step)+1}, (_,i) => i*step);
  if (maxInput-ticks[ticks.length-1] > 8192) ticks.push(maxInput);
  return ticks;
}

function contextBenchmarkSVG(series) {
  if (!series.length) return '<div class="context-empty">No measurements yet for the selected models.</div>';
  const width=940, height=370, left=74, right=28, top=28, bottom=63;
  const maxInput=Math.max(1048576, ...series.flatMap(m => m.points.map(p => p.input_tokens)));
  const maxTps=Math.max(...series.flatMap(m => m.points.map(p => p.max_tps || p.decode_tps)));
  const step=maxTps>150?50:maxTps>80?25:10;
  const upper=Math.ceil(maxTps/step)*step;
  const x=n => left+n/maxInput*(width-left-right);
  const y=n => height-bottom-n/upper*(height-top-bottom);
  let svg=`<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Decode speed by actual input context tokens"><title>Input tokens on X; decode tokens per second on Y</title>`;
  for(let n=0;n<=upper;n+=step) svg+=`<line class="context-grid" x1="${left}" x2="${width-right}" y1="${y(n)}" y2="${y(n)}"/><text class="context-tick" text-anchor="end" x="${left-12}" y="${y(n)+5}">${n}</text>`;
  const ticks=benchmarkInputTicks(maxInput);
  for(const n of ticks) svg+=`<line class="context-grid" x1="${x(n)}" x2="${x(n)}" y1="${top}" y2="${height-bottom}"/><text class="context-tick" text-anchor="middle" x="${x(n)}" y="${height-bottom+26}">${n>=1048576?(n/1048576)+'M':n?Math.round(n/1024)+'k':'0'}</text>`;
  svg+=`<text class="context-axis" x="${left}" y="17">Decode · tokens/s</text><text class="context-axis" text-anchor="middle" x="${width/2}" y="${height-12}">Input context tokens · k = 1024 · M = 1,048,576</text>`;
  for(const m of series) {
    const color=m.color;
    svg+=`<polyline fill="none" stroke="${color}" stroke-width="3" points="${m.points.map(p=>x(p.input_tokens)+','+y(p.decode_tps)).join(' ')}"/>`;
    for(const p of m.points) {
      const tooltip=`${m.short} ${m.quant}: ${benchmarkNumber(p.input_tokens)} input tokens; ${p.decode_tps} tok/s; range ${p.min_tps}–${p.max_tps}; ${p.trials} trials`;
      svg+=`<line stroke="${color}" stroke-width="2" opacity=".65" x1="${x(p.input_tokens)}" x2="${x(p.input_tokens)}" y1="${y(p.min_tps||p.decode_tps)}" y2="${y(p.max_tps||p.decode_tps)}"/><circle cx="${x(p.input_tokens)}" cy="${y(p.decode_tps)}" r="5" fill="${color}" stroke="var(--bg)" stroke-width="2"><title>${benchmarkEscape(tooltip)}</title></circle>`;
    }
  }
  return svg+'</svg>';
}

async function showContextBenchmark(mid) {
  document.querySelector('#context-benchmark-overlay')?.closeBenchmark?.();
  const overlay=document.createElement('div');
  overlay.id='context-benchmark-overlay';
  overlay.className='modal-overlay';
  overlay.innerHTML=`<section class="modal context-modal" role="dialog" aria-modal="true" aria-labelledby="context-title">
    <div class="context-heading"><div><h2 id="context-title">Speed by context</h2><p>Median decode speed · 4 GPUs · 1 request · 512 output tokens</p></div><button class="btn btn-ghost" id="context-close" aria-label="Close speed graph">✕</button></div>
    <div class="context-toolbar"><button class="btn btn-ghost" id="context-compare">Compare all</button><button class="btn btn-ghost" id="context-refresh">Refresh</button><button class="btn btn-ghost" id="context-csv">Download CSV</button><span id="context-date"></span></div>
    <div id="context-legend" class="context-legend"></div><div id="context-chart" class="context-chart-wrap"><div class="context-empty">Loading measurements…</div></div>
    <div id="context-note" class="context-note"></div><div id="context-table" class="context-table-wrap"></div>
  </section>`;
  $('modal-root').appendChild(overlay);
  const previousFocus=document.activeElement;
  let doc=null, timer=null, selected=new Set([mid]);
  let initialSelection=true;
  function close() { if(timer) clearInterval(timer); document.removeEventListener('keydown',key); overlay.remove(); (previousFocus?.isConnected?previousFocus:document.getElementById('card-'+mid)?.querySelector('.context-speed'))?.focus(); }
  overlay.closeBenchmark=close;
  function key(e) {
    if(e.key==='Escape') close();
    if(e.key==='Tab') {
      const controls=[...overlay.querySelectorAll('button,input')].filter(el=>!el.disabled);
      const first=controls[0], last=controls[controls.length-1];
      if(e.shiftKey && document.activeElement===first) {e.preventDefault();last?.focus();}
      else if(!e.shiftKey && document.activeElement===last) {e.preventDefault();first?.focus();}
    }
  }
  document.addEventListener('keydown',key);
  overlay.addEventListener('click',e=>{if(e.target===overlay) close();});
  overlay.querySelector('#context-close').onclick=close;
  overlay.querySelector('#context-close').focus();
  function render() {
    const focusedId=document.activeElement?.dataset.mid;
    const palette=document.documentElement.getAttribute('data-theme')==='light'?benchmarkLightColors:benchmarkColors;
    const models=(doc.models||[]).map((m,i)=>({...m,color:palette[i%palette.length]}));
    overlay.querySelector('#context-date').textContent=`${doc.date||''} · ${doc.status==='running'?'Measurements in progress':doc.status==='complete'?'Measured':'Partial results'}`;
    overlay.querySelector('#context-legend').innerHTML=models.map(m=>`<label class="context-legend-item"><input type="checkbox" data-mid="${benchmarkEscape(m.id)}" ${selected.has(m.id)?'checked':''}><i style="background:${m.color}"></i><span>${benchmarkEscape(m.short)} <small>${benchmarkEscape(m.quant)}</small></span>${!m.points?.length?'<small>'+benchmarkEscape(m.status||'pending')+'</small>':''}</label>`).join('');
    overlay.querySelectorAll('[data-mid]').forEach(input=>input.onchange=()=>{const id=input.dataset.mid;if(input.checked) selected.add(id);else selected.delete(id);render();[...overlay.querySelectorAll('[data-mid]')].find(el=>el.dataset.mid===id)?.focus();});
    const series=benchmarkSeries({models},selected);
    overlay.querySelector('#context-chart').innerHTML=contextBenchmarkSVG(series);
    overlay.querySelector('#context-note').textContent='X = actual input tokens. Y = generation speed; prompt reading is excluded. The 20k target is 20,480 tokens; actual counts are in the table. Median of 3 trials at 20k, 2 elsewhere; bars show the measured range. Synthetic archive + Russian story, no prefix reuse. Thinking off. Axis extends to at least 1M tokens; lines only connect measured points.';
    const qualityNotes=series.filter(m=>m.quality_note).map(m=>`${m.name || m.short}: ${m.quality_note}`);
    if(qualityNotes.length) overlay.querySelector('#context-note').textContent+=' '+qualityNotes.join(' ');
    overlay.querySelector('#context-table').innerHTML=series.length?`<table class="context-table"><thead><tr><th>Model</th><th>Input tokens</th><th>Decode tok/s</th><th>Range</th><th>Trials</th><th>Retrieval</th></tr></thead><tbody>${series.flatMap(m=>m.points.map(p=>`<tr><td><i style="background:${m.color}"></i>${benchmarkEscape(m.short)} · ${benchmarkEscape(m.quant)}</td><td>${benchmarkNumber(p.input_tokens)}</td><td><b>${p.decode_tps}</b></td><td>${benchmarkNumber(p.min_tps)}–${benchmarkNumber(p.max_tps)}</td><td>${p.trials}</td><td>${p.retrieval_ok?'✓':'Failed'}</td></tr>`)).join('')}</tbody></table>`:'';
    if(focusedId) [...overlay.querySelectorAll('[data-mid]')].find(el=>el.dataset.mid===focusedId)?.focus();
  }
  async function refresh() {
    try {
      const result=await api('/api/benchmarks');
      if(!overlay.isConnected) return;
      doc=result;
      if(initialSelection && mid.includes('flash-next')) selected=new Set((doc.models||[]).filter(m=>m.id.includes('flash-next')).map(m=>m.id));
      initialSelection=false;render();
      if(doc.status==='running' && !timer) timer=setInterval(()=>{if(!document.hidden) refresh();},5000);
      if(doc.status!=='running' && timer) {clearInterval(timer);timer=null;}
    } catch(e) {if(overlay.isConnected) overlay.querySelector('#context-chart').innerHTML='<div class="context-empty">Could not load measurements. Try Refresh.</div>';}
  }
  overlay.querySelector('#context-compare').onclick=()=>{selected=new Set((doc?.models||[]).map(m=>m.id));if(doc) render();};
  overlay.querySelector('#context-refresh').onclick=refresh;
  overlay.querySelector('#context-csv').onclick=()=>{
    if(!doc) return;
    const quote=value=>'"'+String(value).replace(/"/g,'""')+'"';
    const rows=[['model','quant','input_tokens','decode_tps','min_tps','max_tps','trials','retrieval_ok']];
    for(const m of benchmarkSeries(doc,selected)) for(const p of m.points) rows.push([m.short,m.quant,p.input_tokens,p.decode_tps,p.min_tps,p.max_tps,p.trials,p.retrieval_ok]);
    const url=URL.createObjectURL(new Blob([rows.map(r=>r.map(quote).join(',')).join('\n')],{type:'text/csv;charset=utf-8'}));
    const a=document.createElement('a');a.href=url;a.download='ai-hub-context-benchmark.csv';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
  };
  await refresh();
}
