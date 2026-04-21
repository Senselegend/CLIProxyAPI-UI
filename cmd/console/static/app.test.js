const test = require('node:test');
const assert = require('node:assert/strict');

const { deriveAccountStatus, normalizeActivityEntries } = require('./app.js');

test('deriveAccountStatus renders deactivated distinctly', () => {
  const status = deriveAccountStatus({ status: 'deactivated', status_message: 'revoked token' }, null);
  assert.deepEqual(status, { key: 'deactivated', label: 'deactivated' });
});

test('deriveAccountStatus keeps paused mapping for disabled accounts', () => {
  const status = deriveAccountStatus({ status: 'active', disabled: true }, null);
  assert.deepEqual(status, { key: 'paused', label: 'paused' });
});

test('deriveAccountStatus keeps rate-limited mapping for cooldown auths', () => {
  const status = deriveAccountStatus(
    {
      status: 'error',
      unavailable: true,
      next_retry_after: new Date(Date.now() + 60_000).toISOString(),
      status_message: 'quota exceeded',
    },
    null,
  );
  assert.deepEqual(status, { key: 'rate_limited', label: 'rate limited' });
});

test('normalizeActivityEntries hides request activity entries without accounts', () => {
  const rows = normalizeActivityEntries({
    entries: [
      { method: 'POST', path: '/v1/responses', account: '', status: 'pending' },
      { method: 'POST', path: '/v1/chat/completions', account: 'user@example.com', status: 'success' },
    ],
  });

  assert.equal(rows.length, 1);
  assert.equal(rows[0].account, 'user@example.com');
});

test('deriveAccountStatus keeps error mapping for generic backend errors', () => {
  const status = deriveAccountStatus({ status: 'error', status_message: 'context canceled' }, null);
  assert.deepEqual(status, { key: 'error', label: 'error' });
});

test('deriveAccountStatus keeps active mapping for healthy accounts', () => {
  const status = deriveAccountStatus({ status: 'active' }, null);
  assert.deepEqual(status, { key: 'active', label: 'active' });
});
