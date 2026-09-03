/**
 * The transfer view: this machine on the left, the server on the right.
 *
 * The Files tab is for working on a server — editing, deleting, browsing. This
 * is for moving things between two places, which is a different job and wants a
 * different shape: two listings side by side, and a direction.
 *
 * Everything here goes over SFTP. Nothing else in the app does, so this tab is
 * the only place that needs a connection, and it says so when there is not one
 * rather than failing per action.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const bytes = (n) => window.Shell.fmt.bytes(n);
    const icon = (n, c) => window.Icons.svg(n, c);

    // Two panes, the same shape either side.
    const panes = {
        local: { path: '', entries: [], selected: new Set(), loading: false, error: '' },
        remote: { path: '/', entries: [], selected: new Set(), loading: false, error: '' }
    };

    let busy = false;

    /* ------------------------------------------------------------ listings */

    async function load(side, path) {
        const pane = panes[side];
        const api = go();
        if (!api) return;

        pane.loading = true;
        pane.error = '';
        paint(side);

        try {
            if (side === 'local') {
                const listing = await api.ListLocal(path === undefined ? pane.path : path);
                pane.path = listing.path;
                pane.parent = listing.parent;
                pane.roots = listing.roots || [];
                pane.entries = listing.entries || [];
            } else {
                const target = path === undefined ? pane.path : path;
                const items = await api.ListRemote(target);
                pane.path = target || '/';
                pane.entries = (items || []).map((e) => ({
                    path: e.path, name: e.name, size: e.size, is_dir: e.is_dir
                }));
            }
            // A listing is a new set of rows, so a selection from the last one
            // would be pointing at things that are no longer on screen.
            pane.selected.clear();
        } catch (err) {
            pane.error = String(err && err.message ? err.message : err);
            pane.entries = [];
        } finally {
            pane.loading = false;
            paint(side);
            paintActions();
        }
    }

    function up(side) {
        const pane = panes[side];
        if (side === 'local') {
            if (pane.parent) load('local', pane.parent);
            return;
        }
        if (pane.path === '/') return;
        const parent = pane.path.replace(/\/[^/]+\/?$/, '') || '/';
        load('remote', parent);
    }

    function enter(side, entry) {
        if (!entry.is_dir && !entry.isDir) return;
        load(side, entry.path);
    }

    /* -------------------------------------------------------------- drawing */

    function paint(side) {
        const list = $('tx-' + side + '-list');
        const crumb = $('tx-' + side + '-path');
        if (!list || !crumb) return;

        const pane = panes[side];
        crumb.textContent = pane.path || (side === 'local' ? 'This computer' : '/');
        crumb.title = pane.path;

        if (pane.loading && !pane.entries.length) {
            list.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '</div>';
            return;
        }
        if (pane.error) {
            list.innerHTML = '<div class="empty-state">' + icon('warning') +
                '<div class="empty-state-title">' +
                (side === 'remote' ? 'Not connected' : 'That folder could not be read') +
                '</div><div class="empty-state-hint">' + esc(pane.error) + '</div></div>';
            return;
        }

        let html = '';
        const canGoUp = side === 'local' ? !!pane.parent : pane.path !== '/';
        if (canGoUp) {
            html += '<div class="file-item" data-up="1">' +
                '<span class="file-icon kind-dir">' + icon('folder') + '</span>' +
                '<span class="file-name">..</span></div>';
        }

        pane.entries.forEach((entry, i) => {
            const isDir = entry.is_dir || entry.isDir;
            html += '<div class="file-item' + (pane.selected.has(entry.path) ? ' selected' : '') +
                '" data-index="' + i + '">' +
                '<span class="file-icon ' + (isDir ? 'kind-dir' : '') + '">' +
                icon(isDir ? 'folder' : 'file') + '</span>' +
                '<span class="file-name">' + esc(entry.name) + '</span>' +
                '<span class="file-size">' + (isDir ? '' : bytes(entry.size || 0)) + '</span>' +
                '</div>';
        });

        if (!pane.entries.length) {
            html += '<div class="preview-empty">This folder is empty</div>';
        }
        list.innerHTML = html;

        const count = $('tx-' + side + '-count');
        if (count) {
            const n = pane.selected.size;
            count.textContent = n ? n + ' selected' : pane.entries.length + ' item(s)';
        }
    }

    // The direction buttons are only live when they would do something.
    function paintActions() {
        const connected = window.SFTP && window.SFTP.isConnected();
        const upBtn = $('txUpload');
        const downBtn = $('txDownload');

        if (upBtn) upBtn.disabled = busy || !connected || panes.local.selected.size === 0;
        if (downBtn) downBtn.disabled = busy || !connected || panes.remote.selected.size === 0;

        const status = $('txStatus');
        if (status) {
            if (!connected) {
                status.innerHTML = '<span class="tag">Not connected</span>' +
                    '<span class="list-sub">Connect to move files</span>';
            } else {
                const s = window.SFTP.status();
                status.innerHTML = '<span class="tag ok">Connected</span>' +
                    '<span class="list-sub mono">' + esc(s.user + '@' + s.host) +
                    ' · ' + s.streams + ' parallel</span>';
            }
        }

        const connectBtn = $('txConnect');
        if (connectBtn) connectBtn.textContent = connected ? 'Disconnect' : 'Connect';
    }

    /* ------------------------------------------------------------ selection */

    function clickRow(side, row, e) {
        const pane = panes[side];

        if (row.dataset.up) return up(side);

        const entry = pane.entries[Number(row.dataset.index)];
        if (!entry) return;

        if (e.detail > 1) return enter(side, entry);

        if (e.ctrlKey || e.metaKey) {
            if (pane.selected.has(entry.path)) pane.selected.delete(entry.path);
            else pane.selected.add(entry.path);
        } else if (e.shiftKey && pane.lastIndex !== undefined) {
            const from = Math.min(pane.lastIndex, Number(row.dataset.index));
            const to = Math.max(pane.lastIndex, Number(row.dataset.index));
            for (let i = from; i <= to; i++) pane.selected.add(pane.entries[i].path);
        } else {
            pane.selected.clear();
            pane.selected.add(entry.path);
        }
        pane.lastIndex = Number(row.dataset.index);

        // Only the classes, not the whole list. Redrawing on every click
        // replaced the row under the pointer, so the second half of a
        // double-click landed on an element that no longer existed — and it
        // flickered besides.
        paintSelection(side);
        paintActions();
    }

    function paintSelection(side) {
        const pane = panes[side];
        const list = $('tx-' + side + '-list');
        if (!list) return;

        list.querySelectorAll('.file-item[data-index]').forEach((row) => {
            const entry = pane.entries[Number(row.dataset.index)];
            row.classList.toggle('selected', !!entry && pane.selected.has(entry.path));
        });

        const count = $('tx-' + side + '-count');
        if (count) {
            const n = pane.selected.size;
            count.textContent = n ? n + ' selected' : pane.entries.length + ' item(s)';
        }
    }

    /* ------------------------------------------------------------ transfers */

    async function upload() {
        const picked = Array.from(panes.local.selected);
        if (!picked.length) return;
        busy = true;
        paintActions();
        try {
            await window.SFTP.upload(picked, panes.remote.path || '/');
            await load('remote');
        } finally {
            busy = false;
            paintActions();
        }
    }

    async function download() {
        const picked = Array.from(panes.remote.selected);
        if (!picked.length) return;
        busy = true;
        paintActions();
        try {
            // Straight into the folder the local pane is showing, rather than
            // asking again: the whole point of two panes is that the
            // destination is already on screen.
            await window.SFTP.downloadTo(picked, panes.local.path);
            await load('local');
        } finally {
            busy = false;
            paintActions();
        }
    }

    /* --------------------------------------------------------------- wiring */

    document.addEventListener('click', async (e) => {
        const root = $('transferTab');
        if (!root || !root.contains(e.target)) return;

        const row = e.target.closest('.file-item');
        if (row) {
            const pane = e.target.closest('[data-pane]');
            if (pane) return clickRow(pane.dataset.pane, row, e);
        }

        const btn = e.target.closest('button');
        if (!btn) return;

        if (btn.id === 'txConnect') {
            if (window.SFTP.isConnected()) await window.SFTP.disconnect();
            else await window.SFTP.connect();
            paintActions();
            if (window.SFTP.isConnected()) load('remote', '/');
            return;
        }
        if (btn.id === 'txUpload') return upload();
        if (btn.id === 'txDownload') return download();
        if (btn.id === 'txLocalUp') return up('local');
        if (btn.id === 'txRemoteUp') return up('remote');
        if (btn.id === 'txLocalRefresh') return load('local');
        if (btn.id === 'txRemoteRefresh') return load('remote');
        if (btn.id === 'txReveal') {
            try { await go().RevealLocal(panes.local.path); } catch (err) { window.UX.toast.bad(String(err)); }
        }
    });

    document.addEventListener('tab:show', (e) => {
        if (e.detail !== 'transfer') return;
        if (!panes.local.entries.length) load('local');
        if (window.SFTP && window.SFTP.isConnected() && !panes.remote.entries.length) load('remote');
        paintActions();
    });

    function listen() {
        if (!window.runtime || !window.runtime.EventsOn) return setTimeout(listen, 200);
        window.runtime.EventsOn('sftp-status', () => {
            paintActions();
            if (window.SFTP && window.SFTP.isConnected()) load('remote', '/');
            else paint('remote');
        });
    }
    listen();

    window.Transfer = {
        load: load,
        localPath: () => panes.local.path,
        remotePath: () => panes.remote.path
    };
}());
