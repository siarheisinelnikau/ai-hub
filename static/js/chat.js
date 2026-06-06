// Keep old /chat links working and select the exact WebUI serving profile.
(async () => {
  const config = window.AIHUB_CONFIG || {};
  const target = new URL(config.webuiURL || '/', location.origin);
  if (!config.webuiURL) {
    if (config.serverIP) target.hostname = config.serverIP.includes(':') ? `[${config.serverIP.replace(/^\[|\]$/g, '')}]` : config.serverIP;
    target.protocol = 'http:';
    target.port = '3000';
  }
  const fallback = document.getElementById('open-chat');
  fallback.href = target.href;
  const model = new URLSearchParams(location.search).get('model');
  try {
    if (model) {
      const response = await fetch('/static/webui-models.json?v=1', {cache: 'no-store'});
      if (!response.ok) throw new Error('Model list unavailable');
      const mapping = await response.json();
      const selected = Object.hasOwn(mapping.models, model) ? mapping.models[model] : null;
      if (!selected) throw new Error('Model is not configured in Open WebUI');
      target.searchParams.set('models', selected.id);
    }
    fallback.href = target.href;
    location.replace(target.href);
  } catch (_) {
    document.getElementById('status').textContent = 'Open WebUI to choose a model';
  }
})();
