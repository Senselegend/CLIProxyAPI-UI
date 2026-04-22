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
  handleRefresh,
  computeQuotaSummaryFromQuotas,
  resolveAccountUsage,
  buildDashboardUsage,
  updateQuotaRings,
  getInitialSummaryWindow,
  getSummaryWindowValue,
  summaryWindowLabel,
  renderSummaryCards,
  setSummaryWindow,
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
    className: '',
    style: {},
    dataset: {},
    disabled: false,
    appendChild() {},
    remove() {},
    setAttribute() {},
    classList: {
      add() {},
      remove() {},
      toggle() {},
      contains() { return false; },
    },
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
  const summaryButtons = ['today', 'last_7_days', 'last_30_days'].map((value) => {
    const button = createElementStub(`summary-${value}`);
    button.dataset.summaryWindow = value;
    let active = value === 'last_7_days';
    button.classList = {
      add(name) {
        if (name === 'active') active = true;
      },
      remove(name) {
        if (name === 'active') active = false;
      },
      toggle(name, force) {
        if (name === 'active') active = Boolean(force);
      },
      contains(name) {
        return name === 'active' ? active : false;
      },
    };
    return button;
  });

  const register = (id, element = createElementStub(id)) => {
    elements.set(id, element);
    return element;
  };

  register('logs-body');
  const footer = register('logs-footer');
  register('logs-search').value = '';
  register('logs-account-filter').value = 'All Accounts';
  register('logs-status-filter').value = 'All Status';
  register('metric-requests');
  register('metric-requests-trend');
  register('metric-requests-window');
  register('metric-tokens');
  register('metric-tokens-trend');
  register('metric-tokens-window');
  register('metric-cost');
  register('metric-cost-trend');
  register('metric-cost-window');
  register('metric-errors');
  register('metric-errors-trend');
  register('metric-errors-window');
  register('active-accounts-count');
  register('refresh-btn');
  register('quota-5h-percent');
  register('quota-5h-remaining');
  register('quota-5h-ring', { ...createElementStub('quota-5h-ring'), style: {}, getAttribute(name) { return name === 'r' ? '45' : null; } });
  register('quota-7d-percent');
  register('quota-7d-remaining');
  register('quota-7d-ring', { ...createElementStub('quota-7d-ring'), style: {}, getAttribute(name) { return name === 'r' ? '45' : null; } });
  register('quota-5h-legend');
  register('quota-7d-legend');
  register('dashboard-accounts-grid');
  register('accounts-list');
  register('accounts-count');
  register('account-detail-panel');

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
    body: createElementStub('body'),
    documentElement: { setAttribute() {} },
    createElement() {
      let text = '';
      return {
        style: {},
        className: '',
        appendChild() {},
        remove() {},
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
    querySelector(selector) {
      if (selector === '[data-summary-window].active') {
        return summaryButtons.find((button) => button.classList.contains('active')) || null;
      }
      return null;
    },
    querySelectorAll(selector) {
      if (selector === '[data-summary-window]') {
        return summaryButtons;
      }
      return [];
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

test('computeQuotaSummaryFromQuotas averages lower-usage accounts into used percent totals', () => {
  const summary = computeQuotaSummaryFromQuotas([
    {
      primary_window: { used_percent: 100 },
      secondary_window: { used_percent: 100 },
    },
    {
      primary_window: { used_percent: 34 },
      secondary_window: { used_percent: 34 },
    },
  ]);

  assert.deepEqual(summary, {
    primary_used_percent: 67,
    secondary_used_percent: 67,
  });
});

test('computeQuotaSummaryFromQuotas ignores missing windows when averaging used percent totals', () => {
  const summary = computeQuotaSummaryFromQuotas([
    {
      primary_window: { used_percent: 100 },
    },
    {
      secondary_window: { used_percent: 34 },
    },
    {
      primary_window: { used_percent: 34 },
      secondary_window: { used_percent: 100 },
    },
  ]);

  assert.deepEqual(summary, {
    primary_used_percent: 67,
    secondary_used_percent: 67,
  });
});

test('computeQuotaSummaryFromQuotas returns zero used percent totals when no usable data exists', () => {
  const summary = computeQuotaSummaryFromQuotas([
    {},
    null,
    { primary_window: null, secondary_window: {} },
  ]);

  assert.deepEqual(summary, {
    primary_used_percent: 0,
    secondary_used_percent: 0,
  });
});

test('resolveAccountUsage does not fuzzy-match email prefixes', () => {
  const state = {
    usage: {
      apis: {
        'jacquelinebevins2@outlook.com': { total_requests: 11, total_tokens: 1100 },
        'jacquelinebevins@outlook.com': { total_requests: 7, total_tokens: 700 },
      },
    },
  };
  const acc = {
    id: 'jacquelinebevins25.json',
    email: 'jacquelinebevins25@outlook.com',
  };

  assert.equal(resolveAccountUsage(state, acc), null);
});

test('resolveAccountUsage returns usage for an exact email match', () => {
  const expectedUsage = { total_requests: 13, total_tokens: 1300 };
  const state = {
    usage: {
      apis: {
        'jacquelinebevins25@outlook.com': expectedUsage,
      },
    },
  };
  const acc = {
    id: 'jacquelinebevins25.json',
    email: 'jacquelinebevins25@outlook.com',
  };

  assert.equal(resolveAccountUsage(state, acc), expectedUsage);
});

test('resolveAccountUsage returns usage for an exact account id match', () => {
  const expectedUsage = { total_requests: 17, total_tokens: 1700 };
  const state = {
    usage: {
      apis: {
        'jacquelinebevins25.json': expectedUsage,
      },
    },
  };
  const acc = {
    id: 'jacquelinebevins25.json',
    email: 'jacquelinebevins25@outlook.com',
  };

  assert.equal(resolveAccountUsage(state, acc), expectedUsage);
});

test('buildDashboardUsage preserves rolling token window fields per account', () => {
  const usage = buildDashboardUsage(
    { usage: { total_requests: 0, total_tokens: 0, failure_count: 0, apis: {} } },
    {
      by_account: {
        'alpha@example.com': {
          total_requests: 10,
          total_tokens: 1000,
          failed_count: 1,
          models: { 'gpt-5.4': 10 },
          last_5_hours: { total_tokens: 123 },
          last_7_days: { total_tokens: 456 },
        },
      },
    },
  );

  assert.equal(usage.apis['alpha@example.com'].last_5_hours.total_tokens, 123);
  assert.equal(usage.apis['alpha@example.com'].last_7_days.total_tokens, 456);
});

test('buildDashboardUsage preserves summary payload for top cards', () => {
  const usage = buildDashboardUsage(
    {
      usage: {
        total_requests: 99,
        total_tokens: 12345,
        failure_count: 4,
        summary: {
          lifetime: { requests: 99, tokens: 12345, cost_usd: 1.23, errors: 4 },
          today: { requests: 5, tokens: 500, cost_usd: 0.05, errors: 1 },
          last_7_days: { requests: 20, tokens: 2000, cost_usd: 0.2, errors: 2 },
          last_30_days: { requests: 70, tokens: 7000, cost_usd: 0.7, errors: 3 },
        },
      },
    },
    { by_account: {} },
  );

  assert.equal(usage.summary.last_7_days.tokens, 2000);
  assert.equal(usage.summary.last_30_days.cost_usd, 0.7);
});

test('getInitialSummaryWindow defaults to last_7_days and restores valid saved value', () => {
  assert.equal(getInitialSummaryWindow({ getItem: () => null }), 'last_7_days');
  assert.equal(getInitialSummaryWindow({ getItem: () => 'last_30_days' }), 'last_30_days');
  assert.equal(getInitialSummaryWindow({ getItem: () => 'weird' }), 'last_7_days');
});

test('summaryWindowLabel returns compact labels', () => {
  assert.equal(summaryWindowLabel('today'), 'Today');
  assert.equal(summaryWindowLabel('last_7_days'), '7 days');
  assert.equal(summaryWindowLabel('last_30_days'), '30 days');
});

test('getSummaryWindowValue returns zeroed fallback when summary window is missing', () => {
  assert.deepEqual(
    getSummaryWindowValue({ lifetime: { requests: 10 } }, 'last_30_days'),
    { requests: 0, tokens: 0, cost_usd: 0, errors: 0 },
  );
});

test('renderSummaryCards keeps lifetime values and swaps comparison window copy', () => {
  global.document = createDocumentStub();
  try {
    renderSummaryCards(
      {
        lifetime: { requests: 54321, tokens: 2300000, cost_usd: 23.45, errors: 8 },
        today: { requests: 7, tokens: 700, cost_usd: 0.07, errors: 1 },
        last_7_days: { requests: 42, tokens: 4200, cost_usd: 0.42, errors: 3 },
        last_30_days: { requests: 99, tokens: 9900, cost_usd: 0.99, errors: 5 },
      },
      'last_30_days',
    );

    assert.equal(document.getElementById('metric-requests').textContent, '54.3K');
    assert.equal(document.getElementById('metric-requests-window').textContent, '99 in 30 days');
    assert.equal(document.getElementById('metric-tokens-window').textContent, '9.9K in 30 days');
    assert.equal(document.getElementById('metric-cost-window').textContent, '$0.99 in 30 days');
    assert.equal(document.getElementById('metric-errors-window').textContent, '5 in 30 days');
    assert.equal(document.getElementById('metric-errors-trend').textContent, '8 total');
  } finally {
    delete global.document;
  }
});

test('setSummaryWindow updates active button state and persists selection', () => {
  global.document = createDocumentStub();
  const writes = [];
  const fakeStorage = {
    setItem(key, value) {
      writes.push([key, value]);
    },
  };

  try {
    setSummaryWindow('today', fakeStorage);

    assert.equal(document.querySelector('[data-summary-window].active').dataset.summaryWindow, 'today');
    assert.deepEqual(writes, [['dashboard-summary-window', 'today']]);
  } finally {
    delete global.document;
  }
});

test('setSummaryWindow falls back to default when called with invalid value', () => {
  global.document = createDocumentStub();
  const writes = [];
  const fakeStorage = {
    setItem(key, value) {
      writes.push([key, value]);
    },
  };

  try {
    const selected = setSummaryWindow('bogus', fakeStorage);

    assert.equal(selected, 'last_7_days');
    assert.equal(document.querySelector('[data-summary-window].active').dataset.summaryWindow, 'last_7_days');
    assert.deepEqual(writes, [['dashboard-summary-window', 'last_7_days']]);
  } finally {
    delete global.document;
  }
});

test('updateQuotaRings uses rolling token windows for 5h and 7d displays', () => {
  global.document = createDocumentStub();
  try {
    updateQuotaRings(
      {
        'alpha@example.com': {
          total_requests: 99,
          total_tokens: 9999,
          last_5_hours: { total_tokens: 120 },
          last_7_days: { total_tokens: 1200 },
        },
        'beta@example.com': {
          total_requests: 88,
          total_tokens: 8888,
          last_5_hours: { total_tokens: 30 },
          last_7_days: { total_tokens: 700 },
        },
      },
      { primary_used_percent: 25, secondary_used_percent: 50 },
    );

    assert.equal(document.getElementById('quota-5h-remaining').textContent, '150 tokens');
    assert.equal(document.getElementById('quota-7d-remaining').textContent, '1.9K tokens');
    assert.match(document.getElementById('quota-5h-legend').innerHTML, /alpha@example.com/);
    assert.match(document.getElementById('quota-5h-legend').innerHTML, /120/);
    assert.match(document.getElementById('quota-7d-legend').innerHTML, /1.2K/);
  } finally {
    delete global.document;
  }
});

test('handleRefresh reloads data, triggers account recheck, and reloads accounts again', async () => {
  const calls = [];
  const originalSetTimeout = global.setTimeout;
  global.document = createDocumentStub();
  global.setTimeout = (fn) => {
    fn();
    return 0;
  };
  global.fetch = async (url, options = {}) => {
    calls.push({ url: String(url), method: options.method || 'GET' });
    if (String(url).includes('/usage')) return { ok: true, json: async () => ({ usage: {} }) };
    if (String(url).includes('/account-usage')) return { ok: true, json: async () => ({ by_account: {} }) };
    if (String(url).includes('/auth-files') && !String(url).includes('/recheck')) return { ok: true, json: async () => ({ files: [] }) };
    if (String(url).includes('/quotas')) return { ok: true, json: async () => ({ quotas: [] }) };
    if (String(url).includes('/logs')) return { ok: true, json: async () => ({ logs: [] }) };
    if (String(url).includes('/request-activity')) return { ok: true, json: async () => ({ entries: [] }) };
    if (String(url).includes('/auth-files/recheck')) return { ok: true, json: async () => ({ triggered: 2 }) };
    throw new Error(`unexpected url ${url}`);
  };

  try {
    await handleRefresh();
    const authFileCalls = calls.filter(call => call.url.includes('/auth-files') && !call.url.includes('/recheck'));
    const recheckIndex = calls.findIndex(call => call.url.includes('/auth-files/recheck') && call.method === 'POST');

    assert.equal(authFileCalls.length >= 2, true);
    assert.equal(recheckIndex > -1, true);
    assert.equal(calls.findIndex(call => call.url.includes('/auth-files') && !call.url.includes('/recheck')) < recheckIndex, true);
    assert.equal(calls.slice(recheckIndex + 1).some(call => call.url.includes('/auth-files') && !call.url.includes('/recheck')), true);
  } finally {
    delete global.fetch;
    delete global.document;
    global.setTimeout = originalSetTimeout;
  }
});

test('handleRefresh still resolves when account recheck fails', async () => {
  const originalSetTimeout = global.setTimeout;
  global.document = createDocumentStub();
  global.setTimeout = (fn) => {
    fn();
    return 0;
  };
  global.fetch = async (url, options = {}) => {
    if (String(url).includes('/usage')) return { ok: true, json: async () => ({ usage: {} }) };
    if (String(url).includes('/account-usage')) return { ok: true, json: async () => ({ by_account: {} }) };
    if (String(url).includes('/auth-files') && !String(url).includes('/recheck')) return { ok: true, json: async () => ({ files: [] }) };
    if (String(url).includes('/quotas')) return { ok: true, json: async () => ({ quotas: [] }) };
    if (String(url).includes('/logs')) return { ok: true, json: async () => ({ logs: [] }) };
    if (String(url).includes('/request-activity')) return { ok: true, json: async () => ({ entries: [] }) };
    if (String(url).includes('/auth-files/recheck')) return { ok: false, status: 500, json: async () => ({ error: 'boom' }) };
    throw new Error(`unexpected url ${url}`);
  };

  try {
    await assert.doesNotReject(handleRefresh());
  } finally {
    delete global.fetch;
    delete global.document;
    global.setTimeout = originalSetTimeout;
  }
});

test('handleRefresh ignores concurrent clicks while refresh is in flight', async () => {
  const originalSetTimeout = global.setTimeout;
  const calls = [];
  let releaseUsage;
  global.document = createDocumentStub();
  global.setTimeout = (fn) => {
    fn();
    return 0;
  };
  global.fetch = async (url, options = {}) => {
    const currentUrl = String(url);
    calls.push({ url: currentUrl, method: options.method || 'GET' });
    if (currentUrl.includes('/usage')) {
      await new Promise((resolve) => {
        releaseUsage = resolve;
      });
      return { ok: true, json: async () => ({ usage: {} }) };
    }
    if (currentUrl.includes('/account-usage')) return { ok: true, json: async () => ({ by_account: {} }) };
    if (currentUrl.includes('/auth-files') && !currentUrl.includes('/recheck')) return { ok: true, json: async () => ({ files: [] }) };
    if (currentUrl.includes('/quotas')) return { ok: true, json: async () => ({ quotas: [] }) };
    if (currentUrl.includes('/logs')) return { ok: true, json: async () => ({ logs: [] }) };
    if (currentUrl.includes('/request-activity')) return { ok: true, json: async () => ({ entries: [] }) };
    if (currentUrl.includes('/auth-files/recheck')) return { ok: true, json: async () => ({ triggered: 1 }) };
    throw new Error(`unexpected url ${url}`);
  };

  try {
    const first = handleRefresh();
    const second = handleRefresh();
    releaseUsage();
    await Promise.all([first, second]);

    const refreshBtn = document.getElementById('refresh-btn');
    assert.equal(calls.filter(call => call.url.includes('/usage')).length, 1);
    assert.equal(calls.filter(call => call.url.includes('/auth-files/recheck')).length, 1);
    assert.equal(refreshBtn.disabled, false);
  } finally {
    delete global.fetch;
    delete global.document;
    global.setTimeout = originalSetTimeout;
  }
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
