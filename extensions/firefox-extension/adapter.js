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

    async getActiveTabUrl() {
      const tabs = await ext.tabs.query({ active: true, currentWindow: true });
      return tabs?.[0]?.url || '';
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
