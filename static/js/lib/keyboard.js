import { isFiniteNumber } from './utils.js';

/**
 * Controls – keyboard shortcut handling for the cut-page editor.
 *
 * Instantiated in CutPageEditor constructor: `this.controls = new Controls(this);`
 * Wired via: `document.addEventListener('keydown', e => this.controls.handleKeyDown(e))`
 */
export class Controls {
  constructor(editor) {
    this.editor = editor;
  }

  handleKeyDown(e) {
    const ed = this.editor;
    // Skip if focused on an input
    if (
      e.target?.isContentEditable ||
      e.target?.tagName === 'INPUT' ||
      e.target?.tagName === 'TEXTAREA' ||
      e.target?.tagName === 'SELECT'
    ) {
      return;
    }

    const key = e.key;
    const lowerKey = key.toLowerCase();
    const hasModifier = e.ctrlKey || e.metaKey || e.altKey;

    // Media keys
    if (key === 'MediaPlayPause') {
      e.preventDefault();
      ed.transportTogglePlay();
      return;
    }
    if (key === 'MediaTrackPrevious') {
      e.preventDefault();
      ed.seekRelative(-10);
      return;
    }
    if (key === 'MediaTrackNext') {
      e.preventDefault();
      ed.seekRelative(10);
      return;
    }

    // User-configurable keybindings (no modifiers)
    if (!hasModifier) {
      const action = ed.keyMap[key];
      if (action) {
        e.preventDefault();
        this.executeKeybindingAction(action);
        return;
      }
    }

    // Space or K - toggle play/pause
    if ((lowerKey === ' ' || lowerKey === 'k') && !hasModifier) {
      e.preventDefault();
      ed.transportTogglePlay();
      return;
    }

    // Arrow left - seek backward 5s
    if (lowerKey === 'arrowleft' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      ed.seekRelative(-5);
      return;
    }

    // Arrow right - seek forward 5s
    if (lowerKey === 'arrowright' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      ed.seekRelative(5);
      return;
    }

    // Comma - previous frame (when paused)
    if (lowerKey === ',' && ed.video?.paused && !hasModifier) {
      e.preventDefault();
      ed.transportPrevFrame();
      return;
    }

    // Period - next frame (when paused)
    if (lowerKey === '.' && ed.video?.paused && !hasModifier) {
      e.preventDefault();
      ed.transportNextFrame();
      return;
    }

    // I - set in point
    if (lowerKey === 'i' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      ed.btnSetIn?.click();
      return;
    }

    // O - set out point
    if (lowerKey === 'o' && !e.shiftKey && !hasModifier) {
      e.preventDefault();
      ed.btnSetOut?.click();
      return;
    }

    // Home - go to start
    if (lowerKey === 'home' && !hasModifier) {
      e.preventDefault();
      ed.transportGoToStart();
      return;
    }

    // End - go to end
    if (lowerKey === 'end' && !hasModifier) {
      e.preventDefault();
      ed.transportGoToEnd();
      return;
    }

    // E - toggle edit mode on selected clip
    if (lowerKey === 'e' && !e.shiftKey && !hasModifier) {
      if (ed.selectedClipId) {
        e.preventDefault();
        if (ed.editMode) {
          ed.exitEditMode();
        } else {
          ed.enterEditMode();
        }
        return;
      }
    }

    // S - split clip at playhead (edit mode)
    if (lowerKey === 's' && !e.shiftKey && !hasModifier) {
      if (ed.editMode && ed.selectedClipId) {
        e.preventDefault();
        ed.splitClipAtPlayhead();
        return;
      }
    }

    // Escape - exit edit mode (or deselect clip)
    if (lowerKey === 'escape' && !hasModifier) {
      e.preventDefault();
      if (ed.editMode) {
        ed.exitEditMode();
      } else if (ed.selectedClipId) {
        ed.clearSelectedClip();
      }
      return;
    }

    // Selection nudge shortcuts (edit mode only)
    const frameDur = ed.videoFps > 0 ? 1 / ed.videoFps : 1 / 30;
    if (ed.editMode && ed.selectedClipId) {
      // Shift+, / Shift+. - nudge in point by 1 frame
      if (lowerKey === ',' && e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeInPoint(-frameDur);
        return;
      }
      if (lowerKey === '.' && e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeInPoint(frameDur);
        return;
      }
      // Alt+, / Alt+. - nudge out point by 1 frame
      if (lowerKey === ',' && e.altKey && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeOutPoint(-frameDur);
        return;
      }
      if (lowerKey === '.' && e.altKey && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeOutPoint(frameDur);
        return;
      }
      // Shift+Alt+, / Shift+Alt+. - nudge entire selection by 1 frame
      if (lowerKey === ',' && e.shiftKey && e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeSelection(-frameDur);
        return;
      }
      if (lowerKey === '.' && e.shiftKey && e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        ed.nudgeSelection(frameDur);
        return;
      }
    }

    // + or = - zoom in work window (no modifier)
    if ((lowerKey === '+' || lowerKey === '=') && !hasModifier) {
      e.preventDefault();
      ed.zoomWorkWindow(0.8);
      return;
    }

    // - - zoom out work window (no modifier)
    if (lowerKey === '-' && !hasModifier) {
      e.preventDefault();
      ed.zoomWorkWindow(1.25);
      return;
    }

    // Ctrl++ / Ctrl+= - zoom in overview
    if ((lowerKey === '+' || lowerKey === '=') && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      const center = (ed.overviewStart + ed.overviewEnd) / 2;
      ed.zoomOverview(0.8, center);
      return;
    }

    // Ctrl+- - zoom out overview
    if (lowerKey === '-' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      const center = (ed.overviewStart + ed.overviewEnd) / 2;
      ed.zoomOverview(1.25, center);
      return;
    }

    // Ctrl+0 - reset both overview and work window to full duration
    if (lowerKey === '0' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      if (isFiniteNumber(ed.duration) && ed.duration > 0) {
        ed.workStart = 0;
        ed.workEnd = ed.duration;
        ed.resetOverviewZoom();
        ed.render();
      }
      return;
    }

    // 0-9 - seek to percentage
    if (/^[0-9]$/.test(lowerKey) && isFiniteNumber(ed.duration) && !hasModifier) {
      e.preventDefault();
      const percent = parseInt(lowerKey) * 10;
      const time = (percent / 100) * ed.duration;
      if (ed.video) {
        ed.video.currentTime = time;
        ed.workHeadTime = time;
        ed.renderPlayheads();
        ed.updateTransportTime();
      }
    }
  }

  executeKeybindingAction(action) {
    const ed = this.editor;
    switch (action) {
      case 'set_in_point':
        ed.btnSetIn?.click();
        break;
      case 'set_out_point':
        ed.btnSetOut?.click();
        break;
      case 'create_clip':
        ed.createClipFromRange();
        break;
      case 'play_pause':
        ed.transportTogglePlay();
        break;
      case 'seek_back':
        ed.seekRelative(-10);
        break;
      case 'seek_forward':
        ed.seekRelative(10);
        break;
      case 'prev_frame':
        ed.transportPrevFrame();
        break;
      case 'next_frame':
        ed.transportNextFrame();
        break;
      default:
        break;
    }
  }
}
