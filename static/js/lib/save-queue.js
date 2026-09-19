// Debounced writes are serialized; edits made during a save remain queued.
export class SaveQueue {
  constructor(write, state = () => {}, delay = 800) {
    this.write = write;
    this.state = state;
    this.delay = delay;
    this.pending = null;
    this.running = null;
    this.latest = null;
  }
  schedule(value) {
    const snapshot = JSON.stringify(value);
    if (snapshot === this.latest) return;
    this.latest = snapshot;
    this.pending = snapshot;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.flush().catch(() => {}), this.delay);
  }
  get dirty() { return !!(this.pending || this.running); }
  async flush() {
    clearTimeout(this.timer);
    if (this.running) return this.running;
    if (!this.pending) return;
    this.running = Promise.resolve().then(async () => {
      this.state(true, true);
      while (this.pending) {
        const snapshot = this.pending;
        this.pending = null;
        try { await this.write(JSON.parse(snapshot)); }
        catch (error) { this.pending ||= snapshot; throw error; }
      }
    });
    try { await this.running; }
    finally { this.running = null; this.state(false, !!this.pending); }
  }
  dispose() { clearTimeout(this.timer); }
}
