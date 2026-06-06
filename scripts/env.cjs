// Node 22+ provides a literal .env loader; no shell execution or dependencies.
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const explicit = process.env.AIHUB_ENV_FILE;
if (explicit !== '-') {
  const candidates = explicit ? [explicit] : [
    ...(process.env.AIHUB ? [path.join(process.env.AIHUB, '.env')] : []),
    path.join(root, '.env'),
  ];
  const file = candidates.find(file => fs.existsSync(file));
  if (explicit && !file) throw new Error('AIHUB_ENV_FILE does not exist');
  if (file) process.loadEnvFile(file);
}
const host = process.env.AIHUB_SERVER_IP || '127.0.0.1';
const authority = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
module.exports.serviceURL = (port, suffix = '/v1') => `http://${authority}:${port}${suffix}`;
module.exports.hubURL = (suffix = '/v1') => {
  const base = process.env.AIHUB_PUBLIC_URL || `http://${authority}:${process.env.AIHUB_PORT || 8080}`;
  return new URL(suffix, base).href;
};
