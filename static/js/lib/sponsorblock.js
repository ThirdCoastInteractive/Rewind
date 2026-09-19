// Only explicit SponsorBlock skip actions may seek playback automatically.
// Chapters, mute actions, and archive markers remain navigation annotations.
export function isSkipSegment(marker) {
  return marker.source === 'sponsorblock' &&
    marker.action_type === 'skip' &&
    marker.marker_type !== 'chapter' && marker.category !== 'chapter' &&
    Number.isFinite(marker.timestamp) && marker.timestamp >= 0 &&
    Number.isFinite(marker.duration) && marker.duration > 0;
}
