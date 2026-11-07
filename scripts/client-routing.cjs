// Known legacy client providers retain their IDs and settings through the hub.
const {modelURL, hubURL, hubToken} = require('./env.cjs');

const profiles = {
  18021:'qwen-q4k-llama', 18026:'qwen-q6k-llama', 18027:'qwen38-35b-q8',
  18028:'qwen35-122b-q4k', 18029:'qwen38-flash-next-strata',
  18030:'qwen38-flash-next-uncensored-strata', 18031:'gemma4-26b-q4k-mtp',
  18033:'qwen38-flash-next-iq3-strata', 18034:'nemotron35-super-q4k',
  18035:'nemotron35-super-nvfp4', 18036:'nemotron35-super-uncensored',
  18037:'glm53-flash-maya-m', 18038:'glm53-flash-maya-m-derisked', 18041:'gpt-oss-120b',
};
const opencodeNames = new Set([...Object.values(profiles), 'qwen-q4k', 'qwen-q6k']);

function profileURL(port) {
  const id = profiles[port];
  if (!id) throw new Error('Unknown ai-hub model port: ' + port);
  return modelURL(id);
}

function migrateProviders(doc, kind, token = hubToken()) {
  const providers = kind === 'pi' ? doc.providers : doc.provider;
  const changed = [];
  for (const [name, provider] of Object.entries(providers || {})) {
    const universal = name === (kind === 'pi' ? 'ai-hub' : 'universal');
    if (!universal && !(kind === 'pi' ? name.startsWith('ai-hub-') : opencodeNames.has(name))) continue;
    const options = kind === 'pi' ? provider : provider.options;
    const urlKey = kind === 'pi' ? 'baseUrl' : 'baseURL';
    if (!options) continue;
    let url;
    try { url = new URL(options[urlKey]); } catch { continue; }
    let baseURL;
    if (universal) baseURL = hubURL();
    else if (profiles[url.port]) baseURL = profileURL(url.port);
    else if (/\/v1\/model\/[^/]+\/?$/.test(url.pathname)) {
      const id = decodeURIComponent(url.pathname.split('/').filter(Boolean).at(-1));
      if (!Object.values(profiles).includes(id)) continue;
      baseURL = modelURL(id);
    } else continue;
    if (options[urlKey] !== baseURL || options.apiKey !== token) changed.push(name);
    options[urlKey] = baseURL;
    options.apiKey = token;
  }
  return changed;
}

module.exports = {profileURL, migrateProviders};
