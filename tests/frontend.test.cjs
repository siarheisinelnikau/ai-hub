const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

test('model artwork uses local allowlisted assets even for hostile names', () => {
  const context = vm.createContext({});
  vm.runInContext(fs.readFileSync('static/js/cards.js', 'utf8'), context);
  for (const [id, asset] of [['qwen-example', 'qwen'], ['gemma-example', 'gemma'], ['glm-example', 'glm'], ['nemotron-example', 'nemotron'], ['<img src=x onerror=alert(1)>', 'generic']]) {
    const html = context.modelArtwork({ id });
    assert.ok(html.includes(`/static/img/models/${asset}.svg`));
    assert.ok(!html.includes('onerror'));
    assert.ok(fs.existsSync(`static/img/models/${asset}.svg`));
  }
});

test('launch feasibility uses detected GPU memory', () => {
  const context = vm.createContext({ S: { cache: { gpus: [{ mem_total: 8192 }] } }, document: { addEventListener() {} } });
  vm.runInContext(fs.readFileSync('static/js/launch.js', 'utf8'), context);
  const caps = context.launchCaps({ parallel: { enabled: true, weights_gb: 10, kv_gb_per_100k: 1, max_gpus: 1, ctx_options: [8192] } });
  assert.equal(caps.fits(1, 8192, 1), false);
});

test('login clears legacy stored credentials and reports network errors', async () => {
  const nodes = { app: {}, tok: { value: 'test-only', focus() {} }, lerr: {}, 'login-submit': {} };
  const removed = [];
  const context = vm.createContext({
    $: id => nodes[id], localStorage: { getItem() { return null; }, removeItem(key) { removed.push(key); } },
    document: { addEventListener() {} }, fetch: async () => { throw new Error('offline'); },
  });
  vm.runInContext(fs.readFileSync('static/js/ui.js', 'utf8'), context);
  context.loginView();
  assert.ok(removed.includes('aih_token'));
  assert.ok(!nodes.app.innerHTML.includes('Remember token'));
  await context.doLogin();
  assert.match(nodes.lerr.textContent, /Cannot reach/);
  assert.equal(nodes['login-submit'].disabled, false);
});

test('client generators prefer the remote hub key and reject missing credentials', () => {
  const source = fs.readFileSync('scripts/env.cjs', 'utf8');
  function load(env, fileToken) {
    const module = { exports: {} };
    vm.runInNewContext(source, {
      module, __dirname: '/project/scripts', URL,
      process: { env: { AIHUB_ENV_FILE: '-', ...env } },
      require: name => name === 'node:fs' ? {
        existsSync: () => fileToken !== undefined,
        readFileSync: () => fileToken,
      } : name === 'node:os' ? { homedir: () => '/private' } : require(name),
    });
    return module.exports;
  }
  assert.equal(load({ AIHUB_TOKEN: 'local-test-token' }, 'remote-test-token\n').hubToken(), 'remote-test-token');
  assert.equal(load({ AIHUB_CLIENT_TOKEN: 'explicit-test-token' }, 'remote-test-token').hubToken(), 'explicit-test-token');
  assert.equal(load({ AIHUB_TOKEN: 'local-test-token' }).hubToken(), 'local-test-token');
  assert.throws(() => load({}).hubToken(), /authenticated AI-HUB API/);
  assert.throws(() => load({ AIHUB_CLIENT_TOKEN: 'none' }).hubToken(), /authenticated AI-HUB API/);
});

test('API instructions use the hub token for Maya and Qwen profiles', () => {
  let modal;
  const context = vm.createContext({
    hubURL: suffix => 'https://hub.example' + suffix,
    showModal: value => { modal = value; },
  });
  vm.runInContext(fs.readFileSync('static/js/page.js', 'utf8'), context);
  for (const [id, backend, model] of [
    ['glm53-flash-maya-m-derisked', 'Maya', 'glm-5.3-flash-derisked'],
    ['qwen38-flash-next-uncensored-strata', 'Strata', 'qwen3.8-flash-next-uncensored-ud-iq4_xs'],
  ]) {
    context.showApi(id, 18038, backend);
    assert.ok(modal.body.includes('https://hub.example/v1/model/' + id + '/chat/completions'));
    assert.ok(modal.body.includes('Bearer $AIHUB_TOKEN'));
    assert.ok(modal.body.includes(`"model": "${model}"`));
    assert.ok(!modal.body.includes('MAYA_API_KEY'));
    assert.ok(!modal.body.includes(':18038'));
  }
});

