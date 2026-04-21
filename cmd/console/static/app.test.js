const test = require('node:test');
const assert = require('node:assert/strict');

const {
  deriveAccountStatus,
  normalizeActivityEntries,
  getVisibleLogs,
  shouldShowOlderLogsControl,
  renderLogs,
  filterLogs,
  setLogVisibleCount,
  setLogsForTest,
} = require('./app.js');

function escapeHtmlForStub(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function createElementStub(id = null) {
  let innerHTMLValue = '';
  const listeners = new Map();
  const element = {
    id,
    value: '',
    textContent: '',
    addEventListener(event, handler) {
      listeners.set(event, handler);
    },
    click() {
      const handler = listeners.get('click');
      if (handler) handler();
    },
    get innerHTML() {
      return innerHTMLValue;
    },
    set innerHTML(value) {
      innerHTMLValue = value;
    },
  };

  return element;
}

function createDocumentStub() {
  const elements = new Map();

  const register = (id, element = createElementStub(id)) => {
    elements.set(id, element);
    return element;
  };

  register('logs-body');
  const footer = register('logs-footer');
  register('logs-search').value = '';
  register('logs-account-filter').value = 'All Accounts';
  register('logs-status-filter').value = 'All Status';

  Object.defineProperty(footer, 'innerHTML', {
    get() {
      return this._innerHTML || '';
    },
    set(value) {
      this._innerHTML = value;
      if (String(value).includes('id="logs-show-older"')) {
        register('logs-show-older');
      } else {
        elements.delete('logs-show-older');
      }
    },
    configurable: true,
  });

  return {
    createElement() {
      let text = '';
      return {
        get textContent() {
          return text;
        },
        set textContent(value) {
          text = String(value);
        },
        get innerHTML() {
          return escapeHtmlForStub(text);
        },
      };
    },
    getElementById(id) {
      return elements.get(id) || null;
    },
  };
}

function makeLogs(count) {
  return Array.from({ length: count }, (_, index) => ({
    id: `log-${index + 1}`,
    account: `user-${index + 1}`,
    model: 'gpt-4o',
    transport: 'http',
    latency: '10ms',
    status: 'success',
    time: `${index + 1}s ago`,
    message: `message ${index + 1}`,
  }));
}

test('filterLogs applies filters before visible-window slicing', () => {
  const logs = [
    ...Array.from({ length: 80 }, (_, index) => ({
      id: `success-${index + 1}`,
      account: 'success-user',
      model: 'gpt-4o',
      transport: 'http',
      latency: '10ms',
      status: 'success',
      time: `${index + 1}s ago`,
      message: `success ${index + 1}`,
    })),
    ...Array.from({ length: 40 }, (_, index) => ({
      id: `error-${index + 1}`,
      account: 'error-user',
      model: 'gpt-4o',
      transport: 'http',
      latency: '10ms',
      status: 'error',
      time: `${index + 81}s ago`,
      message: `error ${index + 1}`,
    })),
  ];

  const filtered = logs.filter(log => log.status === 'error');
  const visible = getVisibleLogs(filtered, 50);

  assert.equal(visible.length, 40);
  assert.equal(visible[0].id, 'error-1');
  assert.equal(visible[39].id, 'error-40');
});

test('filterLogs filters full state before render windowing', () => {
  global.document = createDocumentStub();
  setLogVisibleCount(50);
  const logs = [
    ...Array.from({ length: 80 }, (_, index) => ({
      id: `success-${index + 1}`,
      account: 'success-user',
      model: 'gpt-4o',
      transport: 'http',
      latency: '10ms',
      status: 'success',
      time: `${index + 1}s ago`,
      message: `success ${index + 1}`,
    })),
    ...Array.from({ length: 40 }, (_, index) => ({
      id: `error-${index + 1}`,
      account: 'error-user',
      model: 'gpt-4o',
      transport: 'http',
      latency: '10ms',
      status: 'error',
      time: `${index + 81}s ago`,
      message: `error ${index + 1}`,
    })),
  ];

  try {
    setLogsForTest(logs);
    document.getElementById('logs-status-filter').value = 'Error';
    filterLogs();

    const tbody = document.getElementById('logs-body');

    assert.equal((tbody.innerHTML.match(/<tr>/g) || []).length, 40);
    assert.match(tbody.innerHTML, /error-1/);
    assert.match(tbody.innerHTML, /error-40/);
    assert.doesNotMatch(tbody.innerHTML, /success-1/);
  } finally {
    delete global.document;
  }
});

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

test('getVisibleLogs returns newest 50 rows by default', () => {
  const logs = Array.from({ length: 75 }, (_, index) => ({
    id: `log-${index + 1}`,
    account: `user-${index + 1}`,
    model: 'gpt-4o',
    transport: 'http',
    latency: '10ms',
    status: 'success',
    time: `${index + 1}s ago`,
    message: `message ${index + 1}`,
  }));

  const visible = getVisibleLogs(logs, 50);

  assert.equal(visible.length, 50);
  assert.equal(visible[0].id, 'log-26');
  assert.equal(visible[49].id, 'log-75');
});

test('getVisibleLogs returns all rows when count exceeds result size', () => {
  const logs = Array.from({ length: 12 }, (_, index) => ({
    id: `log-${index + 1}`,
    account: `user-${index + 1}`,
    model: 'gpt-4o',
    transport: 'http',
    latency: '10ms',
    status: 'success',
    time: `${index + 1}s ago`,
    message: `message ${index + 1}`,
  }));

  const visible = getVisibleLogs(logs, 50);

  assert.equal(visible.length, 12);
  assert.equal(visible[0].id, 'log-1');
  assert.equal(visible[11].id, 'log-12');
});

test('renderLogs shows newest 50 rows and reveals 50 more when requested', () => {
  const logs = makeLogs(120);

  assert.equal(getVisibleLogs(logs, 50).length, 50);
  assert.equal(getVisibleLogs(logs, 100)[0].id, 'log-21');
  assert.equal(getVisibleLogs(logs, 100)[99].id, 'log-120');
});

test('renderLogs renders 50 rows with a show older footer when more rows exist', () => {
  global.document = createDocumentStub();
  setLogVisibleCount(50);
  const logs = makeLogs(120);

  try {
    renderLogs(logs);

    const tbody = document.getElementById('logs-body');
    const footer = document.getElementById('logs-footer');

    assert.equal((tbody.innerHTML.match(/<tr>/g) || []).length, 50);
    assert.match(tbody.innerHTML, /log-71/);
    assert.match(tbody.innerHTML, /log-120/);
    assert.equal(document.getElementById('logs-show-older').id, 'logs-show-older');
    assert.match(footer.innerHTML, /Show 50 older/);
  } finally {
    delete global.document;
  }
});

test('clicking show older expands the rendered logs window to 100 rows', () => {
  global.document = createDocumentStub();
  setLogVisibleCount(50);
  const logs = makeLogs(120);

  try {
    setLogsForTest(logs);
    renderLogs(logs);
    document.getElementById('logs-show-older').click();

    const tbody = document.getElementById('logs-body');

    assert.equal((tbody.innerHTML.match(/<tr>/g) || []).length, 100);
    assert.match(tbody.innerHTML, /log-21/);
    assert.match(tbody.innerHTML, /log-120/);
  } finally {
    delete global.document;
  }
});

test('renderLogs footer is empty when there are no extra rows', () => {
  global.document = createDocumentStub();
  setLogVisibleCount(50);
  const logs = makeLogs(50);

  try {
    renderLogs(logs);

    const footer = document.getElementById('logs-footer');

    assert.equal(footer.innerHTML, '');
    assert.equal(document.getElementById('logs-show-older'), null);
  } finally {
    delete global.document;
  }
});

test('shouldShowOlderLogsControl is false when no extra rows exist', () => {
  assert.equal(shouldShowOlderLogsControl(Array.from({ length: 50 }, (_, index) => ({ id: `log-${index}` })), 50), false);
  assert.equal(shouldShowOlderLogsControl(Array.from({ length: 51 }, (_, index) => ({ id: `log-${index}` })), 50), true);
});

test('deriveAccountStatus keeps error mapping for generic backend errors', () => {
  const status = deriveAccountStatus({ status: 'error', status_message: 'context canceled' }, null);
  assert.deepEqual(status, { key: 'error', label: 'error' });
});

test('deriveAccountStatus keeps active mapping for healthy accounts', () => {
  const status = deriveAccountStatus({ status: 'active' }, null);
  assert.deepEqual(status, { key: 'active', label: 'active' });
});
