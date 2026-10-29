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
  assert.throws(() => load({}).hubToken(), /authenticated Universal API/);
  assert.throws(() => load({ AIHUB_CLIENT_TOKEN: 'none' }).hubToken(), /authenticated Universal API/);
});
