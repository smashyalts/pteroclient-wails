/**
 * Vault — the local safety net, and the only tab that shows files that are not
 * on a panel right now.
 *
 * Two stores, both under ~/.pteroclient, both written by the Go side before it
 * lets anything be overwritten or removed:
 *
 *   History — every state a file had before an edit. Saving config.yml ten
 *             times leaves ten entries; any of them can be restored, and any of
 *             them can be diffed against what the panel holds now.
 *   Bin     — content the app took away wholesale: a deleted file, a file an
 *             upload replaced, a file a restore wrote over. Capped at 100 MB,
 *             oldest out first.
 *
 * Nothing in here touches a panel until Restore is pressed, and Restore itself
 * refuses to land on an existing file unless the overwrite is confirmed.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const bytes = (v) => window.Shell.fmt.bytes(v);
    const icon = (n, c) => window.Icons.svg(n, c);

    // When set, the history section shows only this path.
    let focusPath = '';

    function when(value) {
        if (!value) return '—';
        const d = new Date(value);
        if (isNaN(d.getTime())) return String(value);
        return d.toLocaleString();
    }

    function ago(value) {
        const d = new Date(value);
        if (isNaN(d.getTime())) return '';
        const secs = Math.max(0, (Date.now() - d.getTime()) / 1000);
        if (secs < 90) return 'just now';
        const mins = Math.round(secs / 60);
        if (mins < 90) return mins + 'm ago';
        const hours = Math.round(mins / 60);
        if (hours < 36) return hours + 'h ago';
        return Math.round(hours / 24) + 'd ago';
    }

    function meter(used, limit) {
        const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
        const tone = pct >= 90 ? 'var(--danger)' : pct >= 70 ? 'var(--warning)' : 'var(--accent)';
        return '<div style="height:5px;border-radius:3px;background:var(--line);overflow:hidden;margin-top:8px">' +
            '<div style="height:100%;width:' + pct + '%;background:' + tone + '"></div></div>';
    }

    /* ------------------------------------------------------------ rendering */

    function entryRow(entry) {
        const name = entry.path.split('/').pop();
        const actions = entry.captured
            ? '<button class="sm" data-vault-preview="' + esc(entry.id) + '" data-kind="' + esc(entry.kind) + '" type="button">Preview</button>' +
              '<button class="sm" data-vault-diff="' + esc(entry.id) + '" data-kind="' + esc(entry.kind) + '" type="button">Diff</button>' +
              '<button class="sm primary" data-vault-restore="' + esc(entry.id) + '" data-kind="' + esc(entry.kind) + '" type="button">Restore</button>'
            : '<span class="tag bad">No content</span>';

        return '<div class="list-row">' +
            '<span class="list-badge' + (entry.captured ? ' on' : '') + '">' +
            icon(entry.kind === 'bin' ? 'trash' : 'history', 'ic-14') + '</span>' +
            '<span class="list-main">' +
            '<span class="list-title mono">' + esc(name) +
            (entry.reason ? ' <span class="tag muted">' + esc(entry.reason) + '</span>' : '') + '</span>' +
            '<span class="list-sub mono">' + esc(entry.path) + '</span>' +
            (entry.note ? '<span class="list-sub" style="color:var(--warning)">' + esc(entry.note) + '</span>' : '') +
            '</span>' +
            '<span class="list-meta">' +
            '<span>' + (entry.captured ? bytes(entry.size) : '—') + '</span>' +
            '<span title="' + esc(when(entry.created_at)) + '">' + esc(ago(entry.created_at)) + '</span>' +
            '</span>' +
            '<span class="list-actions">' + actions +
            '<button class="sm danger" data-vault-forget="' + esc(entry.id) + '" data-kind="' + esc(entry.kind) + '" type="button" ' +
            'title="Remove this local copy. Does not touch the panel.">Forget</button>' +
            '</span></div>';
    }

    function groupHistory(entries) {
        const groups = new Map();
        entries.forEach((e) => {
            if (!groups.has(e.path)) groups.set(e.path, []);
            groups.get(e.path).push(e);
        });
        return groups;
    }

    async function render() {
        const target = $('vaultBody');
        if (!target) return null;
        const api = go();
        if (!api) {
            target.innerHTML = '<div class="empty-state">' + icon('plug') +
                '<div class="empty-state-title">Not connected</div></div>';
            return null;
        }

        target.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '<div>Reading the local store…</div></div>';

        let stats;
        let bin;
        let history;
        let policies;
        try {
            stats = await api.GetStoreStats();
            bin = (await api.ListRecycleBin()) || [];
            history = (await api.ListFileVersions('', focusPath)) || [];
            // A panel being unreachable must not stop the rest of the tab
            // rendering, so this one is allowed to come back empty.
            try { policies = await api.GetBinPolicies(); } catch (err) { policies = null; }
        } catch (err) {
            target.innerHTML = '<div class="empty-state">' + icon('warning') +
                '<div class="empty-state-title" style="color:var(--danger-text)">The local store could not be read</div>' +
                '<div class="empty-state-hint">' + esc(String(err && err.message ? err.message : err)) + '</div></div>';
            return null;
        }

        let html = '';

        /* ---- an index the store could not read ---- */
        if (stats.warning) {
            html += '<div class="card card-pad" style="margin-bottom:12px;' +
                'border-color:var(--danger);background:rgba(239,68,68,.09)">' +
                '<div class="eyebrow" style="color:var(--danger-text)">Some copies are no longer tracked</div>' +
                '<div style="margin-top:7px;font-size:12.5px;line-height:1.55">' +
                esc(stats.warning) + '</div></div>';
        }

        /* ---- header ---- */
        html += '<div class="card card-pad" style="margin-bottom:12px">' +
            '<div class="eyebrow">Local store</div>' +
            '<div class="mono" style="margin-top:8px;font-size:11.5px;color:var(--text-dim);word-break:break-all">' +
            esc(stats.root) + '</div>' +
            '<div style="display:flex;gap:26px;flex-wrap:wrap;margin-top:14px">' +
            '<div style="flex:1;min-width:180px">' +
            '<div class="list-meta"><span><b>Recycle bin</b></span><span>' +
            bytes(stats.bin_used) + ' of ' + bytes(stats.bin_limit) + ' · ' + stats.bin_count + ' item(s)</span></div>' +
            meter(stats.bin_used, stats.bin_limit) +
            '<div style="margin-top:8px"><button class="sm" id="vaultBinLimitBtn" type="button" ' +
            'data-current="' + Math.round(stats.bin_limit / (1024 * 1024)) + '">Change the limit</button></div>' +
            '</div>' +
            '<div style="flex:1;min-width:180px">' +
            '<div class="list-meta"><span><b>File history</b></span><span>' +
            bytes(stats.version_used) + ' of ' + bytes(stats.version_limit) + ' · ' + stats.version_count + ' version(s)</span></div>' +
            meter(stats.version_used, stats.version_limit) +
            '<div class="list-sub" style="margin-top:8px">Up to 50 states per file, oldest dropped first.</div>' +
            '</div>' +
            '</div></div>';

        /* ---- recycle bin per server ---- */
        if (policies) {
            const off = (policies.servers || []).filter(p => !p.enabled).length;
            html += '<div class="card card-pad" style="margin-bottom:12px">' +
                '<div class="list-meta"><span class="eyebrow">Recycle bin per server</span>' +
                '<span>' + (off ? off + ' switched off' : 'on everywhere') + '</span></div>' +
                '<div class="list-sub" style="margin-top:6px">With this off, a delete on that server keeps no copy ' +
                'and cannot be undone. The confirmation says so before it runs.</div>' +
                '<div style="margin-top:10px;display:flex;align-items:center;gap:9px">' +
                '<label style="display:flex;align-items:center;gap:7px;font-size:12.5px">' +
                '<input type="checkbox" id="vaultBinDefault"' + (policies.default ? ' checked' : '') + '>' +
                'Keep copies on servers not set below</label></div>';

            if (policies.partial) {
                html += '<div class="list-sub" style="margin-top:8px;color:var(--warning)">' +
                    'Some panels could not be reached, so this list may be short: ' +
                    esc(policies.problem || '') + '</div>';
            }

            if (!(policies.servers || []).length) {
                html += '<div class="list-sub" style="margin-top:10px">No servers to list yet.</div>';
            } else {
                html += '<div style="margin-top:12px;max-height:230px;overflow:auto;' +
                    'border:1px solid var(--line);border-radius:6px">';
                policies.servers.forEach((p) => {
                    html += '<div class="list-row"' + (p.current ? ' style="background:var(--bg-secondary)"' : '') + '>' +
                        '<span class="list-main"><span class="list-title">' + esc(p.name) +
                        (p.current ? ' <span class="list-badge">this server</span>' : '') + '</span>' +
                        '<span class="list-sub">' + esc(p.panel) + ' · ' +
                        (p.explicit ? 'set here' : 'following the default') + '</span></span>' +
                        '<label style="display:flex;align-items:center;gap:6px;font-size:12px">' +
                        '<input type="checkbox" class="vault-bin-toggle" data-server="' + esc(p.server_id) + '"' +
                        (p.enabled ? ' checked' : '') + '>Keep copies</label>' +
                        (p.explicit
                            ? '<button class="sm vault-bin-clear" type="button" data-server="' + esc(p.server_id) +
                              '" title="Follow the default again">Reset</button>'
                            : '') +
                        '</div>';
                });
                html += '</div>';
            }
            html += '</div>';
        }

        /* ---- recycle bin ---- */
        html += '<div class="eyebrow" style="margin:16px 0 9px">Recycle bin</div>';
        if (!bin.length) {
            html += '<div class="empty-state">' + icon('trash') +
                '<div class="empty-state-title">The bin is empty</div>' +
                '<div class="empty-state-hint">Deleted files from <b>this server</b> land here, and so does anything an upload or a restore replaces. ' +
                'Everything stays until the bin passes ' + bytes(stats.bin_limit) + ', then the oldest goes first.</div></div>';
        } else {
            // Grouped by the delete that produced them, so a folder that came
            // in as two hundred entries goes back out as one button.
            const batches = new Map();
            const loose = [];
            bin.forEach((e) => {
                if (!e.batch) return loose.push(e);
                if (!batches.has(e.batch)) batches.set(e.batch, []);
                batches.get(e.batch).push(e);
            });

            batches.forEach((entries, batch) => {
                const total = entries.reduce((sum, e) => sum + (e.size || 0), 0);
                html += '<div class="card" style="margin-bottom:10px">' +
                    '<div class="list-row" style="background:var(--bg-secondary)">' +
                    '<span class="list-badge">' + icon('trash', 'ic-14') + '</span>' +
                    '<span class="list-main">' +
                    '<span class="list-title">Deleted ' + esc(ago(entries[entries.length - 1].created_at)) + '</span>' +
                    '<span class="list-sub">' + entries.length + ' file(s), ' + bytes(total) + '</span></span>' +
                    '<span class="list-actions">' +
                    '<button class="sm primary" data-vault-restore-batch="' + esc(batch) + '" type="button">Restore all</button>' +
                    '</span></div>' +
                    entries.map(entryRow).join('') +
                    '</div>';
            });

            if (loose.length) {
                html += '<div class="card">' + loose.map(entryRow).join('') + '</div>';
            }
        }

        /* ---- history ---- */
        html += '<div class="eyebrow" style="margin:20px 0 9px">File history' +
            (focusPath ? ' — <span class="mono">' + esc(focusPath) + '</span> ' +
                '<button class="sm" id="vaultClearFocusBtn" type="button">Show all</button>' : '') +
            '</div>';

        if (!history.length) {
            html += '<div class="empty-state">' + icon('history') +
                '<div class="empty-state-title">' + (focusPath ? 'No saved versions of this file yet' : 'Nothing saved yet') + '</div>' +
                '<div class="empty-state-hint">Every time a file is saved, the state it had before the save is filed here first. ' +
                'That happens on the first save, not on open.</div></div>';
        } else {
            const groups = groupHistory(history);
            groups.forEach((entries, path) => {
                html += '<div class="card" style="margin-bottom:10px">' +
                    '<div class="list-row" style="background:var(--bg-secondary)">' +
                    '<span class="list-badge">' + icon('file', 'ic-14') + '</span>' +
                    '<span class="list-main"><span class="list-title mono">' + esc(path) + '</span>' +
                    '<span class="list-sub">' + entries.length + ' saved state(s), newest first</span></span>' +
                    '</div>' +
                    entries.map(entryRow).join('') +
                    '</div>';
            });
        }

        target.innerHTML = html;
        return bin.concat(history);
    }

    /* ----------------------------------------------------------------- diff */

    // Line diff via a longest-common-subsequence table. Bounded: the table is
    // O(n*m), and a pair of large files would otherwise lock the window.
    const DIFF_MAX_LINES = 1500;

    function diffLines(before, after) {
        const a = before.split('\n');
        const b = after.split('\n');
        if (a.length > DIFF_MAX_LINES || b.length > DIFF_MAX_LINES) return null;

        const lcs = [];
        for (let i = 0; i <= a.length; i++) lcs.push(new Uint32Array(b.length + 1));
        for (let i = a.length - 1; i >= 0; i--) {
            for (let j = b.length - 1; j >= 0; j--) {
                lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
            }
        }

        const out = [];
        let i = 0;
        let j = 0;
        while (i < a.length && j < b.length) {
            if (a[i] === b[j]) {
                out.push(['=', a[i]]);
                i++;
                j++;
            } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
                out.push(['-', a[i++]]);
            } else {
                out.push(['+', b[j++]]);
            }
        }
        while (i < a.length) out.push(['-', a[i++]]);
        while (j < b.length) out.push(['+', b[j++]]);
        return out;
    }

    function renderDiff(rows) {
        if (!rows) {
            return '<p style="font-size:12.5px;color:var(--text-secondary)">' +
                'Both sides are over ' + DIFF_MAX_LINES + ' lines, which is more than this diff view will chew through. ' +
                'Preview the stored copy instead.</p>';
        }

        const changed = rows.filter((r) => r[0] !== '=').length;
        if (!changed) {
            return '<p style="font-size:12.5px;color:var(--text-secondary)">' +
                'This stored copy is identical to what the panel holds right now.</p>';
        }

        // Collapse long unchanged runs so the changes stay findable.
        const keep = new Array(rows.length).fill(false);
        rows.forEach((r, idx) => {
            if (r[0] === '=') return;
            for (let k = Math.max(0, idx - 3); k <= Math.min(rows.length - 1, idx + 3); k++) keep[k] = true;
        });

        let html = '<p style="font-size:12.5px;color:var(--text-secondary);margin-bottom:8px">' +
            changed + ' changed line(s). <span style="color:var(--danger-text)">−</span> is the stored copy, ' +
            '<span style="color:var(--success)">+</span> is the panel now.</p>';
        html += '<div class="mono" style="max-height:340px;overflow:auto;font-size:11.5px;line-height:1.55;' +
            'background:var(--bg-secondary);border:1px solid var(--line);border-radius:6px;padding:8px 10px">';

        let skipping = false;
        rows.forEach((r, idx) => {
            if (!keep[idx]) {
                if (!skipping) {
                    html += '<div style="opacity:.45">⋯</div>';
                    skipping = true;
                }
                return;
            }
            skipping = false;
            const colour = r[0] === '-' ? 'var(--danger-text)' : r[0] === '+' ? 'var(--success)' : 'var(--text-dim)';
            const mark = r[0] === '=' ? ' ' : r[0];
            html += '<div style="color:' + colour + ';white-space:pre-wrap">' +
                esc(mark + ' ' + r[1]) + '</div>';
        });

        html += '</div>';
        return html;
    }

    /* -------------------------------------------------------------- actions */

    async function preview(kind, id) {
        const api = go();
        try {
            const text = await api.ReadStoredCopy(kind, id);
            await window.Shell.dialog.open({
                title: 'Stored copy',
                body: '<div class="mono" style="max-height:360px;overflow:auto;font-size:11.5px;line-height:1.55;' +
                    'white-space:pre-wrap;background:var(--bg-secondary);border:1px solid var(--line);' +
                    'border-radius:6px;padding:10px">' + esc(text) + '</div>',
                confirmLabel: 'Close'
            });
        } catch (err) {
            await window.Shell.dialog.confirm('Cannot preview', esc(String(err)), { confirmLabel: 'OK' });
        }
    }

    async function diff(kind, id, entries) {
        const api = go();
        const entry = entries.find((e) => e.id === id);
        if (!entry) return;

        try {
            const stored = await api.ReadStoredCopy(kind, id);

            let current = '';
            let missing = false;
            try {
                const read = await api.ReadFileForEdit(entry.server, entry.path);
                if (read.binary || read.too_big) {
                    await window.Shell.dialog.confirm('Cannot diff',
                        'The file on the panel is binary or too large to load.', { confirmLabel: 'OK' });
                    return;
                }
                current = read.content;
            } catch (err) {
                missing = true;
            }

            const body = missing
                ? '<p style="font-size:12.5px;color:var(--text-secondary)">' +
                  'There is nothing at <span class="mono">' + esc(entry.path) + '</span> on the panel now, ' +
                  'so there is nothing to compare against. Restore puts this copy back.</p>'
                : renderDiff(diffLines(stored, current));

            await window.Shell.dialog.open({
                title: entry.path.split('/').pop() + ' — stored vs panel',
                body: body,
                confirmLabel: 'Close'
            });
        } catch (err) {
            await window.Shell.dialog.confirm('Cannot diff', esc(String(err)), { confirmLabel: 'OK' });
        }
    }

    async function restore(kind, id, entries) {
        const api = go();
        const entry = entries.find((e) => e.id === id);
        if (!entry) return;

        const D = window.Shell.dialog;

        // First attempt without overwrite. The backend answers whether anything
        // is in the way, which is a better source of truth than a stale listing.
        try {
            await api.RestoreStoredCopy(kind, id, false);
            await D.confirm('Restored', 'Wrote <span class="mono">' + esc(entry.path) + '</span> back to the panel.',
                { confirmLabel: 'OK' });
            reload();
            if (window.app) window.app.refreshFiles();
            return;
        } catch (err) {
            if (String(err).indexOf('already exists') === -1) {
                await D.confirm('Restore failed', esc(String(err)), { confirmLabel: 'OK' });
                return;
            }
        }

        const ok = await D.confirm('Replace the file on the panel?',
            '<span class="mono">' + esc(entry.path) + '</span> already exists. ' +
            'Restoring replaces it with this copy from ' + esc(when(entry.created_at)) + '.' +
            '<br><br>The file being replaced goes to the recycle bin first, so this is reversible too.',
            { danger: true, confirmLabel: 'Replace' });
        if (!ok) return;

        try {
            await api.RestoreStoredCopy(kind, id, true);
            reload();
            if (window.app) window.app.refreshFiles();
        } catch (err) {
            await D.confirm('Restore failed', esc(String(err)), { confirmLabel: 'OK' });
        }
    }

    async function restoreBatch(batch) {
        const D = window.Shell.dialog;
        const ok = await D.confirm('Restore everything from this delete',
            'Each file goes back to the path it came from. Anything already at one of those paths is left alone ' +
            'unless you say otherwise on the next prompt.',
            { confirmLabel: 'Restore' });
        if (!ok) return;

        let out;
        try {
            out = await go().RestoreBinBatch(batch, false);
        } catch (err) {
            await D.confirm('Restore failed', esc(String(err)), { confirmLabel: 'OK' });
            return;
        }

        // Anything that failed because a file is already there gets a second,
        // explicit pass. Everything else is reported as-is.
        const blocked = (out.failed || []).filter((f) => f.indexOf('already exists') !== -1);
        if (blocked.length) {
            const replace = await D.confirm('Some paths are occupied',
                blocked.length + ' of these files already exist on the panel:<br>' +
                '<span class="mono" style="font-size:11.5px">' + blocked.slice(0, 8).map(esc).join('<br>') + '</span>' +
                '<br><br>Replace them? Each one goes to the recycle bin first.',
                { danger: true, confirmLabel: 'Replace them' });
            if (replace) {
                try {
                    const second = await go().RestoreBinBatch(batch, true);
                    out.restored = (out.restored || []).concat(second.restored || []);
                    out.failed = second.failed || [];
                } catch (err) {
                    await D.confirm('Restore failed', esc(String(err)), { confirmLabel: 'OK' });
                }
            }
        }

        await D.confirm('Restore finished',
            (out.restored || []).length + ' file(s) written back.' +
            ((out.failed || []).length ? '<br><br>Could not restore:<br><span class="mono" style="font-size:11.5px">' +
                out.failed.slice(0, 10).map(esc).join('<br>') + '</span>' : ''),
            { confirmLabel: 'OK' });

        reload();
        if (window.app) window.app.refreshFiles();
    }

    /* --------------------------------------------------------------- wiring */

    // Kept so the click handler can look an entry up without another API call.
    let cache = [];

    async function reload() {
        cache = (await render()) || [];
    }

    function showHistoryFor(path) {
        focusPath = path || '';
        window.Shell.showTab('vault');
    }

    document.addEventListener('tab:show', (e) => {
        if (e.detail !== 'vault') return;
        reload();
    });

    document.addEventListener('change', async (e) => {
        const box = e.target;
        if (!box || box.type !== 'checkbox') return;

        if (box.id === 'vaultBinDefault') {
            try {
                await go().SetBinPolicyDefault(box.checked);
            } catch (err) {
                box.checked = !box.checked;
                await window.Shell.dialog.confirm('Could not save that', esc(String(err)), { confirmLabel: 'OK' });
                return;
            }
            return reload();
        }

        if (box.classList.contains('vault-bin-toggle')) {
            const server = box.dataset.server;
            // Turning it off is the dangerous direction, so it is confirmed
            // once here rather than only at delete time.
            if (!box.checked) {
                const ok = await window.Shell.dialog.confirm('Stop keeping copies for this server?',
                    'Deletes on this server will not be copied anywhere first, so nothing deleted there ' +
                    'can be restored. The delete confirmation will say so each time.',
                    { danger: true, confirmLabel: 'Stop keeping copies' });
                if (!ok) {
                    box.checked = true;
                    return;
                }
            }
            try {
                await go().SetBinPolicy(server, box.checked);
            } catch (err) {
                box.checked = !box.checked;
                await window.Shell.dialog.confirm('Could not save that', esc(String(err)), { confirmLabel: 'OK' });
                return;
            }
            return reload();
        }
    });

    document.addEventListener('click', async (e) => {
        const reset = e.target.closest && e.target.closest('.vault-bin-clear');
        if (reset) {
            try {
                await go().ClearBinPolicy(reset.dataset.server);
            } catch (err) {
                await window.Shell.dialog.confirm('Could not reset that', esc(String(err)), { confirmLabel: 'OK' });
            }
            return reload();
        }

        const el = e.target.closest('button');
        if (!el) return;

        if (el.dataset.reload === 'vault') return reload();

        if (el.id === 'vaultClearFocusBtn') {
            focusPath = '';
            return reload();
        }

        if (el.id === 'vaultEmptyBinBtn') {
            const ok = await window.Shell.dialog.confirm('Empty the recycle bin',
                'This throws away every local copy in the bin. Files already restored to the panel are not affected, ' +
                'but anything still only in the bin is gone.',
                { danger: true, confirmLabel: 'Empty it' });
            if (!ok) return;
            try {
                await go().EmptyRecycleBin();
            } catch (err) {
                await window.Shell.dialog.confirm('Could not empty the bin', esc(String(err)), { confirmLabel: 'OK' });
            }
            return reload();
        }

        if (el.id === 'vaultBinLimitBtn') {
            // Seeded with the current value rather than the default, so setting
            // it does not silently move a limit the user already raised.
            const v = await window.Shell.dialog.form('Recycle bin limit', [
                { name: 'mb', label: 'Megabytes', value: String(el.dataset.current || '100'), mono: true,
                  hint: 'Between 1 and 4096. The default is 100 MB. Lowering it evicts the oldest entries on the next delete.' }
            ], { confirmLabel: 'Set' });
            if (!v) return;
            try {
                await go().SetRecycleBinLimitMB(parseInt(v.mb, 10) || 0);
            } catch (err) {
                await window.Shell.dialog.confirm('Could not set the limit', esc(String(err)), { confirmLabel: 'OK' });
            }
            return reload();
        }

        if (el.dataset.vaultRestoreBatch) return restoreBatch(el.dataset.vaultRestoreBatch);
        if (el.dataset.vaultPreview) return preview(el.dataset.kind, el.dataset.vaultPreview);
        if (el.dataset.vaultDiff) return diff(el.dataset.kind, el.dataset.vaultDiff, cache);
        if (el.dataset.vaultRestore) return restore(el.dataset.kind, el.dataset.vaultRestore, cache);

        if (el.dataset.vaultForget) {
            const ok = await window.Shell.dialog.confirm('Forget this copy',
                'Removes the local copy only. Nothing on the panel changes, but this copy cannot be restored afterwards.',
                { danger: true, confirmLabel: 'Forget' });
            if (!ok) return;
            try {
                await go().ForgetStoredCopy(el.dataset.kind, el.dataset.vaultForget);
            } catch (err) {
                await window.Shell.dialog.confirm('Could not remove it', esc(String(err)), { confirmLabel: 'OK' });
            }
            return reload();
        }
    });

    // renderDiff is shared with the editor's save-conflict dialog, so both
    // places show a change the same way.
    window.Vault = { reload, showHistoryFor, diffHtml: (before, after) => renderDiff(diffLines(before, after)) };
})();
