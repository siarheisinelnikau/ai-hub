// Migrate existing generated OpenCode/pi configuration without changing choices.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {hubToken} = require('./env.cjs');
const {migrateProviders} = require('./client-routing.cjs');
const token = hubToken();

for (const [kind, target] of [
  ['opencode', process.env.AIHUB_OPENCODE_OUTPUT || path.join(os.homedir(), '.config', 'opencode', 'opencode.jsonc')],
  ['pi', process.env.AIHUB_PI_OUTPUT || path.join(os.homedir(), '.pi', 'agent', 'models.json')],
]) {
  if (!fs.existsSync(target)) { console.log(kind + ': no existing configuration; skipped'); continue; }
  const before = fs.readFileSync(target, 'utf8');
  const doc = JSON.parse(before);
  const changed = migrateProviders(doc, kind, token);
  if (changed.length) {
    const backup = target + '.bak.hub-auth-' + new Date().toISOString().replace(/[:.]/g, '-');
    fs.writeFileSync(backup, before, {mode:0o600, flag:'wx'});
    fs.writeFileSync(target, JSON.stringify(doc, null, 2) + '\n', {mode:0o600});
    if (process.platform !== 'win32') fs.chmodSync(target, 0o600);
  }
  console.log(kind + ': ' + changed.length + ' provider(s) migrated to the hub token; model IDs and defaults preserved');
}
