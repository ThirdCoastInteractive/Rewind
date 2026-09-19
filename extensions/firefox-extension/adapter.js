(function (root) {
  'use strict';

  const ext = browser;

  const adapter = {
    ext,

    runtimeId() {
      return ext.runtime.id;
    },

    getRedirectURL(path) {
      return ext.identity.getRedirectURL(path);
    },

    storageGet(keys) {
      return ext.storage.local.get(keys);
    },

    storageSet(obj) {
      return ext.storage.local.set(obj);
    },

    storageRemove(keys) {
      return ext.storage.local.remove(keys);
    },

    async getActiveTab() {
      const tabs = await ext.tabs.query({ active: true, currentWindow: true });
      const tab = tabs?.[0];
      if (!tab) return null;
      return { id: tab.id, url: tab.url || '' };
    },

    async getActiveTabUrl() {
      const tab = await this.getActiveTab();
      return tab?.url || '';
    },

    async executeScript({ tabId, func }) {
      // MV2: inject via code string (function injection is MV3-only).
      const code = '(' + Function.prototype.toString.call(func) + ')()';
      const results = await ext.tabs.executeScript(tabId, { code });
      return results?.[0];
    },

    tabsCreate(url) {
      return ext.tabs.create({ url });
    },

    openOptionsPage() {
      return ext.runtime.openOptionsPage();
    },

    sendMessage(message) {
      return ext.runtime.sendMessage(message);
    },

    permissionsRequest({ permissions, origins }) {
      return ext.permissions.request({ permissions, origins });
    },

    launchWebAuthFlow({ url, interactive }) {
      return ext.identity.launchWebAuthFlow({ url, interactive });
    },

    cookiesGetAll({ url }) {
      return ext.cookies.getAll({ url });
    },

    async alarmsEnsure({ name, periodMinutes }) {
      const alarm = await ext.alarms.get(name);
      if (!alarm) {
        ext.alarms.create(name, { periodInMinutes: periodMinutes });
      }
    },

    badgeSet({ color, text }) {
      const actionApi = ext.action || ext.browserAction;
      actionApi.setBadgeBackgroundColor({ color });
      actionApi.setBadgeText({ text });
      return Promise.resolve();
    }
  };

  root.RewindAdapter = adapter;
})(typeof window !== 'undefined' ? window : self);
