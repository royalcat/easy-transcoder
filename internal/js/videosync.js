(function () {
  "use strict";

  document.addEventListener("alpine:init", () => {
    Alpine.data("videoSplitCompare", () => ({
      position: 50,     // percentage from left (0 = all transcoded, 100 = all original)
      dragging: false,
      _master: null,
      _seeking: false,

      // --- Slider drag ---
      onDragStart() {
        this.dragging = true;
      },

      onDrag(e) {
        if (!this.dragging) return;
        const rect = this.$refs.container.getBoundingClientRect();
        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        let pct = ((clientX - rect.left) / rect.width) * 100;
        this.position = Math.max(5, Math.min(95, pct));
      },

      onDragEnd() {
        this.dragging = false;
      },

      // --- Video sync ---
      onPlay(src) {
        const other = this._other(src);
        if (!other) return;
        if (other.paused) {
          other.play().catch(() => {});
        }
      },

      onPause(src) {
        const other = this._other(src);
        if (!other) return;
        if (!other.paused) other.pause();
      },

      onSeek(src) {
        if (this._seeking) return;
        this._master = src;
        const other = this._other(src);
        if (!other) return;
        this._seeking = true;
        other.currentTime = src.currentTime;
      },

      onSeeked(src) {
        if (this._master === src) {
          this._seeking = false;
          this._master = null;
        }
      },

      _other(src) {
        if (!this.$refs.videoA || !this.$refs.videoB) return null;
        return src === this.$refs.videoA ? this.$refs.videoB : this.$refs.videoA;
      },
    }));
  });
})();
