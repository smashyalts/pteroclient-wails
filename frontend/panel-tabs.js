/**
 * The panel tabs that sit beside Files and Console: Databases, Backups,
 * Schedules, Users, Network, Startup, Activity and Settings. Each one loads
 * lazily the first time its tab is shown and reloads on demand, so opening the
 * app does not fire eight API calls at once.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const bytes = (v) => window.Shell.fmt.bytes(v);
    const icon = (n, c) => window.Icons.svg(n, c);

    function when(value) {
        if (!value) return '—';
        const d = new Date(value);
        if (isNaN(d.getTime())) return String(value);
        return d.toLocaleString();
    }

    function empty(target, title, hint, iconName) {
        target.innerHTML =
            '<div class="empty-state">' + icon(iconName || 'file') +
            '<div class="empty-state-title">' + esc(title) + '</div>' +
            (hint ? '<div class="empty-state-hint">' + esc(hint) + '</div>' : '') +
            '</div>';
    }

    function busy(target) {
        target.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '<div>Loading…</div></div>';
    }

    function failed(target, err) {
        target.innerHTML =
            '<div class="empty-state">' + icon('warning') +
            '<div class="empty-state-title" style="color:var(--danger-text)">Could not load this tab</div>' +
            '<div class="empty-state-hint">' + esc(String(err && err.message ? err.message : err)) + '</div>' +
            '</div>';
    }

    /** Wraps a loader so every tab gets the same busy/empty/error handling. */
    function pane(id, loader) {
        return async function () {
            const target = $(id);
            if (!target) return;
            const api = go();
            if (!api) return empty(target, 'Not connected', 'Connect a panel and pick a server first.', 'plug');

            busy(target);
            try {
                await loader(target, api);
            } catch (err) {
                failed(target, err);
            }
        };
    }

    function rows(list) {
        return '<div class="card">' + list.join('') + '</div>';
    }

    /* -------------------------------------------------------- databases */

    const loadDatabases = pane('databasesBody', async (target, api) => {
        const items = await api.ListDatabases();
        if (!items || !items.length) {
            return empty(target, 'No databases', 'Databases you create here appear in the panel too, with their own host and credentials.', 'database');
        }

        target.innerHTML = rows(items.map((db) => {
            const host = db.host || {};
            return '<div class="list-row">' +
                '<span class="list-badge on">' + icon('database', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(db.name) + '</span>' +
                '<span class="list-sub">' + esc(host.address || '') + ':' + esc(host.port || '') + ' · ' + esc(db.username || '') + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>from ' + esc(db.connections_from || '%') + '</span>' +
                '<span>' + esc(db.max_connections || 0) + ' max conn</span></span>' +
                '<span class="list-actions">' +
                '<button class="sm" data-db-rotate="' + esc(db.id) + '" type="button">Rotate password</button>' +
                '<button class="sm danger" data-db-delete="' + esc(db.id) + '" data-name="' + esc(db.name) + '" type="button">Delete</button>' +
                '</span></div>';
        }));
    });

    /* ----------------------------------------------------------- backups */

    const loadBackups = pane('backupsBody', async (target, api) => {
        const items = await api.ListBackups();
        if (!items || !items.length) {
            return empty(target, 'No backups yet', 'A backup captures the server files at a point in time. The panel caps how many each server may keep.', 'backup');
        }

        target.innerHTML = rows(items.map((b) => {
            const done = !!b.completed_at;
            const tag = !done ? '<span class="tag warn">Running</span>'
                : b.is_successful ? '<span class="tag ok">Complete</span>'
                    : '<span class="tag bad">Failed</span>';
            return '<div class="list-row">' +
                '<span class="list-badge' + (done && b.is_successful ? ' on' : '') + '">' + icon(b.is_locked ? 'lock' : 'backup', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(b.name) + ' ' + tag + '</span>' +
                '<span class="list-sub">' + esc(b.uuid) + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>' + bytes(b.bytes) + '</span><span>' + esc(when(b.created_at)) + '</span></span>' +
                '<span class="list-actions">' +
                (done && b.is_successful ? '<button class="sm" data-bk-download="' + esc(b.uuid) + '" type="button">Download</button>' : '') +
                '<button class="sm" data-bk-lock="' + esc(b.uuid) + '" type="button">' + (b.is_locked ? 'Unlock' : 'Lock') + '</button>' +
                (done && b.is_successful ? '<button class="sm" data-bk-restore="' + esc(b.uuid) + '" data-name="' + esc(b.name) + '" type="button">Restore</button>' : '') +
                '<button class="sm danger" data-bk-delete="' + esc(b.uuid) + '" data-name="' + esc(b.name) + '" type="button">Delete</button>' +
                '</span></div>';
        }));
    });

    /* --------------------------------------------------------- schedules */

    function cronText(cron) {
        if (!cron) return '';
        return [cron.minute, cron.hour, cron.day_of_month, cron.month, cron.day_of_week].join(' ');
    }

    const loadSchedules = pane('schedulesBody', async (target, api) => {
        const items = await api.ListSchedules();
        if (!items || !items.length) {
            return empty(target, 'No schedules', 'Schedules run tasks on a cron — a nightly restart, a backup before a wipe. Create them in the panel; run and pause them here.', 'clock');
        }

        target.innerHTML = rows(items.map((s) => {
            const cron = s.cron || {};
            const state = s.is_processing ? '<span class="tag warn">Running</span>'
                : s.is_active ? '<span class="tag ok">Active</span>'
                    : '<span class="tag muted">Paused</span>';
            return '<div class="list-row">' +
                '<span class="list-badge' + (s.is_active ? ' on' : '') + '">' + icon('clock', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(s.name) + ' ' + state + '</span>' +
                '<span class="list-sub">' + esc(cronText(cron)) + (s.only_when_online ? ' · only when online' : '') + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>last ' + esc(when(s.last_run_at)) + '</span><span>next ' + esc(when(s.next_run_at)) + '</span></span>' +
                '<span class="list-actions">' +
                '<button class="sm" data-sc-run="' + esc(s.id) + '" type="button">Run now</button>' +
                '<button class="sm" data-sc-toggle="' + esc(s.id) + '" ' +
                'data-name="' + esc(s.name) + '" data-active="' + (s.is_active ? '1' : '0') + '" ' +
                'data-online="' + (s.only_when_online ? '1' : '0') + '" ' +
                'data-minute="' + esc(cron.minute) + '" data-hour="' + esc(cron.hour) + '" ' +
                'data-dom="' + esc(cron.day_of_month) + '" data-month="' + esc(cron.month) + '" ' +
                'data-dow="' + esc(cron.day_of_week) + '" type="button">' + (s.is_active ? 'Pause' : 'Resume') + '</button>' +
                '<button class="sm danger" data-sc-delete="' + esc(s.id) + '" data-name="' + esc(s.name) + '" type="button">Delete</button>' +
                '</span></div>';
        }));
    });

    /* ------------------------------------------------------------- users */

    const loadUsers = pane('usersBody', async (target, api) => {
        const items = await api.ListSubusers();
        if (!items || !items.length) {
            return empty(target, 'No subusers', 'Subusers get scoped access to this one server. The owner is not listed here.', 'users');
        }

        target.innerHTML = rows(items.map((u) => {
            const perms = Array.isArray(u.permissions) ? u.permissions.length : 0;
            return '<div class="list-row">' +
                '<span class="list-badge on">' + icon('users', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(u.username || u.email) +
                (u['2fa_enabled'] ? ' <span class="tag ok">2FA</span>' : '') + '</span>' +
                '<span class="list-sub">' + esc(u.email) + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>' + perms + ' permissions</span><span>' + esc(when(u.created_at)) + '</span></span>' +
                '<span class="list-actions">' +
                '<button class="sm danger" data-user-delete="' + esc(u.uuid) + '" data-name="' + esc(u.username || u.email) + '" type="button">Revoke</button>' +
                '</span></div>';
        }));
    });

    /* ----------------------------------------------------------- network */

    const loadNetwork = pane('networkBody', async (target, api) => {
        const items = await api.ListAllocations();
        if (!items || !items.length) {
            return empty(target, 'No allocations', 'Allocations are the IP and port pairs the node has assigned to this server.', 'network');
        }

        target.innerHTML = rows(items.map((a) => (
            '<div class="list-row">' +
            '<span class="list-badge' + (a.is_default ? ' on' : '') + '">' + icon('network', 'ic-14') + '</span>' +
            '<span class="list-main">' +
            '<span class="list-title mono">' + esc(a.ip_alias || a.ip) + ':' + esc(a.port) +
            (a.is_default ? ' <span class="tag accent">Primary</span>' : '') + '</span>' +
            '<span class="list-sub">' + esc(a.notes || 'No note') + '</span>' +
            '</span>' +
            '<span class="list-actions">' +
            '<button class="sm" data-alloc-note="' + esc(a.id) + '" data-notes="' + esc(a.notes || '') + '" type="button">Note</button>' +
            (a.is_default ? '' : '<button class="sm" data-alloc-primary="' + esc(a.id) + '" type="button">Make primary</button>') +
            '</span></div>'
        )));
    });

    /* ----------------------------------------------------------- startup */

    const loadStartup = pane('startupBody', async (target, api) => {
        const info = await api.GetStartup();
        const vars = info.variables || [];

        let html = '<div class="card card-pad" style="margin-bottom:12px">' +
            '<div class="eyebrow">Startup command</div>' +
            '<div class="mono" style="margin-top:9px;font-size:12px;line-height:1.6;color:var(--text-primary);word-break:break-all">' +
            esc(info.startup_command || info.raw_startup_command || '—') + '</div>';

        const images = info.docker_images || {};
        const imageNames = Object.keys(images);
        if (imageNames.length) {
            html += '<div class="eyebrow" style="margin-top:16px">Docker image</div>' +
                '<div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:9px">' +
                imageNames.map((label) => (
                    '<button class="sm" data-image="' + esc(images[label]) + '" type="button" title="' + esc(images[label]) + '">' + esc(label) + '</button>'
                )).join('') + '</div>';
        }
        html += '</div>';

        if (!vars.length) {
            target.innerHTML = html + '<div class="empty-state">' + icon('sliders') +
                '<div class="empty-state-title">This egg exposes no editable variables</div></div>';
            return;
        }

        html += rows(vars.map((v) => (
            '<div class="list-row">' +
            '<span class="list-main">' +
            '<span class="list-title">' + esc(v.name) + (v.is_editable ? '' : ' <span class="tag muted">Read only</span>') + '</span>' +
            '<span class="list-sub">' + esc(v.env_variable) + ' · ' + esc(v.description || '') + '</span>' +
            '</span>' +
            '<span class="list-meta mono">' + esc(v.server_value === '' ? '(empty)' : v.server_value) + '</span>' +
            '<span class="list-actions">' +
            (v.is_editable
                ? '<button class="sm" data-var-key="' + esc(v.env_variable) + '" data-var-name="' + esc(v.name) + '" data-var-value="' + esc(v.server_value || '') + '" data-var-rules="' + esc(v.rules || '') + '" type="button">Edit</button>'
                : '') +
            '</span></div>'
        )));

        target.innerHTML = html;
    });

    /* ---------------------------------------------------------- activity */

    const loadActivity = pane('activityBody', async (target, api) => {
        const items = await api.ListActivity(1);
        if (!items || !items.length) {
            return empty(target, 'No activity recorded', 'The panel logs power actions, file writes, SFTP sessions and subuser changes here.', 'activity');
        }

        target.innerHTML = rows(items.map((a) => (
            '<div class="list-row">' +
            '<span class="list-badge">' + icon('activity', 'ic-14') + '</span>' +
            '<span class="list-main">' +
            '<span class="list-title">' + esc(a.event) + (a.is_api ? ' <span class="tag muted">API</span>' : '') + '</span>' +
            '<span class="list-sub">' + esc(a.description || '') + '</span>' +
            '</span>' +
            '<span class="list-meta"><span>' + esc(a.ip || '') + '</span><span>' + esc(when(a.timestamp)) + '</span></span>' +
            '</div>'
        )));
    });

    /* ---------------------------------------------------------- settings */

    const loadSettings = pane('settingsBody', async (target, api) => {
        const server = await api.GetServerDetails();
        const limits = server.limits || {};
        const features = server.feature_limits || {};

        target.innerHTML =
            '<div class="card card-pad" style="margin-bottom:12px">' +
            '<div class="eyebrow">Server</div>' +
            '<div class="form-group" style="margin-top:12px">' +
            '<label>Display name</label><input type="text" id="settingsName" value="' + esc(server.name || '') + '">' +
            '</div>' +
            '<div class="form-group">' +
            '<label>Description</label><input type="text" id="settingsDescription" value="' + esc(server.description || '') + '">' +
            '</div>' +
            '<button class="primary" id="settingsRenameBtn" type="button">Save details</button>' +
            '</div>' +

            '<div class="card card-pad" style="margin-bottom:12px">' +
            '<div class="eyebrow">Limits</div>' +
            '<div class="list-meta" style="margin-top:10px;gap:22px;flex-wrap:wrap">' +
            '<span>Memory ' + (limits.memory ? limits.memory + ' MB' : '∞') + '</span>' +
            '<span>Disk ' + (limits.disk ? limits.disk + ' MB' : '∞') + '</span>' +
            '<span>CPU ' + (limits.cpu ? limits.cpu + '%' : '∞') + '</span>' +
            '<span>Databases ' + esc(features.databases != null ? features.databases : '—') + '</span>' +
            '<span>Backups ' + esc(features.backups != null ? features.backups : '—') + '</span>' +
            '<span>Allocations ' + esc(features.allocations != null ? features.allocations : '—') + '</span>' +
            '</div></div>' +

            '<div class="card card-pad">' +
            '<div class="eyebrow" style="color:var(--danger-text)">Reinstall</div>' +
            '<div class="pane-intro" style="margin:10px 0 14px">Re-runs the egg install script. Some eggs wipe the server files when they reinstall — take a backup first.</div>' +
            '<button class="danger" id="settingsReinstallBtn" type="button">Reinstall server</button>' +
            '</div>';
    });

    /* ---------------------------------------------------------- registry */

    const LOADERS = {
        databases: loadDatabases,
        backups: loadBackups,
        schedules: loadSchedules,
        users: loadUsers,
        network: loadNetwork,
        startup: loadStartup,
        activity: loadActivity,
        settings: loadSettings
    };

    const loaded = {};

    function reload(name) {
        if (LOADERS[name]) LOADERS[name]();
    }

    document.addEventListener('tab:show', (e) => {
        const name = e.detail;
        if (!LOADERS[name]) return;
        if (!loaded[name]) loaded[name] = true;
        reload(name);
    });

    /* ----------------------------------------------------------- actions */

    async function guard(fn, reloadName) {
        try {
            await fn();
            if (reloadName) reload(reloadName);
        } catch (err) {
            window.Shell.dialog.confirm('Request failed', esc(String(err && err.message ? err.message : err)), { confirmLabel: 'OK' });
        }
    }

    document.addEventListener('click', async (e) => {
        const el = e.target.closest('button');
        if (!el) return;
        const api = go();
        const D = window.Shell.dialog;

        // Toolbar refresh buttons
        if (el.dataset.reload) return reload(el.dataset.reload);

        /* databases */
        if (el.id === 'newDatabaseBtn') {
            const v = await D.form('New database', [
                { name: 'name', label: 'Database name', placeholder: 'survival_stats' },
                { name: 'remote', label: 'Allowed connections from', value: '%', mono: true, hint: 'A host mask. <span class="mono">%</span> allows any host.' }
            ], { confirmLabel: 'Create' });
            if (v && v.name) guard(() => api.CreateDatabase(v.name, v.remote || '%'), 'databases');
            return;
        }
        if (el.dataset.dbRotate) return guard(() => api.RotateDatabasePassword(el.dataset.dbRotate), 'databases');
        if (el.dataset.dbDelete) {
            const ok = await D.confirm('Delete database', 'Deleting <b>' + esc(el.dataset.name) + '</b> destroys its contents. This cannot be undone.', { danger: true, confirmLabel: 'Delete' });
            if (ok) guard(() => api.DeleteDatabase(el.dataset.dbDelete), 'databases');
            return;
        }

        /* backups */
        if (el.id === 'newBackupBtn') {
            const v = await D.form('Create backup', [
                { name: 'name', label: 'Name', placeholder: 'Leave blank for a timestamped name' },
                { name: 'ignored', label: 'Ignored paths', placeholder: 'cache/\nlogs/', mono: true, hint: 'One glob per line, same syntax as .gitignore.' }
            ], { confirmLabel: 'Start backup' });
            if (v) guard(() => api.CreateBackup(v.name || '', v.ignored || ''), 'backups');
            return;
        }
        if (el.dataset.bkLock) return guard(() => api.ToggleBackupLock(el.dataset.bkLock), 'backups');
        if (el.dataset.bkDownload) {
            return guard(async () => {
                const url = await api.GetBackupDownloadURL(el.dataset.bkDownload);
                if (window.runtime && window.runtime.BrowserOpenURL) window.runtime.BrowserOpenURL(url);
                else window.open(url, '_blank');
            });
        }
        if (el.dataset.bkRestore) {
            const ok = await D.confirm('Restore backup', 'Restoring <b>' + esc(el.dataset.name) + '</b> overwrites the current server files. Stop the server first.', { danger: true, confirmLabel: 'Restore' });
            if (ok) guard(() => api.RestoreBackup(el.dataset.bkRestore, false), 'backups');
            return;
        }
        if (el.dataset.bkDelete) {
            const ok = await D.confirm('Delete backup', 'Delete <b>' + esc(el.dataset.name) + '</b> permanently?', { danger: true, confirmLabel: 'Delete' });
            if (ok) guard(() => api.DeleteBackup(el.dataset.bkDelete), 'backups');
            return;
        }

        /* schedules */
        if (el.dataset.scRun) return guard(() => api.ExecuteSchedule(el.dataset.scRun), 'schedules');
        if (el.dataset.scToggle) {
            const d = el.dataset;
            return guard(() => api.SetScheduleActive(
                d.scToggle, d.name, d.minute, d.hour, d.dom, d.month, d.dow,
                d.active !== '1', d.online === '1'
            ), 'schedules');
        }
        if (el.dataset.scDelete) {
            const ok = await D.confirm('Delete schedule', 'Delete <b>' + esc(el.dataset.name) + '</b> and its tasks?', { danger: true, confirmLabel: 'Delete' });
            if (ok) guard(() => api.DeleteSchedule(el.dataset.scDelete), 'schedules');
            return;
        }

        /* users */
        if (el.dataset.userDelete) {
            const ok = await D.confirm('Revoke access', 'Remove <b>' + esc(el.dataset.name) + '</b> from this server?', { danger: true, confirmLabel: 'Revoke' });
            if (ok) guard(() => api.DeleteSubuser(el.dataset.userDelete), 'users');
            return;
        }

        /* network */
        if (el.dataset.allocPrimary) return guard(() => api.SetPrimaryAllocation(el.dataset.allocPrimary), 'network');
        if (el.dataset.allocNote) {
            const v = await D.form('Allocation note', [
                { name: 'notes', label: 'Note', value: el.dataset.notes, placeholder: 'What this port is for' }
            ]);
            if (v) guard(() => api.SetAllocationNotes(el.dataset.allocNote, v.notes || ''), 'network');
            return;
        }

        /* startup */
        if (el.dataset.varKey) {
            const v = await D.form('Edit ' + el.dataset.varName, [
                { name: 'value', label: el.dataset.varKey, value: el.dataset.varValue, mono: true, hint: el.dataset.varRules ? 'Rules: <span class="mono">' + esc(el.dataset.varRules) + '</span>' : '' }
            ]);
            if (v) guard(() => api.SetStartupVariable(el.dataset.varKey, v.value), 'startup');
            return;
        }
        if (el.dataset.image) {
            const ok = await D.confirm('Change Docker image', 'Switch this server to <span class="mono">' + esc(el.dataset.image) + '</span>? It takes effect on the next start.');
            if (ok) guard(() => api.SetDockerImage(el.dataset.image), 'startup');
            return;
        }

        /* settings */
        if (el.id === 'settingsRenameBtn') {
            const name = $('settingsName').value;
            const description = $('settingsDescription').value;
            return guard(() => api.RenameServer(name, description), 'settings');
        }
        if (el.id === 'settingsReinstallBtn') {
            const ok = await D.confirm('Reinstall server', 'This re-runs the egg install script. Some eggs delete the server files. Continue?', { danger: true, confirmLabel: 'Reinstall' });
            if (ok) guard(() => api.ReinstallServer(), 'settings');
            return;
        }
    });

    window.PanelTabs = { reload, loaders: Object.keys(LOADERS) };
})();
