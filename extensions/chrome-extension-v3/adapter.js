(function (root) {
  'use strict';

  const ext = chrome;

  function fromCallback(fn, ...args) {
    return new Promise((resolve, reject) => {
      fn(...args, (result) => {
        const err = ext.runtime?.lastError;
        if (err) {
          reject(new Error(err.message || String(err)));
          return;
        }
        resolve(result);
      });
    });
  }

  const adapter = {
    ext,

    runtimeId() {
      return ext.runtime.id;
    },

    getRedirectURL(path) {
      return ext.identity.getRedirectURL(path);
    },

    storageGet(keys) {
      return fromCallback(ext.storage.local.get.bind(ext.storage.local), keys);
    },

    storageSet(obj) {
      return fromCallback(ext.storage.local.set.bind(ext.storage.local), obj);
    },

    storageRemove(keys) {
      return fromCallback(ext.storage.local.remove.bind(ext.storage.local), keys);
    },

    async getActiveTabUrl() {
      const tabs = await fromCallback(ext.tabs.query.bind(ext.tabs), { active: true, currentWindow: true });
      return tabs?.[0]?.url || '';
    },

    tabsCreate(url) {
      return fromCallback(ext.tabs.create.bind(ext.tabs), { url });
    },

    openOptionsPage() {
      // no callback in most cases
      return Promise.resolve(ext.runtime.openOptionsPage());
    },

    sendMessage(message) {
      // callback style to catch runtime.lastError reliably
      return fromCallback(ext.runtime.sendMessage.bind(ext.runtime), message);
    },

    permissionsRequest({ permissions, origins }) {
      return fromCallback(ext.permissions.request.bind(ext.permissions), {
        permissions,
        origins
      });
    },

    launchWebAuthFlow({ url, interactive }) {
      return fromCallback(ext.identity.launchWebAuthFlow.bind(ext.identity), {
        url,
        interactive
      });
    },

    cookiesGetAll({ url }) {
      return fromCallback(ext.cookies.getAll.bind(ext.cookies), { url });
    },

    async alarmsEnsure({ name, periodMinutes }) {
      const alarm = await fromCallback(ext.alarms.get.bind(ext.alarms), name);
      if (!alarm) {
        ext.alarms.create(name, { periodInMinutes: periodMinutes });
      }
    },

    badgeSet({ color, text }) {
      ext.action.setBadgeBackgroundColor({ color });
      ext.action.setBadgeText({ text });
      return Promise.resolve();
    }
  };

  root.RewindAdapter = adapter;
})(typeof window !== 'undefined' ? window : self);
