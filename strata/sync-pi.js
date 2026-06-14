const {serviceURL, hubURL, hubToken} = require('../scripts/env.cjs');
// Merge Strata into pi with only the largest context; trim the Universal alias.
const fs = require('fs');
const os = require('os');
const path = require('path');
const target = path.join(os.homedir(), '.pi', 'agent', 'models.json');
const before = fs.readFileSync(target, 'utf8');
const doc = JSON.parse(before);
const mayaKey = fs.readFileSync(path.join(os.homedir(), '.config', 'ai-hub', 'maya-glm53-api-key'), 'utf8').trim();
if (mayaKey.length < 32) throw new Error('Missing private Maya API key');
doc.providers['ai-hub-glm53-maya-m'] = {
  baseUrl: serviceURL(18037), api: 'openai-completions', apiKey: mayaKey,
  compat: {supportsDeveloperRole:false, supportsReasoningEffort:true, maxTokensField:'max_tokens', thinkingFormat:'openai'},
  models: [{id:'glm-5.3-flash', name:'GLM 5.3 Flash 321B Maya-M IQ2_S 256k', reasoning:true,
    input:['text','image'], contextWindow:262144, maxTokens:16384,
    thinkingLevelMap:{off:'none',minimal:null,low:'low',medium:null,high:'high',xhigh:'max',max:'max'},
    cost:{input:0,output:0,cacheRead:0,cacheWrite:0}}],
};
const deriskedKey = fs.readFileSync(path.join(os.homedir(), '.config', 'ai-hub', 'maya-glm53-derisked-api-key'), 'utf8').trim();
if (deriskedKey.length < 32) throw new Error('Missing private Derisked API key');
doc.providers['ai-hub-glm53-maya-m-derisked'] = structuredClone(doc.providers['ai-hub-glm53-maya-m']);
const derisked = doc.providers['ai-hub-glm53-maya-m-derisked'];
derisked.baseUrl = serviceURL(18038);
derisked.apiKey = deriskedKey;
derisked.models[0].id = 'glm-5.3-flash-derisked';
derisked.models[0].name = 'GLM 5.3 Flash 321B Derisked Maya-M IQ2_S 1M';
derisked.models[0].contextWindow = 1048576;
const managed = new Set(['ai-hub', 'ai-hub-flash-next-iq4', 'ai-hub-flash-next-uncensored-iq4', 'ai-hub-flash-next-iq3', 'ai-hub-gpt-oss-120b', 'ai-hub-qwen35-122b-q4k', 'ai-hub-qwen38-35b-q8']);
for (const [name, port, id, quant, keyName] of [
  ['ai-hub-flash-next-iq4', 18029, 'qwen3.8-flash-next-unsloth-ud-iq4_xs', 'UD-IQ4_XS', 'strata-api-key'],
  ['ai-hub-flash-next-uncensored-iq4', 18030, 'qwen3.8-flash-next-uncensored-ud-iq4_xs', 'Uncensored UD-IQ4_XS', 'strata-uncensored-iq4-api-key'],
  ['ai-hub-flash-next-iq3', 18033, 'qwen3.8-flash-next-iq3_xxs', 'IQ3_XXS', 'strata-iq3-api-key'],
]) {
  const key = fs.readFileSync(path.join(os.homedir(), '.config', 'ai-hub', keyName), 'utf8').trim();
  if (key.length < 32) throw new Error('Missing private API key for ' + name);
  doc.providers[name] = {
    baseUrl: serviceURL(port), api: 'openai-completions', apiKey: key,
    compat: {supportsDeveloperRole: false, supportsReasoningEffort: true,
      maxTokensField: 'max_tokens', thinkingFormat: 'openai'},
    models: [262144].map(ctx => ({
      // Strata accepts any model name on its dedicated endpoint.
      id: id + (ctx === 262144 ? '' : '-' + ctx / 1024 + 'k'),
      name: `Qwen 3.8 Flash-Next 125B ${quant} ${ctx / 1024}k`,
      reasoning: true, input: ['text'], contextWindow: ctx, maxTokens: 16384,
      thinkingLevelMap: {off: 'none', minimal: null, low: 'low', medium: 'medium',
        high: 'high', xhigh: null, max: null},
      cost: {input: 0, output: 0, cacheRead: 0, cacheWrite: 0},
    })),
  };
}
for (const [name, port, id, label, context] of [
  ['ai-hub-qwen35-122b-q4k', 18028, 'qwen3.5-122b', 'Qwen 3.5 122B Q4K', 262144],
  ['ai-hub-qwen38-35b-q8', 18027, 'qwen3.8-35b', 'Qwen 3.8 35B Q8', 262144],
  ['ai-hub-gpt-oss-120b', 18041, 'gpt-oss-120b', 'GPT-OSS 120B MXFP4', 131072],
]) {
  doc.providers[name] = {
    baseUrl: serviceURL(port), api: 'openai-completions', apiKey: 'none',
    compat: {supportsDeveloperRole: false, supportsReasoningEffort: name.includes('gpt-oss'), maxTokensField: 'max_tokens'},
    models: [{id, name: `${label} ${context/1024}k`, reasoning:true, input:['text','image'], contextWindow:context, maxTokens:32768,
      cost:{input:0,output:0,cacheRead:0,cacheWrite:0}}],
  };
  if (name.includes('gpt-oss')) doc.providers[name].models[0].input = ['text'];
  else {
    doc.providers[name].compat.thinkingFormat = 'chat-template';
    doc.providers[name].compat.chatTemplateKwargs = {enable_thinking: {$var:'thinking.enabled'}};
    doc.providers[name].models[0].thinkingLevelMap = {minimal:null,low:null,medium:null,high:'high',xhigh:null,max:null};
  }
}
const universal = doc.providers['ai-hub'];
if (universal?.models) {
  universal.baseUrl = hubURL();
  universal.apiKey = hubToken();
  universal.models = universal.models.filter(model => model.id !== 'universal-128k');
  const model = universal.models.find(model => model.id === 'universal');
  if (model) {
    model.contextWindow = 1048576;
    model.name = 'AI Hub Universal 1M';
  }
}
const after = JSON.stringify(doc, null, 2) + '\n';
if (after !== before) {
  fs.writeFileSync(target + '.bak.max-context-' + new Date().toISOString().replace(/[:.]/g, '-'), before);
  fs.writeFileSync(target, after);
}
const actual = JSON.parse(fs.readFileSync(target, 'utf8'));
for (const [name, provider] of Object.entries(JSON.parse(before).providers)) {
  if (['ai-hub-glm53-maya-m', 'ai-hub-glm53-maya-m-derisked'].includes(name)) continue;
  if (managed.has(name)) continue;
  if (JSON.stringify(actual.providers[name]) !== JSON.stringify(provider)) throw new Error('Existing provider changed: ' + name);
}
for (const name of ['ai-hub-flash-next-iq4', 'ai-hub-flash-next-uncensored-iq4']) {
  console.log(name + ': ' + actual.providers[name].models.map(m => m.name).join(' | '));
}
console.log('Verified pi configuration; maximum contexts retained and other providers preserved.');