test('fresh harness configurations need only the hub key', () => {
  const os = require('node:os');
  const path = require('node:path');
  const {execFileSync} = require('node:child_process');
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'aihub-client-generation-'));
  try {
    const env = {...process.env, AIHUB_ENV_FILE:'-', AIHUB_CLIENT_TOKEN:'test-hub-token',
      AIHUB_PUBLIC_URL:'https://hub.example', AIHUB_CLIENT_TOKEN_FILE:path.join(temp, 'missing-key'),
      AIHUB_OPENCODE_OUTPUT:path.join(temp, 'opencode.jsonc'), AIHUB_PI_OUTPUT:path.join(temp, 'models.json')};
    for (const file of ['gen-opencode.js', 'strata/sync-pi.js']) execFileSync(process.execPath, [file], {env, stdio:'pipe'});
    const open = JSON.parse(fs.readFileSync(env.AIHUB_OPENCODE_OUTPUT, 'utf8'));
    const pi = JSON.parse(fs.readFileSync(env.AIHUB_PI_OUTPUT, 'utf8'));
    for (const provider of Object.values(open.provider)) {
      assert.equal(provider.options.apiKey, 'test-hub-token');
      assert.ok(provider.options.baseURL.startsWith('https://hub.example/v1'));
    }
    for (const provider of Object.values(pi.providers)) {
      assert.equal(provider.apiKey, 'test-hub-token');
      assert.ok(provider.baseUrl.startsWith('https://hub.example/v1'));
    }
    assert.equal(pi.providers['ai-hub-glm53-maya-m-derisked'].models[0].id, 'glm-5.3-flash-derisked');
    assert.equal(pi.providers['ai-hub-glm53-maya-m-derisked'].models[0].thinkingLevelMap.max, 'max');
    assert.equal(open.provider['qwen-q6k'].options.baseURL, 'https://hub.example/v1/model/qwen-q6k-llama');
  } finally { fs.rmSync(temp, {recursive:true, force:true}); }
});

test('migration preserves models, thinking, defaults and unrelated providers with private backups', () => {
  const os = require('node:os');
  const path = require('node:path');
  const {execFileSync} = require('node:child_process');
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'aihub-client-migration-'));
  try {
    const open = {model:'qwen-q6k/existing-alias', provider:{
      'qwen-q6k':{options:{baseURL:'http://old-host:18026/v1', apiKey:'none', chunkTimeout:123}, models:{'existing-alias':{id:'existing-alias', variants:{max:{reasoningEffort:'max'}}}}},
      external:{options:{baseURL:'https://external.example:18038/v1', apiKey:'keep-private'}, models:{}},
    }};
    const pi = {providers:{
      'ai-hub-glm53-maya-m-derisked':{baseUrl:'http://old-host:18038/v1', apiKey:'old-upstream-key', models:[{id:'glm-5.3-flash-derisked', thinkingLevelMap:{max:'max'}}]},
      external:{baseUrl:'https://external.example/v1', apiKey:'keep-private', models:[]},
    }};
    const env = {...process.env, AIHUB_ENV_FILE:'-', AIHUB_PUBLIC_URL:'https://hub.example',
      AIHUB_CLIENT_TOKEN:'test-hub-token', AIHUB_OPENCODE_OUTPUT:path.join(temp,'opencode.jsonc'), AIHUB_PI_OUTPUT:path.join(temp,'models.json')};
    fs.writeFileSync(env.AIHUB_OPENCODE_OUTPUT, JSON.stringify(open));
    fs.writeFileSync(env.AIHUB_PI_OUTPUT, JSON.stringify(pi));
    execFileSync(process.execPath, ['scripts/connect-clients.cjs'], {env, stdio:'pipe'});
    const actualOpen = JSON.parse(fs.readFileSync(env.AIHUB_OPENCODE_OUTPUT,'utf8'));
    const actualPi = JSON.parse(fs.readFileSync(env.AIHUB_PI_OUTPUT,'utf8'));
    assert.equal(actualOpen.model, open.model);
    assert.deepEqual(actualOpen.provider['qwen-q6k'].models, open.provider['qwen-q6k'].models);
    assert.deepEqual(actualOpen.provider.external, open.provider.external);
    assert.equal(actualOpen.provider['qwen-q6k'].options.chunkTimeout, 123);
    assert.equal(actualOpen.provider['qwen-q6k'].options.apiKey, 'test-hub-token');
    assert.equal(actualOpen.provider['qwen-q6k'].options.baseURL, 'https://hub.example/v1/model/qwen-q6k-llama');
    assert.deepEqual(actualPi.providers.external, pi.providers.external);
    assert.deepEqual(actualPi.providers['ai-hub-glm53-maya-m-derisked'].models, pi.providers['ai-hub-glm53-maya-m-derisked'].models);
    assert.equal(actualPi.providers['ai-hub-glm53-maya-m-derisked'].apiKey, 'test-hub-token');
    const files = fs.readdirSync(temp);
    assert.equal(files.filter(file => file.includes('.bak.hub-auth-')).length, 2);
    execFileSync(process.execPath, ['scripts/connect-clients.cjs'], {env, stdio:'pipe'});
    assert.deepEqual(fs.readdirSync(temp), files);
    if (process.platform !== 'win32') assert.equal(fs.statSync(env.AIHUB_PI_OUTPUT).mode & 0o777, 0o600);
  } finally { fs.rmSync(temp, {recursive:true, force:true}); }
});
