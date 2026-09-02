/**
 * The panel tabs that sit beside Files and Console: Databases, Schedules,
 * Users, Network, Startup, Activity and Settings. Each one loads lazily the
 * first time its tab is shown and reloads on demand, so opening the app does
 * not fire seven API calls at once.
 *
 * Backups and Reinstall are not here, and not in the Go layer either.
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

    /* --------------------------------------------------------- schedules */

    function cronText(cron) {
        if (!cron) return '';
        return [cron.minute, cron.hour, cron.day_of_month, cron.month, cron.day_of_week].join(' ');
    }

    // What a task does, in the panel's own words.
    function taskSummary(task) {
        const payload = String(task.payload || '');
        if (task.action === 'power') return 'Power: ' + esc(payload || 'start');
        if (task.action === 'backup') {
            return 'Create a backup' + (payload ? ' (ignoring ' + esc(payload.split('\n').length) + ' pattern(s))' : '');
        }
        return '<span class="mono">' + esc(payload) + '</span>';
    }

    function taskRows(schedule) {
        const tasks = (schedule.relationships && schedule.relationships.tasks &&
            schedule.relationships.tasks.data) || schedule.tasks || [];

        const list = tasks.map((entry) => entry.attributes || entry);
        list.sort((a, b) => (a.sequence_id || 0) - (b.sequence_id || 0));

        if (!list.length) {
            return '<div class="task-empty">No tasks yet — this schedule fires and does nothing. ' +
                '<button class="sm" data-task-new="' + esc(schedule.id) + '" type="button">Add the first one</button></div>';
        }

        return '<div class="task-list">' + list.map((t) => (
            '<div class="task-row">' +
            '<span class="task-offset mono" title="Seconds after the previous task">+' + (t.time_offset || 0) + 's</span>' +
            '<span class="task-body">' + taskSummary(t) +
            (t.continue_on_failure ? ' <span class="tag muted">continues on failure</span>' : '') + '</span>' +
            '<span class="list-actions">' +
            '<button class="sm" data-task-edit="' + esc(t.id) + '" data-schedule="' + esc(schedule.id) + '" ' +
            'data-action="' + esc(t.action) + '" data-payload="' + esc(t.payload || '') + '" ' +
            'data-offset="' + (t.time_offset || 0) + '" data-continue="' + (t.continue_on_failure ? '1' : '0') + '" ' +
            'type="button">Edit</button>' +
            '<button class="sm danger" data-task-delete="' + esc(t.id) + '" data-schedule="' + esc(schedule.id) + '" ' +
            'type="button">Remove</button>' +
            '</span></div>'
        )).join('') +
            '<div class="task-add"><button class="sm" data-task-new="' + esc(schedule.id) + '" type="button">Add a task</button></div>' +
            '</div>';
    }

    const loadSchedules = pane('schedulesBody', async (target, api) => {
        const items = await api.ListSchedules();
        if (!items || !items.length) {
            return empty(target, 'No schedules', 'A schedule runs tasks on a cron — a nightly restart, a backup before a wipe. Create one with the button above.', 'clock');
        }

        target.innerHTML = items.map((s) => {
            const cron = s.cron || {};
            const state = s.is_processing ? '<span class="tag warn">Running</span>'
                : s.is_active ? '<span class="tag ok">Active</span>'
                    : '<span class="tag muted">Paused</span>';
            return '<div class="card" style="margin-bottom:10px">' +
                '<div class="list-row">' +
                '<span class="list-badge' + (s.is_active ? ' on' : '') + '">' + icon('clock', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(s.name) + ' ' + state + '</span>' +
                '<span class="list-sub mono">' + esc(cronText(cron)) + (s.only_when_online ? ' · only when online' : '') + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>last ' + esc(when(s.last_run_at)) + '</span><span>next ' + esc(when(s.next_run_at)) + '</span></span>' +
                '<span class="list-actions">' +
                '<button class="sm" data-sc-run="' + esc(s.id) + '" type="button">Run now</button>' +
                '<button class="sm" data-sc-edit="' + esc(s.id) + '" ' +
                'data-name="' + esc(s.name) + '" data-active="' + (s.is_active ? '1' : '0') + '" ' +
                'data-online="' + (s.only_when_online ? '1' : '0') + '" ' +
                'data-minute="' + esc(cron.minute) + '" data-hour="' + esc(cron.hour) + '" ' +
                'data-dom="' + esc(cron.day_of_month) + '" data-month="' + esc(cron.month) + '" ' +
                'data-dow="' + esc(cron.day_of_week) + '" type="button">Edit</button>' +
                '<button class="sm" data-sc-toggle="' + esc(s.id) + '" ' +
                'data-name="' + esc(s.name) + '" data-active="' + (s.is_active ? '1' : '0') + '" ' +
                'data-online="' + (s.only_when_online ? '1' : '0') + '" ' +
                'data-minute="' + esc(cron.minute) + '" data-hour="' + esc(cron.hour) + '" ' +
                'data-dom="' + esc(cron.day_of_month) + '" data-month="' + esc(cron.month) + '" ' +
                'data-dow="' + esc(cron.day_of_week) + '" type="button">' + (s.is_active ? 'Pause' : 'Resume') + '</button>' +
                '<button class="sm danger" data-sc-delete="' + esc(s.id) + '" data-name="' + esc(s.name) + '" type="button">Delete</button>' +
                '</span></div>' +
                taskRows(s) +
                '</div>';
        }).join('');
    });

    /* ------------------------------------------------------------- users */

    const loadUsers = pane('usersBody', async (target, api) => {
        const items = await api.ListSubusers();
        if (!items || !items.length) {
            return empty(target, 'No subusers', 'A subuser gets scoped access to this one server — you choose exactly which parts. The owner is not listed here.', 'users');
        }

        target.innerHTML = rows(items.map((u) => {
            const perms = Array.isArray(u.permissions) ? u.permissions : [];
            // Which areas they can reach, rather than a number that says
            // nothing. "file, backup" is the useful part of "7 permissions".
            const areas = Array.from(new Set(perms.map((p) => String(p).split('.')[0]))).sort();
            return '<div class="list-row">' +
                '<span class="list-badge on">' + icon('users', 'ic-14') + '</span>' +
                '<span class="list-main">' +
                '<span class="list-title">' + esc(u.username || u.email) +
                (u['2fa_enabled'] ? ' <span class="tag ok">2FA</span>' : '') + '</span>' +
                '<span class="list-sub">' + esc(u.email) +
                (areas.length ? ' · ' + esc(areas.join(', ')) : ' · no permissions') + '</span>' +
                '</span>' +
                '<span class="list-meta"><span>' + perms.length + ' permission' + (perms.length === 1 ? '' : 's') +
                '</span><span>' + esc(when(u.created_at)) + '</span></span>' +
                '<span class="list-actions">' +
                '<button class="sm" data-user-edit="' + esc(u.uuid) + '" data-name="' + esc(u.username || u.email) + '" ' +
                'data-perms="' + esc(perms.join(' ')) + '" type="button">Permissions</button>' +
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

        // A variable is a name, a description and a value with no upper bound
        // on its length — a block, not a row. As a row the value pushed the
        // name column down to nothing.
        const LONG = 180;

        html += '<div class="card">' + vars.map((v) => {
            const value = v.server_value === '' || v.server_value == null ? '' : String(v.server_value);
            const long = value.length > LONG;

            return '<div class="var-row">' +
                '<div class="var-head">' +
                '<span class="var-name">' + esc(v.name) +
                (v.is_editable ? '' : ' <span class="tag muted">Read only</span>') + '</span>' +
                '<span class="spacer"></span>' +
                (v.is_editable
                    ? '<button class="sm" data-var-key="' + esc(v.env_variable) +
                      '" data-var-name="' + esc(v.name) +
                      '" data-var-value="' + esc(value) +
                      '" data-var-rules="' + esc(v.rules || '') + '" type="button">Edit</button>'
                    : '') +
                '</div>' +
                '<div class="var-meta mono">' + esc(v.env_variable) +
                (v.description ? ' <span class="var-desc">' + esc(v.description) + '</span>' : '') + '</div>' +
                '<div class="var-value mono' + (long ? ' clipped' : '') + '">' +
                (value ? esc(value) : '<span class="var-empty">(empty)</span>') + '</div>' +
                (long ? '<button class="var-more" type="button" data-var-expand="1">Show all</button>' : '') +
                '</div>';
        }).join('') + '</div>';

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
            '<div class="eyebrow">Not available here</div>' +
            '<div class="pane-intro" style="margin:10px 0 0">Backups and Reinstall are deliberately left out of this app. ' +
            'A restore overwrites every file on the server and a reinstall wipes them on some eggs, and neither is ' +
            'recoverable from the local Vault. Both are still in the panel\'s own web UI.</div>' +
            '</div>';
    });

    /* ---------------------------------------------------------- registry */

    // No Backups entry: the backup routes are not in the Go layer at all, so a
    // restore cannot overwrite the server's files from here. Same for
    // Reinstall on the Settings tab.
    const LOADERS = {
        databases: loadDatabases,
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

    /** The five cron fields plus the two switches, in one dialog. */
    async function scheduleForm(title, current) {
        const D = window.Shell.dialog;
        const v = await D.form(title, [
            { name: 'name', label: 'Name', value: current.name, placeholder: 'Nightly restart' },
            { name: 'minute', label: 'Minute', value: current.minute, mono: true },
            { name: 'hour', label: 'Hour', value: current.hour, mono: true },
            { name: 'dom', label: 'Day of month', value: current.dom, mono: true },
            { name: 'month', label: 'Month', value: current.month, mono: true },
            { name: 'dow', label: 'Day of week', value: current.dow, mono: true,
              hint: 'Standard cron. <span class="mono">0 4 * * *</span> is 04:00 daily; ' +
                    '<span class="mono">*/15 * * * *</span> is every fifteen minutes.' },
            { name: 'active', label: 'Active', type: 'checkbox', value: current.active },
            { name: 'online', label: 'Only run while the server is online', type: 'checkbox', value: current.online }
        ], { confirmLabel: 'Save' });

        if (!v) return null;
        if (!String(v.name || '').trim()) {
            window.UX.toast.bad('A schedule needs a name');
            return null;
        }
        return v;
    }

    /** Action first, then the fields that action actually needs. */
    async function taskForm(title, current) {
        const D = window.Shell.dialog;
        const api = go();
        const actions = await api.ScheduleTaskActions();

        const action = await D.choose(title,
            current.action ? 'Currently: <span class="mono">' + esc(current.action) + '</span>' : '',
            actions.map((a) => ({ key: a.id, label: a.label, detail: a.hint })));
        if (!action) return null;

        const payloadField = action === 'command'
            ? { name: 'payload', label: 'Command', value: current.action === 'command' ? current.payload : '',
                mono: true, placeholder: 'say Restarting in 60 seconds' }
            : action === 'power'
                ? { name: 'payload', label: 'Signal', value: current.action === 'power' ? current.payload : 'restart',
                    mono: true, hint: 'start, stop, restart or kill.' }
                : { name: 'payload', label: 'Ignored paths', type: 'textarea',
                    value: current.action === 'backup' ? current.payload : '', mono: true,
                    placeholder: 'cache/\nlogs/', hint: 'One glob per line. Leave empty to back up everything.' };

        const v = await D.form(actions.find((a) => a.id === action).label, [
            payloadField,
            { name: 'offset', label: 'Wait before running', value: String(current.offset || 0), mono: true,
              hint: 'Seconds after the previous task in this schedule. The first task should be 0.' },
            { name: 'cont', label: 'Carry on if this task fails', type: 'checkbox', value: !!current.cont }
        ], { confirmLabel: 'Save task' });
        if (!v) return null;

        const offset = parseInt(v.offset, 10);
        if (isNaN(offset) || offset < 0) {
            window.UX.toast.bad('The wait has to be a number of seconds');
            return null;
        }
        return { action: action, payload: String(v.payload || '').trim(), offset: offset, cont: !!v.cont };
    }

    async function addTask(scheduleID) {
        const v = await taskForm('Add a task', { action: '', payload: '', offset: 0, cont: false });
        if (!v) return;
        try {
            await go().CreateScheduleTask(scheduleID, v.action, v.payload, v.offset, v.cont);
            reload('schedules');
        } catch (err) {
            window.Shell.dialog.confirm('Could not add the task',
                esc(String(err && err.message ? err.message : err)), { confirmLabel: 'OK' });
        }
    }

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

        /* schedules */
        if (el.id === 'newScheduleBtn') {
            const v = await scheduleForm('New schedule', {
                name: '', minute: '0', hour: '*', dom: '*', month: '*', dow: '*',
                active: true, online: false
            });
            if (!v) return;
            return guard(async () => {
                const made = await api.CreateSchedule(v.name, v.minute, v.hour, v.dom, v.month, v.dow,
                    v.active, v.online);
                window.UX.toast.ok('Created ' + v.name + ' — it has no tasks yet', {
                    action: made && made.id ? { label: 'Add a task', run: () => addTask(String(made.id)) } : null
                });
            }, 'schedules');
        }

        if (el.dataset.scEdit) {
            const d = el.dataset;
            const v = await scheduleForm('Edit ' + d.name, {
                name: d.name, minute: d.minute, hour: d.hour, dom: d.dom, month: d.month, dow: d.dow,
                active: d.active === '1', online: d.online === '1'
            });
            if (!v) return;
            return guard(() => api.UpdateSchedule(d.scEdit, v.name, v.minute, v.hour, v.dom, v.month, v.dow,
                v.active, v.online), 'schedules');
        }

        /* schedule tasks */
        if (el.dataset.taskNew) return addTask(el.dataset.taskNew);

        if (el.dataset.taskEdit) {
            const d = el.dataset;
            const v = await taskForm('Edit task', {
                action: d.action, payload: d.payload,
                offset: d.offset, cont: d.continue === '1'
            });
            if (!v) return;
            return guard(() => api.UpdateScheduleTask(d.schedule, d.taskEdit, v.action, v.payload,
                v.offset, v.cont), 'schedules');
        }

        if (el.dataset.taskDelete) {
            const ok = await D.confirm('Remove task',
                'The schedule keeps running; it just stops doing this one thing.',
                { danger: true, confirmLabel: 'Remove' });
            if (ok) guard(() => api.DeleteScheduleTask(el.dataset.schedule, el.dataset.taskDelete), 'schedules');
            return;
        }

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
        if (el.id === 'newSubuserBtn') {
            const who = await D.form('Invite a subuser', [
                { name: 'email', label: 'Email address', placeholder: 'someone@example.com', type: 'email',
                  hint: 'They need an account on this panel already; the invite links this server to it.' }
            ], { confirmLabel: 'Choose permissions' });
            if (!who || !who.email) return;

            const groups = await api.SubuserPermissions();
            // Read-only across the board is the least surprising starting
            // point: it is the set you can hand out without thinking.
            const defaults = ['control.console', 'file.read', 'file.read-content'];
            const perms = await D.checklist('Permissions for ' + esc(who.email), groups, defaults, {
                confirmLabel: 'Invite',
                intro: '<p class="form-hint" style="margin-bottom:12px">Everything is off unless you tick it. ' +
                       'The header checkbox in each group toggles the whole group.</p>'
            });
            if (!perms) return;

            return guard(async () => {
                await api.CreateSubuser(who.email, perms);
                window.UX.toast.ok('Invited ' + who.email + ' with ' + perms.length + ' permission(s)');
            }, 'users');
        }

        if (el.dataset.userEdit) {
            const groups = await api.SubuserPermissions();
            const current = String(el.dataset.perms || '').split(' ').filter(Boolean);
            const perms = await D.checklist('Permissions for ' + esc(el.dataset.name), groups, current, {
                confirmLabel: 'Save',
                intro: '<p class="form-hint" style="margin-bottom:12px">Unticking everything leaves them with ' +
                       'access to nothing; revoke them instead if that is what you mean.</p>'
            });
            if (!perms) return;

            return guard(async () => {
                await api.UpdateSubuser(el.dataset.userEdit, perms);
                window.UX.toast.ok(el.dataset.name + ' now has ' + perms.length + ' permission(s)');
            }, 'users');
        }

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
        if (el.dataset.varExpand) {
            const value = el.previousElementSibling;
            const open = value.classList.toggle('clipped');
            el.textContent = open ? 'Show all' : 'Show less';
            return;
        }
        if (el.dataset.varKey) {
            const current = el.dataset.varValue || '';
            // A single-line input for six hundred characters of JVM flags is
            // not an edit box, it is a keyhole.
            const long = current.length > 120 || current.indexOf('\n') !== -1;
            const v = await D.form('Edit ' + el.dataset.varName, [
                {
                    name: 'value', label: el.dataset.varKey, value: current, mono: true,
                    type: long ? 'textarea' : 'text', rows: 8,
                    hint: el.dataset.varRules ? 'Rules: <span class="mono">' + esc(el.dataset.varRules) + '</span>' : ''
                }
            ]);
            if (v) guard(() => api.SetStartupVariable(el.dataset.varKey, String(v.value).trim()), 'startup');
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

    });

    window.PanelTabs = { reload, loaders: Object.keys(LOADERS) };
})();
