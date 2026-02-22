// Firefox background script (MV2)
// Loads the shared implementation + adapter, then starts listeners.
importScripts('adapter.js', 'common/rewind_common.js', 'common/background_impl.js');
RewindBackgroundMain(RewindAdapter, RewindCommon);
