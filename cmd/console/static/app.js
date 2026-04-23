// CLI Proxy Dashboard - Neo-Terminal Theme
(function() {
  'use strict';

  const API_BASE = '/v0/management';
  const LOGS_CACHE_KEY = 'dashboard-request-activity-cache';
  const LOGS_CACHE_LIMIT = 200;
  const LOGS_PAGE_SIZE = 50;
  const SUMMARY_WINDOW_KEY = 'dashboard-summary-window';
  const RECOVERY_POLL_ATTEMPTS = 4;
  const RECOVERY_POLL_DELAY_MS = 1500;
  const HARD_REFRESH_DOUBLE_CLICK_WINDOW_MS = 1500;
  const DEFAULT_SUMMARY_WINDOW = 'last_7_days';
  const SUMMARY_WINDOWS = new Set(['today', 'last_7_days', 'last_30_days']);

  const storage = typeof localStorage !== 'undefined' && localStorage && typeof localStorage.getItem === 'function'
    ? localStorage
    : {
        getItem() { return null; },
        setItem() {},
        removeItem() {},
      };

  // State
  let state = {
    theme: storage.getItem('dashboard-theme') || 'dark',
    apiKey: storage.getItem('dashboard-api-key') || '',
    activeTab: 'dashboard',
    summaryWindow: getInitialSummaryWindow(storage),
    accounts: [],
    usage: null,
    logs: [],
    logVisibleCount: 50,
    settings: {},
    quotaSummary: null,
    quotaStartupSync: null
  };
  let detailCountdownTimer = null;
  let startupSyncPollTimer = null;
  let refreshInFlight = false;
  let lastRefreshCompletedAt = 0;
  let lastLogsFilterSignature = '';

  // Icons
  const icons = {
    refresh: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>',
    plus: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>',
    clock: '<svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>'
  };

  // Account colors for quota legend
  const accountColors = [
    '#3b82f6', '#8b5cf6', '#22c55e', '#f59e0b',
    '#ec4899', '#06b6d4', '#6366f1', '#f97316'
  ];

  const CONFIG_TOGGLE_ENDPOINTS = {
    '/debug': { path: ['debug'], type: 'bool' },
    '/logging-to-file': { path: ['logging-to-file'], type: 'bool' },
    '/usage-statistics-enabled': { path: ['usage-statistics-enabled'], type: 'bool' },
    '/force-model-prefix': { path: ['force-model-prefix'], type: 'bool' },
    '/ws-auth': { path: ['ws-auth'], type: 'bool' },
    '/routing/session-affinity': { path: ['routing', 'session-affinity'], type: 'bool' },
    '/quota-exceeded/switch-project': { path: ['quota-exceeded', 'switch-project'], type: 'bool' },
    '/quota-exceeded/switch-preview-model': { path: ['quota-exceeded', 'switch-preview-model'], type: 'bool' },
    '/commercial-mode': { path: ['commercial-mode'], type: 'bool' },
    '/tls-enabled': { path: ['tls', 'enable'], type: 'bool' },
    '/remote-management/allow-remote': { path: ['remote-management', 'allow-remote'], type: 'bool' },
    '/remote-management/control-panel-enabled': { path: ['remote-management', 'disable-control-panel'], type: 'bool', invert: true },
    '/passthrough-headers': { path: ['passthrough-headers'], type: 'bool' }
  };

  const CONFIG_INPUTS = {
    'input-proxy-url': { path: ['proxy-url'], type: 'string' },
    'input-retry': { path: ['request-retry'], type: 'int' },
    'input-max-retry-interval': { path: ['max-retry-interval'], type: 'int' },
    'input-logs-max-size': { path: ['logs-max-total-size-mb'], type: 'int' },
    'input-error-logs-max': { path: ['error-logs-max-files'], type: 'int' },
    'input-host': { path: ['host'], type: 'string', restartRequired: true },
    'input-port': { path: ['port'], type: 'int', restartRequired: true },
    'input-secret': { path: ['remote-management', 'secret-key'], type: 'string' },
    'select-routing': { path: ['routing', 'strategy'], type: 'string' }
  };

  // Initialize
  function init() {
    applyTheme(state.theme);
    setupEventListeners();
    state.summaryWindow = setSummaryWindow(state.summaryWindow, storage);
    loadApiKey();
    showLoadingState();
    ensureDetailCountdownTimer();

    // Check for OAuth callback
    const params = new URLSearchParams(window.location.search);
    console.log('Init: checking OAuth callback, params:', params.toString());
    if (params.get('oauth_callback') === 'success') {
      const code = params.get('code');
      const state = params.get('state');
      console.log('OAuth callback detected:', { code: !!code, state: state });
      // Clean URL immediately
      window.history.replaceState({}, '', window.location.pathname);
      // Show modal if not open
      const modal = document.getElementById('add-account-modal');
      if (!modal.classList.contains('active')) {
        modal.classList.add('active');
      }
      handleOAuthCallback(code, state);
    }

    loadData().then(() => {
      // Restore saved state
      const savedTab = storage.getItem('dashboard-tab');
      if (savedTab && savedTab !== state.activeTab) {
        switchTab(savedTab);
      }
      const savedAccount = storage.getItem('dashboard-selected-account');
      if (savedAccount && state.accounts.find(a => a.id === savedAccount)) {
        selectAccount(savedAccount);
      }
    });
  }

  async function handleOAuthCallback(code, state) {
    console.log('handleOAuthCallback called:', { code: !!code, state: state });
    // Set state for polling
    oauthState = state || 'oauth_pending';

    // Update modal status
    const statusEl = document.getElementById('oauth-status');
    if (statusEl) {
      statusEl.style.display = 'block';
      statusEl.className = 'oauth-status pending';
      statusEl.textContent = 'Processing authorization...';
    }

    showToast('OAuth authorized! Processing...', 'info');

    // Poll for status with retries
    await pollForAuthComplete();
  }

  async function pollForAuthComplete() {
    const maxAttempts = 30; // 30 * 2s = 60 seconds
    let attempts = 0;

    const poll = async () => {
      if (attempts >= maxAttempts) {
        showToast('OAuth timeout', 'error');
        closeModal();
        return;
      }
      attempts++;

      console.log('Polling auth status, attempt:', attempts, 'state:', oauthState);
      const data = await apiFetch('/get-auth-status?state=' + encodeURIComponent(oauthState));
      console.log('Auth status response:', data);

      if (data && data.status === 'ok') {
        showToast('Account added successfully!', 'success');
        closeModal();
        loadAccounts();
        return;
      } else if (data && data.status === 'error') {
        showToast('OAuth error: ' + (data.error || 'Unknown'), 'error');
        closeModal();
        return;
      }

      // Continue polling
      setTimeout(poll, 2000);
    };

    poll();
  }

  function showLoadingState() {
    // Show skeleton loaders
    document.querySelectorAll('.metric-value').forEach(el => {
      el.innerHTML = '<span class="skeleton"></span>';
    });
  }

  function hideLoadingState() {
    document.querySelectorAll('.skeleton').forEach(el => el.remove());
  }

  // Load saved API key and check config
  async function loadApiKey() {
    // Check if key is required
    try {
      const res = await fetch('/api/status');
      const data = await res.json();
      if (!data.keyRequired) {
        // Key is auto-loaded from config, no input needed
        // But we need to signal that API has a key
        // The console server passes the key to API, so we just need to mark it as available
        state.apiKey = 'auto';
        storage.setItem('dashboard-api-key', 'auto');

        const input = document.getElementById('api-key-input');
        if (input) {
          input.placeholder = 'Auto-configured';
          input.disabled = true;
          input.style.opacity = '0.5';
        }
      }
    } catch (e) {}

    // Load saved key from localStorage
    const savedKey = storage.getItem('dashboard-api-key');
    if (savedKey && savedKey !== 'auto') {
      state.apiKey = savedKey;
    }

    const input = document.getElementById('api-key-input');
    if (input) {
      if (state.apiKey && state.apiKey !== 'auto') {
        input.value = state.apiKey;
      }
      input.addEventListener('change', (e) => {
        state.apiKey = e.target.value.trim();
        storage.setItem('dashboard-api-key', state.apiKey);
        showToast('API key saved', 'success');
        loadData();
      });
    }
  }

  // Toast notification
  function showToast(message, type = 'info') {
    const existing = document.querySelector('.toast-container');
    if (existing) existing.remove();

    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    toast.style.cssText = `
      position: fixed;
      bottom: 24px;
      right: 24px;
      padding: 12px 20px;
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 8px;
      font-family: var(--mono);
      font-size: 0.8rem;
      color: var(--text);
      z-index: 9999;
      animation: slideIn 200ms ease-out;
      box-shadow: var(--shadow-md);
    `;
    if (type === 'error') toast.style.borderColor = 'var(--danger)';
    if (type === 'success') toast.style.borderColor = 'var(--signal)';

    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
  }

  // Theme management
  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    storage.setItem('dashboard-theme', theme);

    document.querySelectorAll('.theme-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.theme === theme);
    });
  }

  // Event listeners
  function setupEventListeners() {
    // Tab navigation
    document.querySelectorAll('.nav-btn[data-tab]').forEach(btn => {
      btn.addEventListener('click', () => switchTab(btn.dataset.tab));
    });

    document.querySelectorAll('[data-summary-window]').forEach(btn => {
      btn.addEventListener('click', () => {
        if (!SUMMARY_WINDOWS.has(btn.dataset.summaryWindow)) {
          return;
        }
        state.summaryWindow = setSummaryWindow(btn.dataset.summaryWindow, storage);
        renderSummaryCards(state.usage && state.usage.summary, state.summaryWindow);
      });
    });

    // Refresh button
    document.getElementById('refresh-btn').addEventListener('click', handleRefresh);

    // Theme buttons
    document.querySelectorAll('.theme-btn').forEach(btn => {
      btn.addEventListener('click', () => applyTheme(btn.dataset.theme));
    });

    // Logs search
    const searchInput = document.getElementById('logs-search');
    let searchTimeout;
    searchInput.addEventListener('input', () => {
      clearTimeout(searchTimeout);
      searchTimeout = setTimeout(() => {
        state.logVisibleCount = LOGS_PAGE_SIZE;
        filterLogs();
      }, 300);
    });

    // Logs filters
    const logsAccountFilter = document.getElementById('logs-account-filter');
    const logsAccountTrigger = document.getElementById('logs-account-trigger');
    const logsAccountMenu = document.getElementById('logs-account-menu');
    if (logsAccountTrigger && logsAccountMenu && logsAccountFilter) {
      logsAccountTrigger.addEventListener('click', () => {
        const expanded = logsAccountTrigger.getAttribute('aria-expanded') === 'true';
        if (expanded) {
          dismissLogsAccountMenu();
        } else {
          openLogsAccountMenu();
          dismissLogsStatusMenu();
        }
      });
      logsAccountMenu.addEventListener('change', (event) => {
        const target = event.target;
        if (!target || target.type !== 'checkbox') return;
        const selected = Array.from(logsAccountMenu.querySelectorAll('input[type="checkbox"]:checked')).map(input => input.value);
        setLogsSelectedAccounts(selected);
        updateLogsAccountSummary(selected);
        state.logVisibleCount = LOGS_PAGE_SIZE;
        filterLogs();
      });
    }

    const logsStatusFilter = document.getElementById('logs-status-filter');
    const logsStatusTrigger = document.getElementById('logs-status-trigger');
    const logsStatusMenu = document.getElementById('logs-status-menu');
    if (logsStatusTrigger && logsStatusMenu && logsStatusFilter) {
      logsStatusTrigger.addEventListener('click', () => {
        const expanded = logsStatusTrigger.getAttribute('aria-expanded') === 'true';
        if (expanded) {
          dismissLogsStatusMenu();
        } else {
          openLogsStatusMenu();
          dismissLogsAccountMenu();
        }
      });
      logsStatusMenu.addEventListener('click', (event) => {
        const option = event.target && event.target.closest('[data-status-value]');
        if (!option) return;
        setLogsStatusFilterValue(option.dataset.statusValue || 'All Status');
        state.logVisibleCount = LOGS_PAGE_SIZE;
        filterLogs();
        dismissLogsStatusMenu();
      });
      document.addEventListener('click', (event) => {
        if (!logsAccountMenu.hidden && !logsAccountTrigger.contains(event.target) && !logsAccountMenu.contains(event.target)) {
          dismissLogsAccountMenu();
        }
        if (!logsStatusMenu.hidden && !logsStatusTrigger.contains(event.target) && !logsStatusMenu.contains(event.target)) {
          dismissLogsStatusMenu();
        }
      });
      document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') {
          dismissLogsAccountMenu();
          dismissLogsStatusMenu();
        }
      });
    }

    // Add account modal
    document.getElementById('add-account-btn').addEventListener('click', () => {
      document.getElementById('add-account-modal').classList.add('active');
    });

    document.getElementById('modal-close').addEventListener('click', closeModal);
    document.getElementById('add-account-modal').addEventListener('click', (e) => {
      if (e.target === e.currentTarget) closeModal();
    });

    // Provider selection
    document.querySelectorAll('.provider-btn[data-provider]').forEach(btn => {
      btn.addEventListener('click', () => selectProvider(btn.dataset.provider));
    });

    document.getElementById('modal-next').addEventListener('click', handleModalNext);
    document.getElementById('modal-back').addEventListener('click', handleModalBack);
    document.getElementById('auth-oauth-btn').addEventListener('click', () => {
      document.querySelectorAll('.auth-method-btn').forEach(b => b.classList.remove('selected'));
      document.getElementById('auth-oauth-btn').classList.add('selected');
      const choice = document.getElementById('auth-method-choice');
      if (choice) choice.style.display = 'none';
      const flow = document.getElementById('oauth-flow');
      if (flow) flow.style.display = 'block';
      startOAuthFlow();
    });
    document.getElementById('auth-apikey-btn').addEventListener('click', () => {
      document.querySelectorAll('.auth-method-btn').forEach(b => b.classList.remove('selected'));
      document.getElementById('auth-apikey-btn').classList.add('selected');
      const choice = document.getElementById('auth-method-choice');
      if (choice) choice.style.display = 'none';
      const flow = document.getElementById('apikey-flow');
      if (flow) flow.style.display = 'block';
    });

    // Account search
    const accountsSearchInput = document.getElementById('accounts-search');
    let accountsSearchTimeout;
    accountsSearchInput.addEventListener('input', () => {
      clearTimeout(accountsSearchTimeout);
      accountsSearchTimeout = setTimeout(() => filterAccountsList(accountsSearchInput.value), 200);
    });

    // Settings navigation
    document.querySelectorAll('.settings-nav-item').forEach(btn => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.settings-nav-item').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        const section = btn.dataset.section;
        document.querySelectorAll('.settings-section').forEach(s => {
          s.style.display = s.id === 'section-' + section ? 'block' : 'none';
        });
      });
    });

    // Settings inputs - save on blur
    document.querySelectorAll('.setting-input, .setting-select').forEach(input => {
      input.addEventListener('blur', () => saveSetting(input));
    });

    // Settings toggles
    document.querySelectorAll('.settings-section .toggle').forEach(toggle => {
      toggle.addEventListener('click', () => {
        toggle.classList.toggle('active');
        const endpoint = toggle.dataset.endpoint;
        if (endpoint) {
          saveToggleSetting(endpoint, toggle.classList.contains('active'));
        }
      });
    });
  }

  function filterAccountsList(query) {
    const list = document.getElementById('accounts-list');
    if (!list) return;
    const q = query.toLowerCase();
    list.querySelectorAll('.account-item').forEach(item => {
      const email = item.querySelector('.account-item-email').textContent.toLowerCase();
      const provider = item.querySelector('.account-item-tier').textContent.toLowerCase();
      item.style.display = (email.includes(q) || provider.includes(q)) ? '' : 'none';
    });
  }

  // Tab switching
  function switchTab(tab) {
    state.activeTab = tab;
    storage.setItem('dashboard-tab', tab);

    document.querySelectorAll('.nav-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.tab === tab);
    });

    document.querySelectorAll('.tab-content').forEach(content => {
      content.classList.toggle('active', content.id === `${tab}-tab`);
    });

    if (tab === 'accounts') {
      renderAccountsSidebar();
    }

    if (tab === 'apis') {
      loadModels();
    }

    if (tab === 'settings') {
      loadAllSettings();
    }
  }

  // API calls
  async function apiFetch(endpoint, options = {}) {
    const headers = { 'Content-Type': 'application/json' };
    // Use saved key, state key, or localStorage key
    // But don't send 'auto' - the console server handles auth automatically
    let key = state.apiKey || storage.getItem('dashboard-api-key');
    if (key && key !== 'auto') {
      headers['X-Management-Key'] = key;
    }
    try {
      const res = await fetch(`${API_BASE}${endpoint}`, {
        ...options,
        headers: { ...headers, ...options.headers }
      });
      if (!res.ok) {
        if (res.status === 401 || res.status === 403) {
          showToast('API key required. Check Settings.', 'error');
        }
        throw new Error(`API error: ${res.status}`);
      }
      return await res.json();
    } catch (err) {
      console.warn('API fetch failed:', err);
      return null;
    }
  }

  async function fetchConfigYAMLText() {
    const headers = {};
    const key = state.apiKey || storage.getItem('dashboard-api-key');
    if (key && key !== 'auto') {
      headers['X-Management-Key'] = key;
    }
    try {
      const res = await fetch(`${API_BASE}/config.yaml`, { headers });
      if (!res.ok) throw new Error(`config fetch failed: ${res.status}`);
      return await res.text();
    } catch (err) {
      console.warn('Config YAML fetch failed:', err);
      return null;
    }
  }

  async function saveConfigYAMLText(text) {
    const headers = { 'Content-Type': 'application/yaml' };
    const key = state.apiKey || storage.getItem('dashboard-api-key');
    if (key && key !== 'auto') {
      headers['X-Management-Key'] = key;
    }
    const res = await fetch(`${API_BASE}/config.yaml`, {
      method: 'PUT',
      headers,
      body: text
    });
    return res.ok;
  }

  // Load all dashboard data
  async function loadData() {
    showLoadingState();
    restoreCachedLogs();
    await Promise.all([
      loadUsage(),
      loadAccounts(),
      loadLogs()
    ]);
    hideLoadingState();
  }

  async function triggerAccountRecheck() {
    return await apiFetch('/auth-files/recheck', { method: 'POST' });
  }

  async function triggerQuotaRecovery() {
    return await apiFetch('/quotas/recover', { method: 'POST' });
  }

  function delay(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  function hasRecoveringAccounts(accounts) {
    return Array.isArray(accounts) && accounts.some((account) => account && account.status === 'syncing');
  }

  async function runRecoveryPolling(recheck) {
    const triggered = Number(recheck && recheck.triggered) || 0;
    const inFlightCount = Number(recheck && recheck.recovery && recheck.recovery.in_flight_count) || 0;
    if (triggered <= 0 && inFlightCount <= 0) {
      return;
    }

    for (let attempt = 0; attempt < RECOVERY_POLL_ATTEMPTS; attempt += 1) {
      await loadAccounts();
      if (!hasRecoveringAccounts(state.accounts)) {
        return;
      }
      if (attempt < RECOVERY_POLL_ATTEMPTS - 1) {
        await delay(RECOVERY_POLL_DELAY_MS);
      }
    }
  }

  async function handleRefresh() {
    if (refreshInFlight) {
      return;
    }

    const refreshButton = document.getElementById('refresh-btn');
    const hardRefresh = (Date.now() - lastRefreshCompletedAt) <= HARD_REFRESH_DOUBLE_CLICK_WINDOW_MS;
    refreshInFlight = true;
    if (refreshButton) {
      refreshButton.disabled = true;
      if (refreshButton.classList && typeof refreshButton.classList.add === 'function') {
        refreshButton.classList.add('is-loading');
      }
    }
    try {
      await loadData();
      let recovery = null;
      if (hardRefresh) {
        showToast('Hard refresh in progress', 'info');
        recovery = await triggerQuotaRecovery().catch(() => null);
      } else {
        recovery = await triggerAccountRecheck().catch(() => null);
        if (recovery && recovery.triggered > 0) {
          showToast(`Rechecking ${recovery.triggered} accounts`, 'info');
        }
      }
      await runRecoveryPolling(recovery);
      if (hardRefresh) {
        await loadData();
      } else {
        await loadAccounts();
      }
    } finally {
      refreshInFlight = false;
      lastRefreshCompletedAt = Date.now();
      if (refreshButton) {
        refreshButton.disabled = false;
        if (refreshButton.classList && typeof refreshButton.classList.remove === 'function') {
          refreshButton.classList.remove('is-loading');
        }
      }
    }
  }

  function hasMeaningfulSummary(summary) {
    if (!summary) return false;
    const normalized = normalizeSummaryPayload(summary);
    return Object.values(normalized).some(windowValue => (
      Number(windowValue.requests) > 0
      || Number(windowValue.tokens) > 0
      || Number(windowValue.cost_usd) > 0
      || Number(windowValue.errors) > 0
    ));
  }

  function buildDashboardUsage(data, accountData, previousUsage) {
    const usage = { ...(data && data.usage ? data.usage : {}) };
    const summarySource = accountData && accountData.summary ? accountData.summary : usage.summary;
    const previousSummary = previousUsage && previousUsage.summary ? previousUsage.summary : null;
    usage.summary = hasMeaningfulSummary(summarySource)
      ? normalizeSummaryPayload(summarySource)
      : normalizeSummaryPayload(previousSummary);

    if (accountData && accountData.by_account && Object.keys(accountData.by_account).length > 0) {
      const accountAPIs = {};
      let accountRequests = 0;
      let accountTokens = 0;
      let accountFailures = 0;

      for (const [email, acc] of Object.entries(accountData.by_account)) {
        const totalRequests = Number(acc.total_requests) || 0;
        const totalTokens = Number(acc.total_tokens) || 0;
        const failureCount = Number(acc.failed_count) || 0;

        accountRequests += totalRequests;
        accountTokens += totalTokens;
        accountFailures += failureCount;

        accountAPIs[email] = {
          total_requests: totalRequests,
          total_tokens: totalTokens,
          failure_count: failureCount,
          last_5_hours: {
            total_tokens: Number(acc.last_5_hours && acc.last_5_hours.total_tokens) || 0,
          },
          last_7_days: {
            total_tokens: Number(acc.last_7_days && acc.last_7_days.total_tokens) || 0,
          },
          models: Object.fromEntries(
            Object.entries(acc.models || {}).map(([m, cnt]) => [m, { total_requests: Number(cnt) || 0, total_tokens: 0 }])
          )
        };
      }

      usage.apis = accountAPIs;
      usage.total_requests = Number(usage.total_requests) || accountRequests;
      usage.total_tokens = Number(usage.total_tokens) || accountTokens;
      usage.failure_count = Number(usage.failure_count) || accountFailures;
    }

    return usage;
  }

  function getInitialSummaryWindow(store) {
    const savedValue = store && typeof store.getItem === 'function'
      ? store.getItem(SUMMARY_WINDOW_KEY)
      : null;
    return SUMMARY_WINDOWS.has(savedValue) ? savedValue : DEFAULT_SUMMARY_WINDOW;
  }

  function summaryWindowLabel(summaryWindow) {
    if (summaryWindow === 'today') return 'Today';
    if (summaryWindow === 'last_30_days') return '30 days';
    return '7 days';
  }

  function emptySummaryWindow() {
    return { requests: 0, tokens: 0, cost_usd: 0, errors: 0 };
  }

  function normalizeSummaryWindow(windowValue) {
    const source = windowValue || {};
    return {
      requests: Number(source.requests) || 0,
      tokens: Number(source.tokens) || 0,
      cost_usd: Number(source.cost_usd) || 0,
      errors: Number(source.errors) || 0,
    };
  }

  function normalizeSummaryPayload(summary) {
    const source = summary || {};
    return {
      lifetime: normalizeSummaryWindow(source.lifetime),
      today: normalizeSummaryWindow(source.today),
      last_7_days: normalizeSummaryWindow(source.last_7_days),
      last_30_days: normalizeSummaryWindow(source.last_30_days),
    };
  }

  function getSummaryWindowValue(summary, summaryWindow) {
    if (!summary || !SUMMARY_WINDOWS.has(summaryWindow) || !summary[summaryWindow]) {
      return emptySummaryWindow();
    }
    return normalizeSummaryWindow(summary[summaryWindow]);
  }

  function comparisonText(value, type, summaryWindow) {
    const label = summaryWindow === 'today' ? 'today' : `in ${summaryWindowLabel(summaryWindow).toLowerCase()}`;
    if (type === 'cost') {
      return `$${value.toFixed(2)} ${label}`;
    }
    if (type === 'tokens') {
      return `${formatTokens(value)} ${label}`;
    }
    return `${formatNumber(value)} ${label}`;
  }

  function renderSummaryCards(summary, summaryWindow) {
    const normalizedSummary = normalizeSummaryPayload(summary);
    const lifetime = normalizedSummary.lifetime;
    const comparison = getSummaryWindowValue(normalizedSummary, summaryWindow);

    document.getElementById('metric-requests').textContent = formatNumber(lifetime.requests);
    document.getElementById('metric-requests-window').textContent = comparisonText(comparison.requests, 'requests', summaryWindow);
    document.getElementById('metric-requests-trend').textContent = lifetime.requests > 0 ? 'Lifetime total' : 'No requests yet';

    document.getElementById('metric-tokens').textContent = formatTokens(lifetime.tokens);
    document.getElementById('metric-tokens-window').textContent = comparisonText(comparison.tokens, 'tokens', summaryWindow);
    document.getElementById('metric-tokens-trend').textContent = `${formatTokens(lifetime.tokens)} total`;

    document.getElementById('metric-cost').textContent = `$${lifetime.cost_usd.toFixed(2)}`;
    document.getElementById('metric-cost-window').textContent = comparisonText(comparison.cost_usd, 'cost', summaryWindow);
    document.getElementById('metric-cost-trend').textContent = '$0.00001/token lifetime estimate';

    document.getElementById('metric-errors').textContent = formatNumber(lifetime.errors);
    document.getElementById('metric-errors-window').textContent = comparisonText(comparison.errors, 'errors', summaryWindow);
    document.getElementById('metric-errors-trend').textContent = `${formatNumber(lifetime.errors)} total`;
  }

  function setSummaryWindow(summaryWindow, store) {
    const nextWindow = SUMMARY_WINDOWS.has(summaryWindow) ? summaryWindow : DEFAULT_SUMMARY_WINDOW;
    document.querySelectorAll('[data-summary-window]').forEach((button) => {
      button.classList.toggle('active', button.dataset.summaryWindow === nextWindow);
    });
    if (store && typeof store.setItem === 'function') {
      store.setItem(SUMMARY_WINDOW_KEY, nextWindow);
    }
    return nextWindow;
  }

  // Load usage statistics
  async function loadUsage() {
    const [data, accountData] = await Promise.all([
      apiFetch('/usage'),
      apiFetch('/account-usage').catch(() => null)
    ]);
    if (!data) {
      renderMockMetrics();
      return;
    }

    state.usage = buildDashboardUsage(data, accountData, state.usage);
    state.summaryWindow = setSummaryWindow(state.summaryWindow, storage);
    renderSummaryCards(state.usage.summary, state.summaryWindow);

    if (state.usage.apis) {
      updateQuotaRings(state.usage.apis, state.quotaSummary);
    }

    // Wait for accounts to load before counting
    setTimeout(() => {
      const activeCount = state.accounts.filter(a => !a.disabled).length;
      document.getElementById('active-accounts-count').textContent = `${activeCount} Active`;
    }, 100);
  }

  // Render mock metrics when API unavailable
  function renderMockMetrics() {
    ['metric-requests', 'metric-tokens', 'metric-cost', 'metric-errors'].forEach((id) => {
      document.getElementById(id).textContent = '--';
    });
    document.getElementById('metric-requests-trend').textContent = 'API unavailable';
    document.getElementById('metric-tokens-trend').textContent = 'Enter API key';
    document.getElementById('metric-cost-trend').textContent = '--';
    document.getElementById('metric-errors-trend').textContent = '--';
    ['metric-requests-window', 'metric-tokens-window', 'metric-cost-window', 'metric-errors-window'].forEach((id) => {
      document.getElementById(id).textContent = '--';
    });
  }

  // Update quota rings
  function updateQuotaRings(apis, quotaSummary) {
    const syncing = isQuotaStartupSyncing() && !quotaSummary;
    const fivehPercent = syncing ? null : (quotaSummary ? quotaSummary.primary_used_percent : null);
    const sevendPercent = syncing ? null : (quotaSummary ? quotaSummary.secondary_used_percent : null);

    const apiKeys = apis && Object.keys(apis);
    const total5hTokens = apiKeys ? apiKeys.reduce((sum, key) => sum + ((((apis[key].last_5_hours) || {}).total_tokens) || 0), 0) : 0;
    const total7dTokens = apiKeys ? apiKeys.reduce((sum, key) => sum + ((((apis[key].last_7_days) || {}).total_tokens) || 0), 0) : 0;

    document.getElementById('quota-5h-percent').textContent = fivehPercent == null ? 'syncing' : (fivehPercent + '%');
    document.getElementById('quota-5h-remaining').textContent = syncing ? 'warming up' : (formatTokens(total5hTokens) + ' tokens');

    animateRing(document.getElementById('quota-5h-ring'), fivehPercent == null ? 0 : fivehPercent);

    document.getElementById('quota-7d-percent').textContent = sevendPercent == null ? 'syncing' : (sevendPercent + '%');
    document.getElementById('quota-7d-remaining').textContent = syncing ? 'warming up' : (formatTokens(total7dTokens) + ' tokens');

    animateRing(document.getElementById('quota-7d-ring'), sevendPercent == null ? 0 : sevendPercent);

    if (apiKeys && apiKeys.length > 0) {
      renderQuotaLegend('quota-5h-legend', apiKeys.map((key, i) => ({
        email: key,
        used: (((apis[key].last_5_hours) || {}).total_tokens) || 0,
        color: accountColors[i % accountColors.length]
      })));

      renderQuotaLegend('quota-7d-legend', apiKeys.map((key, i) => ({
        email: key,
        used: (((apis[key].last_7_days) || {}).total_tokens) || 0,
        color: accountColors[i % accountColors.length]
      })));
    } else {
      renderQuotaLegend('quota-5h-legend', []);
      renderQuotaLegend('quota-7d-legend', []);
    }
  }

  function getRingCircumference(ring) {
    const radius = Number(ring && ring.getAttribute('r')) || 45;
    return 2 * Math.PI * radius;
  }

  // Animate quota ring
  function animateRing(ring, percent) {
    if (!ring) return;

    const safePercent = Math.max(0, Math.min(100, Number(percent) || 0));
    const circumference = getRingCircumference(ring);
    const offset = circumference - (safePercent / 100) * circumference;
    ring.style.strokeDasharray = circumference;
    ring.style.strokeDashoffset = offset;
  }

  // Render quota legend
  function renderQuotaLegend(elementId, items) {
    const el = document.getElementById(elementId);
    if (!el) return;

    el.innerHTML = items.map(item => `
      <div class="quota-legend-row">
        <div class="quota-legend-swatch" style="background: ${item.color}"></div>
        <span class="quota-legend-email">${escapeHtml(item.email)}</span>
        <span class="quota-legend-value">${formatTokens(item.used)}</span>
      </div>
    `).join('');
  }

  function computeQuotaSummaryFromQuotas(quotas) {
    const quotaList = Array.isArray(quotas) ? quotas : [];

    function computeWindowAverage(windowKey) {
      const values = quotaList
        .map(quota => {
          const window = quota && quota[windowKey];
          if (!window || window.used_percent == null) {
            return null;
          }
          const value = Number(window.used_percent);
          return Number.isFinite(value) ? value : null;
        })
        .filter(Number.isFinite);

      if (values.length === 0) {
        return null;
      }

      const average = values.reduce((sum, value) => sum + value, 0) / values.length;
      return Math.max(0, Math.min(100, Math.round(average)));
    }

    return {
      primary_used_percent: computeWindowAverage('primary_window'),
      secondary_used_percent: computeWindowAverage('secondary_window')
    };
  }

  function hasFutureRetry(retryAt) {
    if (!retryAt) return false;
    const retryDate = new Date(retryAt);
    return !Number.isNaN(retryDate.getTime()) && retryDate.getTime() > Date.now();
  }

  function hasQuotaCooldown(window) {
    if (!window) return false;
    const usedPercent = Number(window.used_percent) || 0;
    if (usedPercent < 100) return false;
    const resetAt = Number(window.reset_at) || 0;
    if (resetAt <= 0) return true;
    return (resetAt * 1000) > Date.now();
  }

  function isQuotaStartupSyncing() {
    return state.quotaStartupSync && state.quotaStartupSync.state === 'syncing';
  }

  function getStartupSyncAccountStatus(file, quota) {
    if (!isQuotaStartupSyncing()) return null;
    const hasQuotaData = Boolean(quota && (quota.primary_window || quota.secondary_window || quota.fetch_error));
    if (hasQuotaData) return null;

    const backendStatus = String(file?.status || '').trim().toLowerCase();
    if (file?.disabled || backendStatus === 'disabled') {
      return null;
    }

    return { key: 'syncing', label: 'syncing' };
  }

  function getRecoveryAccountStatus(file) {
    const recovery = file?.recovery || {};
    if (recovery.in_flight) {
      return { key: 'syncing', label: 'syncing' };
    }

    if (!file?.unavailable || !hasFutureRetry(file?.next_retry_after)) {
      return null;
    }

    const statusMessage = String(file?.status_message || '').trim().toLowerCase();
    if (statusMessage.includes('quota') || statusMessage.includes('rate')) {
      return { key: 'rate_limited', label: 'rate limited' };
    }

    return { key: 'error', label: 'error' };
  }

  function deriveAccountStatus(file, quota) {
    const backendStatus = String(file?.status || '').trim().toLowerCase();
    const statusMessage = String(file?.status_message || '').trim().toLowerCase();

    if (file?.disabled) {
      return { key: 'paused', label: 'paused' };
    }
    if (backendStatus === 'disabled') {
      return { key: 'disabled', label: 'disabled' };
    }
    if (backendStatus === 'deactivated') {
      return { key: 'deactivated', label: 'deactivated' };
    }
    if (backendStatus === 'rate_limited') {
      return { key: 'rate_limited', label: 'rate limited' };
    }

    const startupSyncStatus = getStartupSyncAccountStatus(file, quota);
    if (startupSyncStatus) {
      return startupSyncStatus;
    }

    const recoveryStatus = getRecoveryAccountStatus(file);
    if (recoveryStatus) {
      return recoveryStatus;
    }

    if (hasQuotaCooldown(quota?.primary_window) || hasQuotaCooldown(quota?.secondary_window)) {
      return { key: 'rate_limited', label: 'rate limited' };
    }

    if (backendStatus === 'error') {
      return { key: 'error', label: 'error' };
    }
    if (backendStatus === 'unknown' || backendStatus === 'refreshing' || backendStatus === 'pending') {
      return { key: 'syncing', label: 'syncing' };
    }

    return { key: 'active', label: 'active' };
  }

  function formatResetCountdown(resetAt) {
    const unixSeconds = Number(resetAt) || 0;
    if (unixSeconds <= 0) {
      return isQuotaStartupSyncing() ? 'syncing' : 'n/a';
    }

    const diffMs = (unixSeconds * 1000) - Date.now();
    if (diffMs <= 0) return 'ready';

    const totalMinutes = Math.ceil(diffMs / 60000);
    const days = Math.floor(totalMinutes / (24 * 60));
    const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
    const minutes = totalMinutes % 60;

    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  }

  function getUsageDisplayValue(usedPercent) {
    if (usedPercent == null && isQuotaStartupSyncing()) {
      return { percent: null, label: 'syncing' };
    }

    const safePercent = Math.max(0, Math.min(100, Number(usedPercent) || 0));
    return { percent: safePercent, label: safePercent + '%' };
  }

  function getRemainingQuotaDisplay(usedPercent) {
    if (usedPercent == null) {
      if (isQuotaStartupSyncing()) {
        return { percent: null, label: 'syncing', color: 'var(--accent)' };
      }
      return { percent: null, label: 'unknown', color: 'var(--text-muted)' };
    }

    const remaining = Math.max(0, 100 - Number(usedPercent));
    return { percent: remaining, label: remaining + '%', color: getQuotaColor(remaining) };
  }

  function renderResetCountdowns(acc) {
    if (!acc) return '';
    return `
      <div class="detail-reset-block">
        <div class="detail-reset-title">Resets in</div>
        <div class="detail-reset-line"><span>5h</span><span>${escapeHtml(formatResetCountdown(acc.primaryResetAt))}</span></div>
        <div class="detail-reset-line"><span>7d</span><span>${escapeHtml(formatResetCountdown(acc.secondaryResetAt))}</span></div>
      </div>
    `;
  }

  function renderDashboardResetCountdowns(acc) {
    if (!acc) return '';
    return `
      <div class="account-reset-block">
        <div class="account-reset-title">Resets in</div>
        <div class="account-reset-line"><span>5h</span><span>${escapeHtml(formatResetCountdown(acc.primaryResetAt))}</span></div>
        <div class="account-reset-line"><span>7d</span><span>${escapeHtml(formatResetCountdown(acc.secondaryResetAt))}</span></div>
      </div>
    `;
  }

  function ensureDetailCountdownTimer() {
    if (detailCountdownTimer) return;
    detailCountdownTimer = setInterval(() => {
      renderDashboardAccounts();
      if (selectedAccountId) {
        selectAccount(selectedAccountId);
      }
    }, 60000);
  }

  function updateStartupSyncPolling() {
    if (isQuotaStartupSyncing()) {
      if (startupSyncPollTimer) return;
      startupSyncPollTimer = setInterval(() => {
        loadAccounts();
      }, 3000);
      return;
    }

    if (startupSyncPollTimer) {
      clearInterval(startupSyncPollTimer);
      startupSyncPollTimer = null;
    }
  }

  // Load accounts
  async function loadAccounts() {
    const [authData, quotaData] = await Promise.all([
      apiFetch('/auth-files'),
      apiFetch('/quotas').catch(() => ({ quotas: [] }))
    ]);
    if (!authData || !authData.files) return;

    state.quotaStartupSync = quotaData && quotaData.startup_sync ? quotaData.startup_sync : null;

    // Build quota map for quick lookup
    const quotaMap = {};
    if (quotaData && quotaData.quotas) {
      quotaData.quotas.forEach(q => {
        quotaMap[q.account_id] = q;
      });
    }

    state.quotaSummary = computeQuotaSummaryFromQuotas(quotaData && quotaData.quotas);

    state.accounts = authData.files.map((file, i) => {
      const quota = quotaMap[file.name] || {};
      const primaryUsed = quota.primary_window?.used_percent;
      const secondaryUsed = quota.secondary_window?.used_percent;

      const accountStatus = deriveAccountStatus(file, quota);

      return {
        id: file.name,
        email: file.email || file.name.replace('.json', '').replace(/_/g, ' '),
        status: accountStatus.key,
        statusLabel: accountStatus.label,
        provider: file.provider || 'unknown',
        accountType: file.id_token?.plan_type || file.plan_type || file.account_type || quota.plan_type || 'unknown',
        disabled: file.disabled || false,
        primaryUsed: primaryUsed,
        secondaryUsed: secondaryUsed,
        primaryResetAt: quota.primary_window?.reset_at || 0,
        secondaryResetAt: quota.secondary_window?.reset_at || 0,
        creditsHas: quota.credits_has,
        creditsUnlimited: quota.credits_unlimited,
        creditsBalance: quota.credits_balance,
        color: accountColors[i % accountColors.length],
        raw: file
      };
    });

    renderDashboardAccounts();
    updateAccountFilters();
    if (selectedAccountId) {
      selectAccount(selectedAccountId);
    }
    if (state.usage && state.usage.apis) {
      updateQuotaRings(state.usage.apis, state.quotaSummary);
    }
    updateStartupSyncPolling();
  }

  // Render accounts on dashboard
  function renderDashboardAccounts() {
    const grid = document.getElementById('dashboard-accounts-grid');
    if (!grid) return;

    if (state.accounts.length === 0) {
      grid.className = 'accounts-grid count-0';
      grid.innerHTML = renderEmptyState();
      return;
    }

    const count = state.accounts.length;
    let gridClass = 'accounts-grid ';
    if (count === 1) gridClass += 'count-1';
    else if (count <= 4) gridClass += 'count-2';
    else gridClass += 'count-3';

    grid.className = gridClass;
    grid.innerHTML = state.accounts.map(acc => renderAccountCard(acc)).join('');
  }

  // Render accounts sidebar
  function renderAccountsSidebar() {
    const list = document.getElementById('accounts-list');
    const count = document.getElementById('accounts-count');
    if (!list) return;

    const count_val = state.accounts.length;
    count.textContent = count_val + ' account' + (count_val !== 1 ? 's' : '');

    if (state.accounts.length === 0) {
      list.innerHTML = `
        <div class="empty-state" style="padding: 24px; margin: 0;">
          <div class="empty-state-icon" style="width: 48px; height: 48px; margin-bottom: 12px;">
            ${icons.clock}
          </div>
          <div class="empty-state-title" style="font-size: 0.85rem;">No accounts</div>
          <div class="empty-state-desc" style="font-size: 0.75rem;">Click Add to configure</div>
        </div>
      `;
      return;
    }

    list.innerHTML = state.accounts.map(acc => renderAccountItem(acc)).join('');

    // Add click handlers
    list.querySelectorAll('.account-item').forEach(item => {
      item.addEventListener('click', () => {
        const accId = item.dataset.id;
        selectAccount(accId);
        list.querySelectorAll('.account-item').forEach(i => i.classList.remove('selected'));
        item.classList.add('selected');
      });
    });
  }

  // Render account item in sidebar
  function renderAccountItem(acc) {
    return `
      <button class="account-item" data-id="${escapeHtml(acc.id)}">
        <div class="account-item-header">
          <span class="account-item-email">${escapeHtml(acc.email)}</span>
          <span class="account-status-badge ${acc.status}">${escapeHtml(acc.statusLabel || acc.status)}</span>
        </div>
        <div class="account-item-meta">
          <span class="account-item-tier">${escapeHtml(acc.provider)} · ${escapeHtml(acc.accountType)}</span>
        </div>
      </button>
    `;
  }

  // Select and show account detail
  let selectedAccountId = null;

  function resolveAccountUsage(state, acc) {
    if (!state || !state.usage || !state.usage.apis || !acc) {
      return null;
    }

    if (acc.email && state.usage.apis[acc.email]) {
      return state.usage.apis[acc.email];
    }

    if (acc.id && state.usage.apis[acc.id]) {
      return state.usage.apis[acc.id];
    }

    return null;
  }

  function selectAccount(accId) {
    selectedAccountId = accId;
    storage.setItem('dashboard-selected-account', accId);
    const acc = state.accounts.find(a => a.id === accId);
    if (!acc) return;

    const panel = document.getElementById('account-detail-panel');
    if (!panel) return;

    const usage = resolveAccountUsage(state, acc);

    const totalRequests = usage ? usage.total_requests || 0 : 0;
    const totalTokens = usage ? usage.total_tokens || 0 : 0;
    const failedReqs = usage ? usage.failure_count || 0 : 0;

    const primaryUsage = getUsageDisplayValue(acc.primaryUsed);
    const secondaryUsage = getUsageDisplayValue(acc.secondaryUsed);

    panel.innerHTML = `
      <div class="detail-header">
        <div>
          <div class="detail-title">${escapeHtml(acc.email)}</div>
          <div class="detail-subtitle">
            ${escapeHtml(acc.provider)}
            ${acc.accountType && acc.accountType !== 'oauth' ? ' · ' + escapeHtml(acc.accountType) : ''}
          </div>
        </div>
        <span class="account-status-badge ${acc.status}">${escapeHtml(acc.statusLabel || acc.status)}</span>
      </div>
      <div class="detail-cards-grid">
        <div class="detail-card">
          <div class="detail-card-title">Usage</div>
          <div class="usage-block">
            <div class="usage-header">
              <span class="usage-label">5h</span>
              <span class="usage-value">${primaryUsage.label}</span>
            </div>
            <div class="usage-bar">
              <div class="usage-bar-fill" style="width: ${primaryUsage.percent || 0}%"></div>
            </div>
            <div class="usage-meta">${formatNumber(totalRequests)} req · ${formatTokens(totalTokens)} tok</div>
          </div>
          <div class="usage-block">
            <div class="usage-header">
              <span class="usage-label">7d</span>
              <span class="usage-value">${secondaryUsage.label}</span>
            </div>
            <div class="usage-bar">
              <div class="usage-bar-fill secondary" style="width: ${secondaryUsage.percent || 0}%"></div>
            </div>
            <div class="usage-meta">${formatNumber(totalRequests)} req · ${formatTokens(totalTokens)} tok</div>
          </div>
        </div>
        <div class="detail-card">
          <div class="detail-card-title">Tokens</div>
          <div class="token-grid">
            <div class="token-item">
              <span class="token-label">Input</span>
              <span class="token-value">${formatTokens(Math.round(totalTokens * 0.4))}</span>
            </div>
            <div class="token-item">
              <span class="token-label">Output</span>
              <span class="token-value success">${formatTokens(Math.round(totalTokens * 0.6))}</span>
            </div>
            <div class="token-item">
              <span class="token-label">Cache</span>
              <span class="token-value warn">${formatTokens(Math.round(totalTokens * 0.15))}</span>
            </div>
            <div class="token-item">
              <span class="token-label">Errors</span>
              <span class="token-value ${failedReqs > 0 ? 'error' : ''}">${failedReqs}</span>
            </div>
          </div>
        </div>
      </div>
      <div class="detail-actions">
        ${renderResetCountdowns(acc)}
        <div class="detail-actions-buttons">
          <button class="detail-btn" onclick="toggleAccount('${escapeHtml(acc.id)}', ${acc.disabled})">
            ${acc.disabled ? 'Resume' : 'Pause'}
          </button>
          <button class="detail-btn danger" onclick="deleteAccount('${escapeHtml(acc.id)}')">Delete</button>
        </div>
      </div>
    `;
  }

  // Render account card
  function renderAccountCard(acc, showDetails = false) {
    const primaryRemaining = getRemainingQuotaDisplay(acc.primaryUsed);
    const secondaryRemaining = getRemainingQuotaDisplay(acc.secondaryUsed);

    return `
      <div class="account-card">
        <div class="account-header">
          <div>
            <span class="account-email">${escapeHtml(acc.email)}</span>
            <div class="account-provider">${escapeHtml(acc.provider)} · ${escapeHtml(acc.accountType)}</div>
          </div>
          <span class="account-status ${acc.status}">
            <span class="account-status-dot"></span>
            ${escapeHtml(acc.statusLabel || acc.status)}
          </span>
        </div>
        <div class="account-quotas">
          <div class="account-quota-row">
            <span class="account-quota-label">5h</span>
            <div class="account-quota-bar">
              <div class="account-quota-fill primary" style="width: ${primaryRemaining.percent || 0}%; background-color: ${primaryRemaining.color}"></div>
            </div>
            <span class="account-quota-value" style="color: ${primaryRemaining.color}">${primaryRemaining.label}</span>
          </div>
          <div class="account-quota-row">
            <span class="account-quota-label">7d</span>
            <div class="account-quota-bar">
              <div class="account-quota-fill secondary" style="width: ${secondaryRemaining.percent || 0}%; background-color: ${secondaryRemaining.color}"></div>
            </div>
            <span class="account-quota-value" style="color: ${secondaryRemaining.color}">${secondaryRemaining.label}</span>
          </div>
        </div>
        <div class="account-footer">
          ${renderDashboardResetCountdowns(acc)}
          <div class="account-footer-actions">
            <button class="account-action" onclick="toggleAccount('${escapeHtml(acc.id)}', ${acc.disabled})" title="${acc.disabled ? 'Resume' : 'Pause'}">
              ${acc.disabled ? '\u25B6 Resume' : '\u23F8 Pause'}
            </button>
            <button class="account-action danger" onclick="deleteAccount('${escapeHtml(acc.id)}')" title="Delete">\u2715 Delete</button>
          </div>
        </div>
      </div>
    `;
  }

  async function toggleAccount(accountId, currentlyDisabled) {
    const nextDisabled = !currentlyDisabled;
    try {
      const res = await apiFetch('/auth-files/status', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: accountId, disabled: nextDisabled })
      });
      if (res && !res.error) {
        showToast(nextDisabled ? 'Account paused' : 'Account resumed', 'success');
        loadAccounts();
      } else {
        showToast('Failed to update account', 'error');
      }
    } catch (e) {
      showToast('Failed to update account', 'error');
    }
  }

  if (typeof window !== 'undefined') {
    window.toggleAccount = toggleAccount;
  }

  function leadingSpaces(line) {
    const match = line.match(/^\s*/);
    return match ? match[0].length : 0;
  }

  function isIgnorableYAMLLine(line) {
    const trimmed = line.trim();
    return trimmed === '' || trimmed.startsWith('#');
  }

  function findYAMLBlockEnd(lines, startIndex, parentIndent) {
    for (let i = startIndex + 1; i < lines.length; i++) {
      if (isIgnorableYAMLLine(lines[i])) continue;
      if (leadingSpaces(lines[i]) <= parentIndent) return i;
    }
    return lines.length;
  }

  function findYAMLPath(lines, path) {
    let searchStart = 0;
    let blockEnd = lines.length;
    let indent = 0;

    for (let idx = 0; idx < path.length; idx++) {
      const segment = path[idx];
      let found = -1;

      for (let i = searchStart; i < blockEnd; i++) {
        if (isIgnorableYAMLLine(lines[i])) continue;
        const line = lines[i];
        if (leadingSpaces(line) !== indent) continue;
        if (line.trimStart().startsWith(`${segment}:`)) {
          found = i;
          break;
        }
      }

      if (found === -1) {
        return { found: false, insertAt: blockEnd, indent, segment, parentIndex: searchStart - 1 };
      }

      if (idx === path.length - 1) {
        return { found: true, index: found, indent };
      }

      blockEnd = findYAMLBlockEnd(lines, found, indent);
      searchStart = found + 1;
      indent += 2;
    }

    return { found: false, insertAt: lines.length, indent: 0 };
  }

  function parseYAMLScalar(raw) {
    const trimmed = raw.trim();
    if (trimmed === '') return '';
    if (trimmed === 'true') return true;
    if (trimmed === 'false') return false;
    if (/^-?\d+$/.test(trimmed)) return parseInt(trimmed, 10);
    if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
      try {
        return JSON.parse(trimmed.replace(/^'/, '"').replace(/'$/, '"'));
      } catch (e) {
        return trimmed.slice(1, -1);
      }
    }
    return trimmed;
  }

  function getYAMLValue(content, path) {
    const lines = content.replace(/\r\n/g, '\n').split('\n');
    const located = findYAMLPath(lines, path);
    if (!located.found) return null;
    const line = lines[located.index];
    const raw = line.replace(/^\s*[^:]+:\s*/, '').replace(/\s+#.*$/, '');
    return parseYAMLScalar(raw);
  }

  function serializeYAMLValue(value, type) {
    if (type === 'bool') return value ? 'true' : 'false';
    if (type === 'int') return String(parseInt(value, 10) || 0);
    return JSON.stringify(value == null ? '' : String(value));
  }

  function setYAMLValue(content, path, value, type) {
    const lines = content.replace(/\r\n/g, '\n').split('\n');
    const serialized = serializeYAMLValue(value, type);

    for (let depth = 1; depth <= path.length; depth++) {
      const subPath = path.slice(0, depth);
      const located = findYAMLPath(lines, subPath);
      const indent = (depth - 1) * 2;
      if (located.found) continue;
      const line = `${' '.repeat(indent)}${subPath[subPath.length - 1]}:${depth === path.length ? ` ${serialized}` : ''}`;
      lines.splice(located.insertAt, 0, line);
    }

    const located = findYAMLPath(lines, path);
    const key = path[path.length - 1].replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    lines[located.index] = lines[located.index].replace(
      new RegExp(`^(\\s*${key}:\\s*)([^#]*?)(\\s+#.*)?$`),
      (_, prefix, __, suffix = '') => `${prefix}${serialized}${suffix || ''}`
    );
    return lines.join('\n');
  }

  async function deleteAccount(accountId) {
    if (!confirm('Delete account ' + accountId + '? This cannot be undone.')) return;

    try {
      const res = await apiFetch('/auth-files?name=' + encodeURIComponent(accountId), {
        method: 'DELETE'
      });
      if (res && (res.status === 'ok' || res.deleted)) {
        showToast('Account deleted', 'success');
        loadAccounts();
      } else {
        showToast('Failed to delete account', 'error');
      }
    } catch (e) {
      showToast('Failed to delete account', 'error');
    }
  }

  if (typeof window !== 'undefined') {
    window.deleteAccount = deleteAccount;
  }

  // Render empty state
  function renderEmptyState() {
    return `
      <div class="empty-state">
        <div class="empty-state-icon">${icons.clock}</div>
        <div class="empty-state-title">No accounts configured</div>
        <div class="empty-state-desc">Add your first API account to start routing requests.</div>
        <button class="btn btn-primary" onclick="document.getElementById('add-account-modal').classList.add('active')">
          ${icons.plus}
          Add Account
        </button>
      </div>
    `;
  }

  // Modal state
  let modalStep = 1;
  let selectedProvider = null;

  function closeModal() {
    document.getElementById('add-account-modal').classList.remove('active');
    resetModal();
  }

  function resetModal() {
    modalStep = 1;
    selectedProvider = null;
    oauthState = null;

    document.querySelectorAll('.modal-step').forEach((s, i) => {
      s.classList.toggle('active', i === 0);
      s.classList.toggle('done', false);
    });
    document.querySelectorAll('.modal-content').forEach(c => c.classList.remove('active'));
    document.getElementById('step-content-1').style.display = 'block';
    document.getElementById('step-content-2').style.display = 'none';
    document.getElementById('step-content-3').style.display = 'none';
    document.querySelectorAll('.provider-btn').forEach(b => b.classList.remove('selected'));
    document.getElementById('modal-next').textContent = 'Continue';
    document.getElementById('modal-back').style.display = 'none';
    // Reset step 2
    const authChoice = document.getElementById('auth-method-choice');
    if (authChoice) authChoice.style.display = 'flex';
    const oauthFlowReset = document.getElementById('oauth-flow');
    if (oauthFlowReset) oauthFlowReset.style.display = 'none';
    const apikeyFlowReset = document.getElementById('apikey-flow');
    if (apikeyFlowReset) apikeyFlowReset.style.display = 'none';
    document.querySelectorAll('.auth-method-btn').forEach(b => b.classList.remove('selected'));
    document.getElementById('modal-actions').classList.remove('hidden');
    // Reset OAuth sections
    document.getElementById('oauth-flow').classList.remove('visible');
    document.getElementById('apikey-flow').classList.remove('visible');
    document.getElementById('api-key-form').value = '';
    document.getElementById('api-key-label').value = '';
    const statusEl = document.getElementById('oauth-status');
    if (statusEl) statusEl.style.display = 'none';
  }

  function selectProvider(provider) {
    selectedProvider = provider;
    document.querySelectorAll('.provider-btn').forEach(b => b.classList.remove('selected'));
    document.querySelector(`.provider-btn[data-provider="${provider}"]`).classList.add('selected');
    document.getElementById('modal-next').disabled = false;
  }

  function handleModalNext() {
    if (modalStep === 1) {
      if (!selectedProvider) {
        showToast('Select a provider first', 'error');
        return;
      }
      modalStep = 2;
      updateModalSteps();
      document.getElementById('step-content-1').style.display = 'none';
      document.getElementById('step-content-2').style.display = 'block';
      const choice = document.getElementById('auth-method-choice');
      if (choice) choice.style.display = 'flex';
      const oauthFlow = document.getElementById('oauth-flow');
      if (oauthFlow) oauthFlow.style.display = 'none';
      const apikeyFlow = document.getElementById('apikey-flow');
      if (apikeyFlow) apikeyFlow.style.display = 'none';
      document.getElementById('modal-next').textContent = 'Add Account';
      document.getElementById('modal-back').style.display = 'flex';
    } else if (modalStep === 2) {
      const apikeyFlow = document.getElementById('apikey-flow');
      if (apikeyFlow && apikeyFlow.style.display !== 'none') {
        const apiKey = document.getElementById('api-key-form').value.trim();
        if (!apiKey) {
          showToast('Enter an API key', 'error');
          return;
        }
        submitApiKeyAccount(apiKey);
      } else {
        const oauthFlow = document.getElementById('oauth-flow');
        if (oauthFlow && oauthFlow.style.display !== 'none') {
          showToast('Waiting for OAuth...', 'info');
        } else {
          showToast('Select OAuth or API Key first', 'error');
        }
      }
    }
  }

  function handleModalBack() {
    if (modalStep === 2) {
      modalStep = 1;
      updateModalSteps();
      document.getElementById('step-content-2').style.display = 'none';
      document.getElementById('step-content-1').style.display = 'block';
      document.getElementById('modal-next').textContent = 'Continue';
      document.getElementById('modal-back').style.display = 'none';
      // Reset step 2
      document.getElementById('auth-method-choice').style.display = 'flex';
      document.getElementById('oauth-flow').style.display = 'none';
      document.getElementById('apikey-flow').style.display = 'none';
    }
  }

  function updateModalSteps() {
    document.querySelectorAll('.modal-step').forEach((s, i) => {
      s.classList.toggle('active', i === modalStep - 1);
      s.classList.toggle('done', i < modalStep - 1);
    });
    document.getElementById('modal-back').style.display = modalStep > 1 ? 'flex' : 'none';
  }

  let oauthState = null;
  let oauthPopup = null;

  async function startOAuthFlow() {
    const statusEl = document.getElementById('oauth-status');
    statusEl.style.display = 'block';
    statusEl.className = 'oauth-status pending';
    statusEl.textContent = 'Opening OAuth window...';

    const data = await apiFetch('/' + selectedProvider + '-auth-url');
    if (data && data.url) {
      // Open OAuth in a popup window
      const width = 600;
      const height = 700;
      const left = window.screenX + (window.outerWidth - width) / 2;
      const top = window.screenY + (window.outerHeight - height) / 2;
      oauthPopup = window.open(
        data.url,
        'oauth_popup',
        `width=${width},height=${height},left=${left},top=${top},toolbar=no,menubar=no`
      );

      if (oauthPopup) {
        oauthState = data.state || 'oauth_pending';
        statusEl.textContent = 'Waiting for authorization. Complete login in the popup window.';
        pollOAuthPopup();
      } else {
        // Fallback to new tab if popup is blocked
        window.open(data.url, '_blank');
        oauthState = data.state || 'oauth_pending';
        statusEl.textContent = 'Waiting for authorization. Complete login in the browser window.';
        pollForAuthComplete();
      }
    } else {
      statusEl.className = 'oauth-status error';
      statusEl.textContent = 'OAuth not available for this provider. Use API Key instead.';
    }
  }

  function pollOAuthPopup() {
    if (!oauthPopup || oauthPopup.closed) {
      if (oauthState) {
        pollForAuthComplete();
      }
      return;
    }

    // Check if popup URL changed to our callback
    try {
      const popupUrl = oauthPopup.location.href;
      if (popupUrl.includes('localhost:' + (window.location.port || 8318)) &&
          (popupUrl.includes('oauth_callback=success') || popupUrl.includes('/?'))) {
        oauthPopup.close();
        pollForAuthComplete();
        return;
      }
    } catch (e) {
      // Cross-origin error - popup is still on OAuth provider's page
    }

    // Check auth status via API
    apiFetch('/get-auth-status?state=' + (oauthState || ''))
      .then(data => {
        if (data && data.status === 'ok') {
          oauthPopup?.close();
          showToast('Account added successfully!', 'success');
          closeModal();
          loadAccounts();
        } else if (data && data.status === 'error') {
          oauthPopup?.close();
          showToast('OAuth error: ' + (data.error || 'Unknown'), 'error');
          closeModal();
        } else {
          setTimeout(pollOAuthPopup, 1000);
        }
      })
      .catch(() => {
        setTimeout(pollOAuthPopup, 1000);
      });
  }

  function showOAuthSuccess() {
    modalStep = 3;
    updateModalSteps();
    document.getElementById('step-content-2').classList.remove('visible');
    document.getElementById('step-content-3').style.display = 'block';
    document.getElementById('modal-actions').classList.add('hidden');
  }

  async function submitApiKeyAccount(apiKey) {
    const label = document.getElementById('api-key-label').value.trim();
    showToast('Adding account...', 'info');

    // Create a JSON file content with the API key
    const authData = {
      provider: selectedProvider,
      label: label || (selectedProvider.charAt(0).toUpperCase() + selectedProvider.slice(1) + ' Key'),
      api_key: apiKey
    };

    try {
      const blob = new Blob([JSON.stringify(authData, null, 2)], { type: 'application/json' });
      const formData = new FormData();
      formData.append('file', blob, selectedProvider + '_' + Date.now() + '.json');

      const res = await fetch('/v0/management/auth-files', {
        method: 'POST',
        headers: { 'X-Management-Key': state.apiKey },
        body: formData
      });

      if (res.ok) {
        showToast('Account added successfully', 'success');
        closeModal();
        loadAccounts();
      } else {
        const err = await res.json();
        showToast('Failed: ' + (err.error || 'Unknown error'), 'error');
      }
    } catch (e) {
      showToast('Failed: ' + e.message, 'error');
    }
  }

  function getLogsSelectedAccounts() {
    const filter = document.getElementById('logs-account-filter');
    return String(filter && filter.value || '').split(',').map(value => value.trim()).filter(Boolean);
  }

  function setLogsSelectedAccounts(selected) {
    const filter = document.getElementById('logs-account-filter');
    if (!filter) return;
    filter.value = Array.isArray(selected) ? selected.join(',') : '';
  }

  function openLogsAccountMenu() {
    const trigger = document.getElementById('logs-account-trigger');
    const menu = document.getElementById('logs-account-menu');
    if (!trigger || !menu) return;
    trigger.setAttribute('aria-expanded', 'true');
    trigger.classList.add('open');
    menu.hidden = false;
  }

  function dismissLogsAccountMenu() {
    const trigger = document.getElementById('logs-account-trigger');
    const menu = document.getElementById('logs-account-menu');
    if (!trigger || !menu) return;
    trigger.setAttribute('aria-expanded', 'false');
    trigger.classList.remove('open');
    menu.hidden = true;
  }

  // Update account filters
  function updateAccountFilters() {
    const menu = document.getElementById('logs-account-menu');
    if (!menu) return;

    const available = new Set(state.accounts.map(acc => acc.email || acc.id || '').filter(Boolean));
    const selected = getLogsSelectedAccounts().filter(value => available.has(value));
    setLogsSelectedAccounts(selected);

    const options = state.accounts.map(acc => {
      const value = acc.email || acc.id || '';
      const label = acc.email || acc.id || '--';
      const checked = selected.includes(value) ? ' checked' : '';
      return `<label class="logs-account-option"><input type="checkbox" value="${escapeHtml(value)}"${checked}> <span>${escapeHtml(label)}</span></label>`;
    }).join('');

    menu.innerHTML = options || '<div class="logs-account-empty">No accounts</div>';
    updateLogsAccountSummary(selected);
  }

  function updateLogsAccountSummary(selected) {
    const summary = document.getElementById('logs-account-summary');
    if (!summary) return;
    if (!Array.isArray(selected) || selected.length === 0) {
      summary.textContent = 'All Accounts';
      return;
    }
    if (selected.length === 1) {
      summary.textContent = selected[0];
      return;
    }
    summary.textContent = `${selected[0]} +${selected.length - 1}`;
  }

  function setLogsStatusFilterValue(value) {
    const filter = document.getElementById('logs-status-filter');
    const summary = document.getElementById('logs-status-summary');
    const menu = document.getElementById('logs-status-menu');
    const normalized = value || 'All Status';
    if (filter) {
      filter.value = normalized;
    }
    if (summary) {
      summary.textContent = normalized;
    }
    if (menu) {
      menu.querySelectorAll('[data-status-value]').forEach(option => {
        const isActive = (option.dataset.statusValue || 'All Status') === normalized;
        option.classList.toggle('active', isActive);
        option.setAttribute('aria-pressed', isActive ? 'true' : 'false');
      });
    }
  }

  function openLogsStatusMenu() {
    const trigger = document.getElementById('logs-status-trigger');
    const menu = document.getElementById('logs-status-menu');
    if (!trigger || !menu) return;
    trigger.setAttribute('aria-expanded', 'true');
    trigger.classList.add('open');
    menu.hidden = false;
  }

  function dismissLogsStatusMenu() {
    const trigger = document.getElementById('logs-status-trigger');
    const menu = document.getElementById('logs-status-menu');
    if (!trigger || !menu) return;
    trigger.setAttribute('aria-expanded', 'false');
    trigger.classList.remove('open');
    menu.hidden = true;
  }

  function restoreCachedLogs() {
    try {
      const raw = storage.getItem(LOGS_CACHE_KEY);
      if (!raw) return;
      const cached = JSON.parse(raw);
      if (!Array.isArray(cached) || cached.length === 0) return;
      state.logs = cached;
      renderLogs(state.logs);
    } catch (error) {
      console.warn('Failed to restore cached logs:', error);
    }
  }

  function persistLogs(logs) {
    try {
      if (!Array.isArray(logs) || logs.length === 0) {
        storage.removeItem(LOGS_CACHE_KEY);
        return;
      }
      storage.setItem(LOGS_CACHE_KEY, JSON.stringify(logs.slice(0, LOGS_CACHE_LIMIT)));
    } catch (error) {
      console.warn('Failed to persist logs cache:', error);
    }
  }

  function mergeLogsWithCache(logs) {
    const merged = [];
    const seen = new Set();
    const cached = Array.isArray(state.logs) ? state.logs : [];
    for (const log of [...logs, ...cached]) {
      if (!log || !log.id) continue;
      if (seen.has(log.id)) continue;
      seen.add(log.id);
      merged.push(log);
      if (merged.length >= LOGS_CACHE_LIMIT) break;
    }
    return merged;
  }

  // Load logs
  async function loadLogs() {
    const activityData = await apiFetch('/request-activity?limit=50');
    const activityLogs = normalizeActivityEntries(activityData);
    if (activityLogs.length > 0) {
      state.logs = mergeLogsWithCache(activityLogs);
      state.logVisibleCount = LOGS_PAGE_SIZE;
      persistLogs(state.logs);
      filterLogs();
      return;
    }

    const data = await apiFetch('/logs?limit=50');
    const lineLogs = normalizeLogLines(data);
    if (lineLogs.length === 0) {
      console.log('Logs unavailable:', data?.error || 'No data');
      if (!state.logs || state.logs.length === 0) {
        renderEmptyLogs();
      }
      return;
    }

    state.logs = mergeLogsWithCache(lineLogs);
    state.logVisibleCount = LOGS_PAGE_SIZE;
    persistLogs(state.logs);
    filterLogs();
  }

  function normalizeActivityEntries(data) {
    if (!data || !Array.isArray(data.entries)) return [];

    return data.entries
      .filter(entry => {
        const method = String(entry?.method || '').trim().toUpperCase();
        const path = String(entry?.path || '').trim();
        const account = String(entry?.account || '').trim();
        return !(method === 'HEAD' && path === '/') && account !== '';
      })
      .map(entry => ({
        id: entry.id || '--',
        time: formatActivityTime(entry.requested_at),
        level: entry.status === 'error' ? 'error' : 'info',
        message: entry.message || `${entry.method || ''} ${entry.path || ''}`.trim() || '--',
        account: entry.account || '--',
        model: entry.model || '--',
        transport: entry.transport || entry.downstream_transport || '--',
        latency: formatLatency(entry.latency_ms),
        status: normalizeLogStatus(entry.status)
      }));
  }

  function normalizeLogLines(data) {
    if (!data || data.error || !Array.isArray(data.lines)) return [];
    return data.lines.map(parseLogLine).filter(Boolean);
  }

  // Parse log line - supports multiple formats
  function parseLogLine(line) {
    if (!line || typeof line !== 'string') return null;

    // Format: [2024-01-15 10:30:45] [INFO] Request completed in 234ms
    const infoMatch = line.match(/\[([^\]]+)\]\s+\[INFO\]\s+(.*)/i);
    if (infoMatch) {
      return {
        id: 'log_' + Date.now().toString(36) + Math.random().toString(36).substr(2, 3),
        time: formatLogTime(infoMatch[1]),
        level: 'info',
        message: infoMatch[2],
        account: extractAccountFromLine(infoMatch[2]),
        model: extractModelFromLine(infoMatch[2]),
        transport: '--',
        latency: extractLatencyFromLine(infoMatch[2]),
        status: 'success'
      };
    }

    // Format: [2024-01-15 10:30:45] [ERROR] Quota exceeded
    const errorMatch = line.match(/\[([^\]]+)\]\s+\[ERROR\]\s+(.*)/i);
    if (errorMatch) {
      return {
        id: 'log_' + Date.now().toString(36) + Math.random().toString(36).substr(2, 3),
        time: formatLogTime(errorMatch[1]),
        level: 'error',
        message: errorMatch[2],
        account: extractAccountFromLine(errorMatch[2]),
        model: '--',
        transport: '--',
        latency: '--',
        status: 'error'
      };
    }

    // Format: [2024-01-15 10:30:45] [WARN] Warning message
    const warnMatch = line.match(/\[([^\]]+)\]\s+\[WARN(ING)?\]\s+(.*)/i);
    if (warnMatch) {
      return {
        id: 'log_' + Date.now().toString(36) + Math.random().toString(36).substr(2, 3),
        time: formatLogTime(warnMatch[1]),
        level: 'warning',
        message: warnMatch[3],
        account: extractAccountFromLine(warnMatch[3]),
        model: '--',
        transport: '--',
        latency: '--',
        status: 'warning'
      };
    }

    // Fallback - just show the raw line
    return {
      id: 'log_' + Date.now().toString(36),
      time: 'recent',
      level: 'info',
      message: line.substring(0, 100),
      account: '--',
      model: '--',
      transport: '--',
      latency: '--',
      status: 'success'
    };
  }

  function formatActivityTime(timestamp) {
    if (!timestamp) return 'recent';
    return formatLogTime(timestamp);
  }

  function formatLatency(ms) {
    const value = Number(ms) || 0;
    return value > 0 ? value + 'ms' : '--';
  }

  function normalizeLogStatus(status) {
    const normalized = String(status || '').toLowerCase();
    if (normalized === 'error') return 'error';
    if (normalized === 'warning' || normalized === 'warn') return 'warning';
    if (normalized === 'pending') return 'pending';
    return 'success';
  }

  function formatLogTime(timestamp) {
    try {
      const date = new Date(timestamp);
      if (isNaN(date.getTime())) return timestamp.substring(5, 16);
      const now = new Date();
      const diff = Math.floor((now - date) / 1000);
      if (diff < 60) return diff + 's ago';
      if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
      if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
      return timestamp.substring(5, 16);
    } catch {
      return timestamp.substring(5, 16);
    }
  }

  function extractAccountFromLine(line) {
    // Try to extract account/auth file name
    const match = line.match(/(?:account|auth|for|using)\s+[\'""]?([^\s\'\"\",]+)/i);
    if (match) return match[1].substring(0, 20);
    return '--';
  }

  function extractModelFromLine(line) {
    const match = line.match(/(?:model|using)\s+([a-zA-Z0-9\-_.]+(?:-\d+)?(?:-\d{4})?)/i);
    if (match) return match[1];
    return '--';
  }

  function extractLatencyFromLine(line) {
    const match = line.match(/(\d+)\s*ms/i);
    if (match) return match[1] + 'ms';
    return '--';
  }

  function renderEmptyLogs() {
    const tbody = document.getElementById('logs-body');
    if (!tbody) return;
    tbody.innerHTML = `
      <tr>
        <td colspan="7" style="text-align: center; padding: 40px; color: var(--text-muted);">
          No log entries yet. Make some API requests to see them here.
        </td>
      </tr>
    `;
  }

  // Render mock logs for demo
  function renderMockLogs() {
    const mockLogs = [
      { id: 'req_8f3k2j1h', account: 'alex.research', model: 'claude-3-5-sonnet', transport: 'http', tokens: '4,821', latency: '234ms', status: 'success', time: '2s ago' },
      { id: 'req_7g4l5m6n', account: 'dev.james', model: 'gpt-4o', transport: 'http', tokens: '2,156', latency: '189ms', status: 'success', time: '5s ago' },
      { id: 'req_6h5m6n7o', account: 'sarah.kim', model: 'claude-3-opus', transport: 'websocket', tokens: '8,432', latency: '456ms', status: 'error', time: '12s ago' },
      { id: 'req_5i6n7o8p', account: 'emma.build', model: 'gpt-4-turbo', transport: 'http', tokens: '1,203', latency: '312ms', status: 'success', time: '18s ago' },
      { id: 'req_4j7o8p9q', account: 'alex.research', model: 'gemini-1.5-pro', transport: 'http', tokens: '3,567', latency: '145ms', status: 'pending', time: '24s ago' }
    ];
    state.logs = mockLogs;
    renderLogs(mockLogs);
  }

  function getVisibleLogs(logs, visibleCount) {
    if (!Array.isArray(logs) || logs.length === 0) return [];
    const count = Math.max(0, Number(visibleCount) || 0);
    if (count <= 0) return [];
    return logs.slice(-count);
  }

  function shouldShowOlderLogsControl(logs, visibleCount) {
    if (!Array.isArray(logs) || logs.length === 0) return false;
    return logs.length > (Number(visibleCount) || 0);
  }

  function renderLogsFooter(logs) {
    const footer = document.getElementById('logs-footer');
    if (!footer) return;

    if (!shouldShowOlderLogsControl(logs, state.logVisibleCount)) {
      footer.innerHTML = '';
      return;
    }

    footer.innerHTML = `<button class="btn btn-secondary btn-sm" id="logs-show-older">Show ${LOGS_PAGE_SIZE} older</button>`;

    const button = document.getElementById('logs-show-older');
    if (!button) return;
    button.addEventListener('click', () => {
      state.logVisibleCount += LOGS_PAGE_SIZE;
      filterLogs();
    });
  }

  // Render logs table
  function renderLogs(logs) {
    const tbody = document.getElementById('logs-body');
    if (!tbody) return;

    if (!logs || logs.length === 0) {
      renderEmptyLogs();
      renderLogsFooter([]);
      updateLogsResults(0, 0);
      return;
    }

    const visibleLogs = getVisibleLogs(logs, state.logVisibleCount);

    tbody.innerHTML = visibleLogs.map(log => `
      <tr>
        <td class="log-status-cell ${escapeHtml(log.status)}-row"><span class="log-status ${escapeHtml(log.status)}"><span class="log-status-dot"></span>${capitalize(log.status)}</span></td>
        <td>
          <div class="request-title">${escapeHtml(buildLogRequestTitle(log))}</div>
          <div class="request-message" title="${escapeHtml(log.message)}">${escapeHtml(log.message)}</div>
        </td>
        <td><div class="account-name">${escapeHtml(log.account)}</div></td>
        <td><div class="model-name">${escapeHtml(log.model)}</div></td>
        <td><div class="transport-name">${escapeHtml(log.transport || '--')}</div></td>
        <td><div class="time-value">${escapeHtml(log.time)}</div></td>
        <td><div class="latency-value${isSlowLatency(log.latency) ? ' slow' : ''}">${escapeHtml(log.latency)}</div></td>
      </tr>
    `).join('');

    updateLogsResults(visibleLogs.length, logs.length);
    renderLogsFooter(logs);
  }

  function buildLogRequestTitle(log) {
    const message = String(log && log.message || '').trim();
    const firstSentence = message.split(/[\n.]/)[0].trim();
    if (firstSentence) return firstSentence;
    return log && log.model ? log.model : 'Request';
  }

  function isSlowLatency(latency) {
    const match = String(latency || '').match(/(\d+)/);
    return match ? Number(match[1]) >= 1000 : false;
  }

  function updateLogsResults(visible, total) {
    const result = document.getElementById('logs-results');
    if (!result) return;
    if (!total) {
      result.textContent = '0 events';
      return;
    }
    result.textContent = `${visible} of ${total} events`;
  }

  // Filter logs
  function filterLogs() {
    const search = document.getElementById('logs-search').value.toLowerCase();
    const accountFilter = document.getElementById('logs-account-filter').value;
    const statusFilter = document.getElementById('logs-status-filter').value;
    const filterSignature = JSON.stringify([search, accountFilter, statusFilter]);
    if (lastLogsFilterSignature && lastLogsFilterSignature !== filterSignature) {
      state.logVisibleCount = LOGS_PAGE_SIZE;
    }
    lastLogsFilterSignature = filterSignature;

    let filtered = state.logs;

    if (search) {
      filtered = filtered.filter(log =>
        log.id.toLowerCase().includes(search) ||
        log.model.toLowerCase().includes(search) ||
        log.account.toLowerCase().includes(search) ||
        (log.transport || '').toLowerCase().includes(search) ||
        (log.message || '').toLowerCase().includes(search)
      );
    }

    if (accountFilter) {
      const selectedAccounts = new Set(accountFilter.split(',').map(value => value.trim()).filter(Boolean));
      if (selectedAccounts.size > 0) {
        filtered = filtered.filter(log => selectedAccounts.has(log.account));
      }
    }

    if (statusFilter && statusFilter !== 'All Status') {
      filtered = filtered.filter(log => log.status === statusFilter.toLowerCase());
    }

    renderLogs(filtered);
  }

  // Toggle setting
  async function toggleSetting(toggle) {
    const setting = toggle.dataset.setting;
    const endpoint = toggle.dataset.endpoint;
    const newValue = !toggle.classList.contains('active');

    toggle.classList.toggle('active');

    if (endpoint) {
      try {
        const res = await apiFetch(endpoint, {
          method: 'PUT',
          body: JSON.stringify({ value: newValue })
        });
        if (res && !res.error) {
          showToast('Setting saved', 'success');
        } else {
          toggle.classList.toggle('active'); // revert
          showToast('Failed to save setting', 'error');
        }
      } catch (e) {
        toggle.classList.toggle('active'); // revert
        showToast('Failed to save setting', 'error');
      }
    }
  }

  // Load models for APIs tab
  async function loadModels() {
    const grid = document.getElementById('models-grid');
    if (!grid) return;

    const data = await apiFetch('/v1/models');
    if (!data || !data.data || data.data.length === 0) {
      grid.innerHTML = `
        <div class="empty-state">
          <div class="empty-state-icon">${icons.clock}</div>
          <div class="empty-state-title">No models available</div>
          <div class="empty-state-desc">Add API accounts to see available models.</div>
        </div>
      `;
      return;
    }

    grid.innerHTML = data.data.map(model => `
      <div class="model-card">
        <div class="model-id">${escapeHtml(model.id)}</div>
        <div class="model-meta">${escapeHtml(model.object || 'model')}</div>
      </div>
    `).join('');
  }

  // Load all settings
  async function loadAllSettings() {
    const yamlText = await fetchConfigYAMLText();
    if (!yamlText) return;
    state.settings.configYAML = yamlText;

    Object.entries(CONFIG_TOGGLE_ENDPOINTS).forEach(([endpoint, configRef]) => {
      const toggle = document.querySelector(`[data-endpoint="${endpoint}"]`);
      if (!toggle) return;
      const rawValue = getYAMLValue(yamlText, configRef.path);
      const active = configRef.invert ? !rawValue : !!rawValue;
      toggle.classList.toggle('active', active);
    });

    Object.entries(CONFIG_INPUTS).forEach(([id, configRef]) => {
      if (id === 'input-secret') return;
      const input = document.getElementById(id);
      if (!input) return;
      const value = getYAMLValue(yamlText, configRef.path);
      if (value !== null && value !== undefined) {
        input.value = value;
      }
    });
  }

  // Save a setting on input blur
  async function saveSetting(input) {
    const id = input.id;
    const configRef = CONFIG_INPUTS[id];
    if (!configRef) return;

    const yamlText = await fetchConfigYAMLText();
    if (!yamlText) {
      showToast('Failed to save', 'error');
      return;
    }

    const value = configRef.type === 'int' ? parseInt(input.value, 10) : input.value;
    const updated = setYAMLValue(yamlText, configRef.path, value, configRef.type);
    const ok = await saveConfigYAMLText(updated);
    if (!ok) {
      showToast('Failed to save', 'error');
      return;
    }

    state.settings.configYAML = updated;
    if (configRef.restartRequired) {
      showToast('Saved. Restart required', 'info');
      return;
    }
    showToast('Setting saved', 'success');
  }

  // Save toggle setting
  async function saveToggleSetting(endpoint, value) {
    const configRef = CONFIG_TOGGLE_ENDPOINTS[endpoint];
    if (!configRef) {
      showToast('Failed to save', 'error');
      return;
    }

    const yamlText = await fetchConfigYAMLText();
    if (!yamlText) {
      showToast('Failed to save', 'error');
      return;
    }

    const persistedValue = configRef.invert ? !value : value;
    const updated = setYAMLValue(yamlText, configRef.path, persistedValue, configRef.type);
    const ok = await saveConfigYAMLText(updated);
    if (!ok) {
      showToast('Failed to save', 'error');
      return;
    }

    state.settings.configYAML = updated;
    if (configRef.path[0] === 'host' || configRef.path[0] === 'port' || (configRef.path[0] === 'tls' && configRef.path[1] === 'enable')) {
      showToast('Saved. Restart required', 'info');
      return;
    }
    showToast('Setting saved', 'success');
  }

  // Helpers
  function getQuotaColor(remainingPercent) {
    const r = isNaN(remainingPercent) ? 100 : Math.max(0, Math.min(100, remainingPercent));
    if (r <= 0) return '#ef4444';
    if (r <= 20) return '#f97316';
    if (r <= 50) return '#eab308';
    return '#22c55e';
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function formatNumber(num) {
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
    return num.toString();
  }

  function formatTokens(tokens) {
    if (tokens >= 1000000) return (tokens / 1000000).toFixed(1) + 'M';
    if (tokens >= 1000) return (tokens / 1000).toFixed(1) + 'K';
    return tokens.toString();
  }

  function capitalize(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
  }

  function setLogVisibleCount(value) {
    state.logVisibleCount = value;
    lastLogsFilterSignature = '';
  }

  function setLogsForTest(logs) {
    state.logs = logs;
    lastLogsFilterSignature = '';
  }

  function setAccountsForTest(accounts) {
    state.accounts = accounts;
  }

  function resetDashboardStateForTest() {
    refreshInFlight = false;
    lastRefreshCompletedAt = 0;
    state.quotaStartupSync = null;
    state.quotaSummary = null;
    state.accounts = [];
    state.usage = null;
    state.logs = [];
    state.logVisibleCount = LOGS_PAGE_SIZE;
    lastLogsFilterSignature = '';
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
      deriveAccountStatus,
      normalizeActivityEntries,
      getVisibleLogs,
      shouldShowOlderLogsControl,
      renderLogs,
      filterLogs,
      updateAccountFilters,
      openLogsAccountMenu,
      dismissLogsAccountMenu,
      setLogVisibleCount,
      setLogsForTest,
      setAccountsForTest,
      resetDashboardStateForTest,
      handleRefresh,
      computeQuotaSummaryFromQuotas,
      resolveAccountUsage,
      buildDashboardUsage,
      updateQuotaRings,
      getRemainingQuotaDisplay,
      getInitialSummaryWindow,
      getSummaryWindowValue,
      summaryWindowLabel,
      renderSummaryCards,
      setSummaryWindow,
    };
  }

  // Start
  if (typeof document !== 'undefined') {
    document.addEventListener('DOMContentLoaded', init);
  }
})();
