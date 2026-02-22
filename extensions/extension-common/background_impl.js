(function (root) {
  'use strict';

  function RewindBackgroundMain(adapter, common) {
    const ext = adapter.ext;

    const onInstalled = ext?.runtime?.onInstalled;
    const onStartup = ext?.runtime?.onStartup;

    if (onInstalled?.addListener) {
      onInstalled.addListener(async () => {
        await common.ensureStatusAlarm(adapter);
        await common.updateBadge(adapter);
      });
    }

    if (onStartup?.addListener) {
      onStartup.addListener(async () => {
        await common.ensureStatusAlarm(adapter);
        await common.updateBadge(adapter);
      });
    }

    if (ext?.alarms?.onAlarm?.addListener) {
      ext.alarms.onAlarm.addListener(async (alarm) => {
        if (alarm?.name === common.ALARM_NAME) {
          await common.updateBadge(adapter);
        }
      });
    }

    if (ext?.storage?.onChanged?.addListener) {
      ext.storage.onChanged.addListener(async (changes, area) => {
        if (area !== 'local') return;
        if (changes?.[common.STORAGE_KEYS.serverUrl] || changes?.[common.STORAGE_KEYS.authToken]) {
          await common.updateBadge(adapter);
        }
      });
    }

    if (ext?.runtime?.onMessage?.addListener) {
      ext.runtime.onMessage.addListener((message, sender, sendResponse) => {
        (async () => {
          if (message?.type === 'badge:update') {
            await common.updateBadge(adapter);
            sendResponse?.({ ok: true });
            return;
          }
          sendResponse?.({ ok: false });
        })();
        return true;
      });
    }

    // Ensure we start polling even if install/startup events didn't fire (manual reload, etc.)
    common.ensureStatusAlarm(adapter).catch(() => {});
    common.updateBadge(adapter).catch(() => {});
  }

  root.RewindBackgroundMain = RewindBackgroundMain;
})(typeof window !== 'undefined' ? window : self);
