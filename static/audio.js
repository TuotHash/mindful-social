// TTS podcast-style player for nodes. Renders behind any
// data-tts-player element: fetches the chunk manifest, plays them in
// order through a single <audio> tag, and prefetches the next chunk
// while the current one is playing so the swap feels gapless. Long
// posts only have the first chunk pre-synthesized; reaching the end
// triggers on-demand generation server-side, and we poll the manifest
// until the next chunk's ready flag flips.

(function () {
    'use strict';

    const POLL_INTERVAL_MS = 2500;
    const POLL_TIMEOUT_MS = 90_000;

    document.addEventListener('DOMContentLoaded', () => {
        document.querySelectorAll('[data-tts-player]').forEach(initPlayer);
    });

    function initPlayer(root) {
        const nodeID = root.dataset.ttsPlayer;
        const manifestURL = `/nodes/${nodeID}/audio/manifest`;

        const audio = root.querySelector('audio');
        const playBtn = root.querySelector('[data-tts-action="play"]');
        const progress = root.querySelector('[data-tts-progress]');
        const positionEl = root.querySelector('[data-tts-position]');
        const durationEl = root.querySelector('[data-tts-duration]');
        const statusEl = root.querySelector('[data-tts-status]');
        const langEl = root.querySelector('[data-tts-lang]');

        let manifest = null;
        let chunkIndex = 0;
        let totalEstimatedMs = 0;
        let chunkOffsetMs = 0; // ms played before the current chunk started

        loadManifest()
            .then(m => {
                if (!m || m.total_chunks === 0) {
                    root.hidden = true;
                    return;
                }
                manifest = m;
                totalEstimatedMs = m.estimated_total_ms;
                durationEl.textContent = formatTime(totalEstimatedMs);
                if (langEl) langEl.textContent = (m.language || '').toUpperCase();
                root.removeAttribute('aria-busy');
            })
            .catch(err => {
                console.error('tts: load manifest', err);
                root.hidden = true;
            });

        playBtn.addEventListener('click', () => {
            if (!manifest) return;
            if (audio.paused) {
                if (!audio.src) loadChunk(0);
                audio.play().catch(err => console.warn('tts: play', err));
            } else {
                audio.pause();
            }
        });

        audio.addEventListener('play', () => setIcon(playBtn, 'pause'));
        audio.addEventListener('pause', () => setIcon(playBtn, 'play_arrow'));

        audio.addEventListener('timeupdate', () => {
            const playedMs = chunkOffsetMs + Math.round(audio.currentTime * 1000);
            positionEl.textContent = formatTime(playedMs);
            if (totalEstimatedMs > 0) {
                progress.value = Math.min(100, (playedMs / totalEstimatedMs) * 100);
            }
            // Prefetch next chunk slightly before this one ends, so its
            // src is hot when we swap. 5s lead-time is enough for the
            // sidecar to finish a typical chunk on a quiet CPU.
            const remaining = audio.duration - audio.currentTime;
            if (remaining < 5 && manifest && chunkIndex + 1 < manifest.total_chunks) {
                ensureChunkReady(chunkIndex + 1);
            }
        });

        audio.addEventListener('ended', () => {
            if (!manifest) return;
            chunkOffsetMs += manifest.chunks[chunkIndex].duration_ms;
            if (chunkIndex + 1 >= manifest.total_chunks) {
                setStatus('Finished.');
                progress.value = 100;
                positionEl.textContent = formatTime(totalEstimatedMs);
                return;
            }
            const next = chunkIndex + 1;
            ensureChunkReady(next).then(() => {
                chunkIndex = next;
                loadChunk(next);
                audio.play().catch(err => console.warn('tts: autoplay next', err));
            });
        });

        function loadChunk(i) {
            const url = manifest.chunks[i].url;
            audio.src = url;
            audio.load();
        }

        function loadManifest() {
            return fetch(manifestURL, { credentials: 'same-origin' }).then(r => {
                if (!r.ok) throw new Error('manifest ' + r.status);
                return r.json();
            });
        }

        function ensureChunkReady(i) {
            if (manifest.chunks[i] && manifest.chunks[i].ready) {
                return Promise.resolve();
            }
            setStatus('Generating next chunk…');
            // Hit the chunk URL once to trigger on-demand enqueue, then
            // poll the manifest until ready flips.
            return fetch(manifest.chunks[i].url, { method: 'HEAD', credentials: 'same-origin' })
                .catch(() => {})
                .then(() => pollUntilReady(i));
        }

        function pollUntilReady(i) {
            const deadline = Date.now() + POLL_TIMEOUT_MS;
            return new Promise((resolve, reject) => {
                const tick = () => {
                    loadManifest().then(m => {
                        manifest = m;
                        if (m.chunks[i] && m.chunks[i].ready) {
                            setStatus('');
                            resolve();
                            return;
                        }
                        if (Date.now() > deadline) {
                            setStatus('Timed out waiting for audio.');
                            reject(new Error('poll timeout'));
                            return;
                        }
                        setTimeout(tick, POLL_INTERVAL_MS);
                    }).catch(err => {
                        setStatus('Error checking audio.');
                        reject(err);
                    });
                };
                tick();
            });
        }

        function setStatus(msg) {
            if (statusEl) statusEl.textContent = msg;
        }
    }

    function setIcon(btn, icon) {
        const span = btn.querySelector('.material-symbols-outlined');
        if (span) span.textContent = icon;
    }

    function formatTime(ms) {
        if (!ms || ms < 0) return '0:00';
        const totalSec = Math.round(ms / 1000);
        const m = Math.floor(totalSec / 60);
        const s = totalSec % 60;
        return `${m}:${s.toString().padStart(2, '0')}`;
    }
})();
