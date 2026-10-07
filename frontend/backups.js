/**
 * The Backups tab: what is there, and pulling it down.
 *
 * Restore and delete are deliberately absent, the same way they are absent
 * from the Go bindings. Downloading is the opposite kind of operation — it
 * changes nothing on the panel, and it is the one that makes a bad day
 * survivable — so it is here.
 *
 * How fast a download goes is not ours to decide, and the window says which
 * it will be rather than leaving it a mystery. A backup kept on the node
 * comes down as one stream, because wings writes the archive to a single
 * response and ignores range requests. A backup kept in a bucket is a
 * presigned S3 URL, which honours ranges, so it is pulled over several
 * connections at once. Each row is labelled before anything is started.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const bytes = (v) => window.Shell.fmt.bytes(v);
    const icon = (n, c) => window.Icons.svg(n, c);
    const toast = () => (window.UX && window.UX.toast) || null;

    function warn(message) {
        const t = toast();
        if (t) t.bad(String(message));
    }

    // Jobs this session started, so a progress event can repaint one row
    // without refetching the whole list.
    const jobs = new Map();
    // A job knows its destination path, not the backup it came from, so the
    // link between the two is kept here.
    const jobRow = new Map();

    function rate(bytesPerSecond) {
        if (!bytesPerSecond || bytesPerSecond < 1) return '';
        return bytes(bytesPerSecond) + '/s';
    }

    function eta(seconds) {
        if (!seconds || seconds < 1) return '';
        const s = Math.round(seconds);
        if (s < 60) return s + 's left';
        const m = Math.floor(s / 60);
        if (m < 60) return m + 'm ' + (s % 60) + 's left';
        return Math.floor(m / 60) + 'h ' + (m % 60) + 'm left';
    }

    function when(value) {
        if (!value) return '';
        const d = new Date(value);
        return isNaN(d.getTime()) ? '' : d.toLocaleString();
    }

    function cssEscape(v) {
        return String(v).replace(/["\\]/g, '\\$&');
    }

    /* ----------------------------------------------------------- the list */

    async function load() {
        const target = $('backupsBody');
        if (!target) return;

        const api = go();
        if (!api) {
            target.innerHTML = '<div class="empty-state">' + icon('plug') +
                '<div class="empty-state-title">Not connected</div>' +
                '<div class="empty-state-hint">Connect a panel and pick a server first.</div></div>';
            return;
        }

        target.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '<div>Loading…</div></div>';
        paintFolder();

        let items;
        try {
            items = await api.ListBackups();
        } catch (err) {
            target.innerHTML = '<div class="empty-state">' + icon('warning') +
                '<div class="empty-state-title" style="color:var(--danger-text)">Could not list backups</div>' +
                '<div class="empty-state-hint">' + esc(String(err && err.message ? err.message : err)) +
                '</div></div>';
            return;
        }

        if (!items || !items.length) {
            target.innerHTML = '<div class="empty-state">' + icon('archive') +
                '<div class="empty-state-title">No backups</div>' +
                '<div class="empty-state-hint">This server has none yet. The panel can create one.</div></div>';
            return;
        }

        target.innerHTML = '<div class="card">' + items.map(row).join('') + '</div>';
        jobs.forEach((status) => paintJob(status));
    }

    function row(b) {
        const tags = [];
        if (!b.is_successful) tags.push('<span class="tag bad">Failed</span>');
        if (b.is_locked) tags.push('<span class="tag">Locked</span>');

        // A failed backup has nothing to fetch, and the panel refuses it.
        const action = b.is_successful
            ? '<button class="sm" data-backup="' + esc(b.uuid) + '" type="button">Download</button>'
            : '';

        return '<div class="list-row">' +
            '<span class="list-badge on">' + icon('archive', 'ic-14') + '</span>' +
            '<span class="list-main">' +
            '<span class="list-title">' + esc(b.name || b.uuid) + ' ' + tags.join(' ') + '</span>' +
            '<span class="list-sub">' + esc(bytes(b.bytes || 0)) +
            (b.completed_at ? ' · ' + esc(when(b.completed_at)) : '') +
            (b.disk ? ' · on ' + esc(b.disk) : '') + '</span>' +
            '<span class="list-sub">' + esc(b.transfer_note || '') + '</span>' +
            '<span class="list-sub" data-slot="' + esc(b.uuid) + '"></span>' +
            '</span>' +
            '<span class="list-actions">' + action + '</span>' +
            '</div>';
    }

    /* ------------------------------------------------------- the progress */

    function paintJob(status) {
        if (!status || !status.id) return;
        jobs.set(status.id, status);

        const uuid = jobRow.get(status.id);
        if (!uuid) return;
        const slot = document.querySelector('[data-slot="' + cssEscape(uuid) + '"]');
        if (!slot) return;

        const p = status.progress || {};
        const total = p.total || 0;
        const done = p.done || 0;
        const pct = total > 0 ? Math.min(100, (100 * done) / total) : 0;

        let line = '';
        let bad = false;

        switch (status.state) {
            case 'queued':
                line = 'Queued';
                break;
            case 'running': {
                const bits = [];
                if (total > 0) bits.push(pct.toFixed(1) + '%');
                bits.push(bytes(done) + (total ? ' of ' + bytes(total) : ''));
                const r = rate(p.bytes_per_second);
                if (r) bits.push(r);
                const e = eta(p.eta_seconds);
                if (e) bits.push(e);
                // Worth showing: it is the difference between a transfer that
                // can use the whole link and one that cannot.
                if (p.ranged && p.parts > 1) bits.push(p.parts + ' connections');
                if (p.retries > 0) bits.push(p.retries + ' retries');
                if (p.phase === 'verifying') bits.push('checking the archive');
                line = bits.join(' · ');
                break;
            }
            case 'done': {
                const result = status.result || {};
                const bits = ['Saved'];
                if (result.verified) bits.push('checksum verified');
                if (result.duration) {
                    const secs = result.duration / 1e9; // Go reports nanoseconds
                    if (secs > 0.5) bits.push('avg ' + rate(result.bytes / secs));
                }
                line = bits.join(' · ');
                break;
            }
            case 'cancelled':
                line = 'Cancelled. The partly downloaded file was kept.';
                break;
            default:
                line = 'Failed: ' + (status.error || 'unknown');
                bad = true;
                break;
        }

        const running = status.state === 'running' || status.state === 'queued';
        const bar = running && total > 0
            ? '<span class="dl-bar"><span style="width:' + pct.toFixed(2) + '%"></span></span>'
            : '';
        const stop = running
            ? ' <button class="sm" data-cancel="' + esc(status.id) + '" type="button">Cancel</button>'
            : '';
        const reveal = status.state === 'done'
            ? ' <button class="sm" data-reveal="' + esc(status.id) + '" type="button">Show folder</button>'
            : '';

        slot.innerHTML = bar +
            '<span' + (bad ? ' style="color:var(--danger-text)"' : '') + '>' + esc(line) + '</span>' +
            stop + reveal;
    }

    async function paintFolder() {
        const label = $('backupsFolder');
        const api = go();
        if (!label || !api) return;
        try {
            label.textContent = await api.GetDownloadFolder();
        } catch (err) {
            label.textContent = '';
        }
    }

    /* --------------------------------------------------------- the wiring */

    document.addEventListener('tab:show', (e) => {
        if (e.detail === 'backups') load();
    });

    document.addEventListener('click', async (event) => {
        const el = event.target.closest(
            '[data-backup],[data-cancel],[data-reveal],#backupsFolderBtn,#backupsClearBtn,[data-reload="backups"]');
        if (!el) return;

        const api = go();
        if (!api) return;

        if (el.dataset.reload === 'backups') return load();

        if (el.id === 'backupsFolderBtn') {
            try {
                await api.ChooseDownloadFolder();
            } catch (err) {
                warn('Could not change the folder: ' + err);
            }
            return paintFolder();
        }

        if (el.id === 'backupsClearBtn') {
            try {
                await api.ClearFinishedDownloads();
            } catch (err) {
                warn(err);
            }
            jobs.clear();
            jobRow.clear();
            return load();
        }

        if (el.dataset.cancel) {
            try {
                await api.CancelDownload(el.dataset.cancel);
            } catch (err) {
                warn(err);
            }
            return;
        }

        if (el.dataset.reveal) {
            try {
                await api.RevealDownload(el.dataset.reveal);
            } catch (err) {
                warn(err);
            }
            return;
        }

        if (el.dataset.backup) {
            const uuid = el.dataset.backup;
            el.disabled = true;
            try {
                const id = await api.StartBackupDownload(uuid);
                jobRow.set(id, uuid);
                paintJob({ id: id, state: 'queued', progress: {} });
            } catch (err) {
                warn('Could not start the download: ' + err);
            } finally {
                el.disabled = false;
            }
        }
    });

    // The Go side pushes every state change, so nothing here polls.
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn('download-progress', (status) => paintJob(status));
    }

    window.Backups = { load: load };
})();
