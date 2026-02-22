(function (root) {
  'use strict';

  function showError(els, msg) {
    els.error.style.display = 'block';
    els.error.textContent = msg;
  }

  function hideError(els) {
    els.error.style.display = 'none';
    els.error.textContent = '';
  }

  async function refresh(adapter, common, els) {
    hideError(els);

    const tabUrl = await adapter.getActiveTabUrl();
    const siteKey = common.siteKeyFromUrl(tabUrl);

    const { serverUrl, authToken, cookiesEnabledBySite } = await adapter.storageGet([
      common.STORAGE_KEYS.serverUrl,
      common.STORAGE_KEYS.authToken,
      common.STORAGE_KEYS.cookiesEnabledBySite
    ]);

    els.site.textContent = siteKey || '—';

    const enabledMap = cookiesEnabledBySite && typeof cookiesEnabledBySite === 'object' ? cookiesEnabledBySite : {};
    const enabledForSite = siteKey ? Boolean(enabledMap[siteKey]) : false;
    els.sendCookies.checked = enabledForSite;
    els.sendCookies.disabled = !siteKey;
    els.cookieCount.textContent = '—';

    els.server.textContent = serverUrl ? new URL(serverUrl).host : 'Not configured';

    if (!serverUrl) {
      els.auth.textContent = 'Not configured';
      els.jobs.textContent = '—';
      els.archiveBtn.disabled = true;
      els.loginBtn.style.display = 'none';
      return;
    }

    if (!authToken) {
      els.auth.textContent = 'Not logged in';
      els.jobs.textContent = '—';
      els.archiveBtn.disabled = true;
      els.loginBtn.style.display = 'block';
      return;
    }

    els.loginBtn.style.display = 'none';

    try {
      const data = await common.getStatus({ serverUrl, authToken, siteKey });

      if (!data?.authenticated) {
        els.auth.textContent = 'Not logged in';
        els.jobs.textContent = '—';
        els.archiveBtn.disabled = true;
        els.loginBtn.style.display = 'block';
        return;
      }

      els.auth.textContent = data?.user?.username ? `Logged in as ${data.user.username}` : 'Logged in';
      els.jobs.textContent = String(data?.running_jobs ?? 0);
      els.cookieCount.textContent = String(data?.cookie_count ?? 0);
      els.archiveBtn.disabled = false;
    } catch (e) {
      els.archiveBtn.disabled = true;
      showError(els, String(e?.message ?? e));
    }
  }

  async function uploadCookiesForUrl(adapter, common, { serverUrl, authToken, url }) {
    let cookies;
    try {
      cookies = await adapter.cookiesGetAll({ url });
    } catch (e) {
      throw new Error(`Failed to read cookies (permission?): ${String(e?.message ?? e)}`);
    }

    const cookiesContent = common.cookiesToNetscape(cookies);
    if (!cookiesContent) {
      throw new Error('No cookies found for this URL');
    }

    await common.uploadCookiesContent({ serverUrl, authToken, cookiesContent });
  }

  async function handleArchive(adapter, common, els) {
    hideError(els);

    const { serverUrl, authToken, cookiesEnabledBySite } = await adapter.storageGet([
      common.STORAGE_KEYS.serverUrl,
      common.STORAGE_KEYS.authToken,
      common.STORAGE_KEYS.cookiesEnabledBySite
    ]);

    if (!serverUrl || !authToken) {
      showError(els, 'Missing server or auth token');
      return;
    }

    const url = await adapter.getActiveTabUrl();
    if (!url) {
      showError(els, 'Could not read current tab URL');
      return;
    }

    const siteKey = common.siteKeyFromUrl(url);
    const enabledMap = cookiesEnabledBySite && typeof cookiesEnabledBySite === 'object' ? cookiesEnabledBySite : {};
    const sendCookies = siteKey ? Boolean(enabledMap[siteKey]) : false;

    els.archiveBtn.disabled = true;

    try {
      if (sendCookies) {
        await uploadCookiesForUrl(adapter, common, { serverUrl, authToken, url });
      }

      const data = await common.archiveUrl({ serverUrl, authToken, url });
      const redirect = data?.redirect;

      if (redirect) {
        const u = new URL(serverUrl);
        const full = `${u.origin}${redirect}`;
        await adapter.tabsCreate(full);
      }

      adapter.sendMessage?.({ type: 'badge:update' }).catch(() => {});
      window.close();
    } catch (e) {
      showError(els, String(e?.message ?? e));
      els.archiveBtn.disabled = false;
    }
  }

  async function handleToggleCookies(adapter, common, els) {
    hideError(els);

    const tabUrl = await adapter.getActiveTabUrl();
    const siteKey = common.siteKeyFromUrl(tabUrl);

    if (!siteKey) {
      els.sendCookies.checked = false;
      return;
    }

    const { cookiesEnabledBySite } = await adapter.storageGet([common.STORAGE_KEYS.cookiesEnabledBySite]);
    const enabledMap = cookiesEnabledBySite && typeof cookiesEnabledBySite === 'object' ? cookiesEnabledBySite : {};

    if (els.sendCookies.checked) {
      try {
        const granted = await adapter.permissionsRequest({
          permissions: ['cookies'],
          origins: common.originsForSiteKey(siteKey)
        });

        if (!granted) {
          els.sendCookies.checked = false;
          showError(els, 'Cookie permission denied');
          return;
        }

        enabledMap[siteKey] = true;
        await adapter.storageSet({ [common.STORAGE_KEYS.cookiesEnabledBySite]: enabledMap });
        await refresh(adapter, common, els);
      } catch (e) {
        els.sendCookies.checked = false;
        showError(els, String(e?.message ?? e));
      }
      return;
    }

    delete enabledMap[siteKey];
    await adapter.storageSet({ [common.STORAGE_KEYS.cookiesEnabledBySite]: enabledMap });
    await refresh(adapter, common, els);
  }

  function RewindPopupMain(adapter, common) {
    const els = {
      server: document.getElementById('server'),
      auth: document.getElementById('auth'),
      jobs: document.getElementById('jobs'),
      site: document.getElementById('site'),
      sendCookies: document.getElementById('sendCookies'),
      cookieCount: document.getElementById('cookieCount'),
      archiveBtn: document.getElementById('archiveBtn'),
      loginBtn: document.getElementById('loginBtn'),
      settingsBtn: document.getElementById('settingsBtn'),
      error: document.getElementById('error')
    };

    document.addEventListener('DOMContentLoaded', async () => {
      els.settingsBtn.addEventListener('click', () => adapter.openOptionsPage());
      els.archiveBtn.addEventListener('click', () => handleArchive(adapter, common, els));
      els.loginBtn.addEventListener('click', () => adapter.openOptionsPage());
      els.sendCookies.addEventListener('change', () => handleToggleCookies(adapter, common, els));

      await refresh(adapter, common, els);
    });
  }

  root.RewindPopupMain = RewindPopupMain;
})(typeof window !== 'undefined' ? window : self);
