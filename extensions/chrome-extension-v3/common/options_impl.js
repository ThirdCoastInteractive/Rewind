(function (root) {
  'use strict';

  function showMsg(els, text, ok) {
    els.saveMsg.style.display = 'block';
    els.saveMsg.textContent = text;
    els.saveMsg.style.borderColor = ok ? 'rgba(255,255,255,0.6)' : 'rgba(220,38,38,0.7)';
  }

  async function updateStatus(adapter, common, els) {
    const { serverUrl, authToken } = await adapter.storageGet([common.STORAGE_KEYS.serverUrl, common.STORAGE_KEYS.authToken]);

    if (!serverUrl) {
      els.status.innerHTML = '<span class="bad">Not configured</span>';
      return;
    }

    if (!authToken) {
      els.status.innerHTML = `<span class="bad">No token</span> (server: ${serverUrl})`;
      return;
    }

    try {
      const data = await common.getStatus({ serverUrl, authToken });
      const user = data?.user?.username ? ` as ${data.user.username}` : '';
      const jobs = Number(data?.running_jobs ?? 0);
      els.status.innerHTML = `<span class="ok">OK</span> authenticated${user}, jobs=${jobs}`;
    } catch (e) {
      els.status.innerHTML = `<span class="bad">Error</span> ${String(e?.message ?? e)}`;
    }
  }

  async function handleSave(adapter, common, els) {
    let normalized;
    try {
      normalized = common.normalizeServerUrl(els.serverUrl.value);
    } catch (e) {
      showMsg(els, String(e?.message ?? e), false);
      return;
    }

    try {
      const granted = await adapter.permissionsRequest({
        permissions: [],
        origins: [`${normalized}/*`]
      });

      if (!granted) {
        showMsg(els, 'Permission denied: extension cannot call the server', false);
        return;
      }

      await adapter.storageSet({ [common.STORAGE_KEYS.serverUrl]: normalized });
      showMsg(els, `Saved server: ${normalized}`, true);

      adapter.sendMessage?.({ type: 'badge:update' }).catch(() => {});
      await updateStatus(adapter, common, els);
    } catch (e) {
      showMsg(els, `Save failed: ${String(e?.message ?? e)}`, false);
    }
  }

  async function handleLogin(adapter, common, els) {
    const { serverUrl } = await adapter.storageGet([common.STORAGE_KEYS.serverUrl]);
    if (!serverUrl) {
      showMsg(els, 'Save a server URL first', false);
      return;
    }

    const redirectUrl = adapter.getRedirectURL('callback');
    const stateHex = common.randomHex(16);

    const authUrl = common.buildAuthStartUrl({
      serverUrl,
      clientId: adapter.runtimeId(),
      redirectUrl,
      state: stateHex
    });

    try {
      const responseUrl = await adapter.launchWebAuthFlow({ url: authUrl, interactive: true });
      const token = common.parseAuthResponseUrl({ responseUrl, expectedState: stateHex });

      await adapter.storageSet({ [common.STORAGE_KEYS.authToken]: token });
      showMsg(els, 'Login successful; token saved.', true);

      adapter.sendMessage?.({ type: 'badge:update' }).catch(() => {});
      await updateStatus(adapter, common, els);
    } catch (e) {
      showMsg(els, `Login failed: ${String(e?.message ?? e)}`, false);
    }
  }

  async function handleLogout(adapter, common, els) {
    const { serverUrl, authToken } = await adapter.storageGet([common.STORAGE_KEYS.serverUrl, common.STORAGE_KEYS.authToken]);

    if (authToken && serverUrl) {
      await common.logout({ serverUrl, authToken });
    }

    await adapter.storageRemove([common.STORAGE_KEYS.authToken]);
    showMsg(els, 'Logged out; token cleared.', true);
    adapter.sendMessage?.({ type: 'badge:update' }).catch(() => {});
    await updateStatus(adapter, common, els);
  }

  function RewindOptionsMain(adapter, common) {
    const els = {
      serverUrl: document.getElementById('serverUrl'),
      saveBtn: document.getElementById('saveBtn'),
      testBtn: document.getElementById('testBtn'),
      loginBtn: document.getElementById('loginBtn'),
      logoutBtn: document.getElementById('logoutBtn'),
      status: document.getElementById('status'),
      saveMsg: document.getElementById('saveMsg')
    };

    document.addEventListener('DOMContentLoaded', async () => {
      const { serverUrl } = await adapter.storageGet([common.STORAGE_KEYS.serverUrl]);
      if (serverUrl) els.serverUrl.value = serverUrl;

      els.saveBtn.addEventListener('click', () => handleSave(adapter, common, els));
      els.testBtn.addEventListener('click', () => updateStatus(adapter, common, els));
      els.loginBtn.addEventListener('click', () => handleLogin(adapter, common, els));
      els.logoutBtn.addEventListener('click', () => handleLogout(adapter, common, els));

      await updateStatus(adapter, common, els);
    });
  }

  root.RewindOptionsMain = RewindOptionsMain;
})(typeof window !== 'undefined' ? window : self);
