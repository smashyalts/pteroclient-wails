/**
 * App shell: rail navigation, the panel/server chip menus, the breadcrumb
 * path bar, the live resource strip and a small dialog helper the panel tabs
 * share. main-editor.js still owns the file tree, editor and console; this
 * file only dresses the chrome around it and re-broadcasts tab changes so the
 * data tabs know when to load.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;

    /* ---------------------------------------------------------- utilities */

    function bytes(n) {
        const v = Number(n) || 0;
        if (v < 1024) return v + ' B';
        const units = ['KB', 'MB', 'GB', 'TB'];
        let size = v / 1024;
        let i = 0;
        while (size >= 1024 && i < units.length - 1) {
            size /= 1024;
            i++;
        }
        return (size >= 10 ? size.toFixed(0) : size.toFixed(1)) + ' ' + units[i];
    }

    function duration(ms) {
        const total = Math.floor((Number(ms) || 0) / 1000);
        if (total <= 0) return '—';
        const d = Math.floor(total / 86400);
        const h = Math.floor((total % 86400) / 3600);
        const m = Math.floor((total % 3600) / 60);
        if (d) return d + 'd ' + h + 'h';
        if (h) return h + 'h ' + m + 'm';
        return m + 'm';
    }

    /**
     * Safe in an attribute as well as in text.
     *
     * The textContent -> innerHTML trick escapes & < > and leaves quotes
     * alone, which is fine for text and wrong for the attributes most of this
     * app interpolates into — one double quote in a value and the rest of it
     * becomes markup.
     */
    function escapeHtml(value) {
        return String(value == null ? '' : value)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    /* ------------------------------------------------------------ dialogs */

    const Dialog = {
        _resolve: null,

        /**
         * A confirm button that has to be pressed twice.
         *
         * For something recoverable but still worth meaning: a stray click
         * cannot do it, and nobody has to type a word out. Typing is kept for
         * what the recycle bin cannot take back.
         */
        open(opts) {
            const modal = $('appDialog');

            // Only one dialog can be on screen, and only one _resolve can be
            // held. A second open() used to overwrite the first's resolve, so
            // whatever was awaiting it never settled again — and an awaited
            // confirm that never settles is an action that silently stalls
            // half-done. Settle the outgoing one as a cancel first.
            if (Dialog._resolve) {
                const stale = Dialog._resolve;
                Dialog._resolve = null;
                stale(null);
            }

            $('appDialogTitle').textContent = opts.title || 'Confirm';
            $('appDialogBody').innerHTML = opts.body || '';
            const confirmBtn = modal.querySelector('[data-dialog-confirm]');
            confirmBtn.textContent = opts.confirmLabel || 'Confirm';
            confirmBtn.className = opts.danger ? 'danger' : 'primary';

            // Reset whatever the last dialog left on it.
            if (Dialog._disarm) { clearTimeout(Dialog._disarm); Dialog._disarm = null; }
            delete confirmBtn.dataset.armed;
            delete confirmBtn.dataset.was;
            if (opts.confirmTwice) confirmBtn.dataset.twice = opts.confirmTwice;
            else delete confirmBtn.dataset.twice;
            modal.classList.add('show');

            const first = modal.querySelector('.modal-body input, .modal-body textarea');
            if (first) setTimeout(() => first.focus(), 20);

            return new Promise((resolve) => {
                Dialog._resolve = resolve;
            });
        },

        close(value) {
            $('appDialog').classList.remove('show');
            const resolve = Dialog._resolve;
            Dialog._resolve = null;
            if (resolve) resolve(value);
        },

        /** Yes/no. Resolves true or false. */
        confirm(title, message, opts) {
            const o = opts || {};
            return Dialog.open({
                title: title,
                body: '<p style="font-size:12.5px;line-height:1.6;color:var(--text-secondary)">' + message + '</p>',
                confirmLabel: o.confirmLabel,
                danger: o.danger
            }).then((v) => v !== null && v !== undefined && v !== false);
        },

        /**
         * Checklist. `groups` is [{group,hint,permissions:[{key,label}]}] and
         * `selected` the keys already on. Resolves an array of keys, or null.
         *
         * Built for the subuser permission set, which is fifty-odd keys in
         * nine groups — a form with fifty text fields was never going to be
         * the way to ask for that.
         */
        checklist(title, groups, selected, opts) {
            const o = opts || {};
            const on = new Set(selected || []);

            const body = (o.intro || '') + '<div class="checklist">' + groups.map((g) => (
                '<div class="checklist-group">' +
                '<div class="checklist-head">' +
                '<label class="check"><input type="checkbox" data-group-all="' + escapeHtml(g.group) + '">' +
                '<span class="eyebrow">' + escapeHtml(g.group) + '</span></label>' +
                (g.hint ? '<span class="checklist-hint">' + escapeHtml(g.hint) + '</span>' : '') +
                '</div>' +
                '<div class="checklist-items" data-group="' + escapeHtml(g.group) + '">' +
                g.permissions.map((perm) => (
                    '<label class="check"><input type="checkbox" data-perm="' + escapeHtml(perm.key) + '"' +
                    (on.has(perm.key) ? ' checked' : '') + '>' +
                    '<span>' + escapeHtml(perm.label) + '</span>' +
                    '<span class="check-key mono">' + escapeHtml(perm.key) + '</span></label>'
                )).join('') +
                '</div></div>'
            )).join('') + '</div>';

            return Dialog.open({
                title: title,
                body: body,
                confirmLabel: o.confirmLabel || 'Save'
            }).then((ok) => {
                if (!ok) return null;
                const out = [];
                document.querySelectorAll('#appDialogBody [data-perm]:checked').forEach((el) => {
                    out.push(el.getAttribute('data-perm'));
                });
                return out;
            });
        },

        /**
         * Multiple-choice. `choices` is [{key,label,detail,danger,primary}].
         * Resolves the chosen key, or null when cancelled.
         */
        choose(title, message, choices) {
            const modal = $('appDialog');
            const body = (message ? '<p style="font-size:12.5px;line-height:1.6;color:var(--text-secondary)">' +
                message + '</p>' : '') +
                '<div class="choice-list">' + choices.map((c) => (
                    '<button class="choice" type="button" data-choice="' + escapeHtml(c.key) + '"' +
                    (c.danger ? ' data-danger="1"' : '') + '>' +
                    '<span class="choice-label">' + escapeHtml(c.label) + '</span>' +
                    (c.detail ? '<span class="choice-detail">' + c.detail + '</span>' : '') +
                    '</button>'
                )).join('') + '</div>';

            $('appDialogTitle').textContent = title;
            $('appDialogBody').innerHTML = body;
            modal.classList.add('show');
            modal.querySelector('.modal-footer').style.display = 'none';

            return new Promise((resolve) => {
                if (Dialog._resolve) {
                    const stale = Dialog._resolve;
                    Dialog._resolve = null;
                    stale(null);
                }
                Dialog._resolve = (value) => {
                    modal.querySelector('.modal-footer').style.display = '';
                    resolve(value);
                };
            });
        },

        /** Form dialog. `fields` is [{name,label,placeholder,type,value}].
         *  `opts.intro` is HTML placed above the fields — used by the delete
         *  confirmation to show what is about to go before asking to type it.
         *  Resolves an object of values, or null when cancelled. */
        form(title, fields, opts) {
            const o = opts || {};
            const body = (o.intro || '') + fields.map((f) => {
                // A yes/no answer typed into a text box is a worse question
                // than a checkbox, and tasks need two of them.
                if (f.type === 'checkbox') {
                    return '<div class="form-group">' +
                        '<label class="check"><input type="checkbox" data-field="' + escapeHtml(f.name) + '"' +
                        (f.value ? ' checked' : '') + '><span>' + escapeHtml(f.label) + '</span></label>' +
                        (f.hint ? '<div class="form-hint">' + f.hint + '</div>' : '') +
                        '</div>';
                }
                if (f.type === 'textarea') {
                    return '<div class="form-group">' +
                        '<label>' + escapeHtml(f.label) + '</label>' +
                        '<textarea rows="' + (f.rows || 4) + '" class="' + (f.mono ? 'mono' : '') + '" ' +
                        'data-field="' + escapeHtml(f.name) + '" ' +
                        'placeholder="' + escapeHtml(f.placeholder || '') + '">' +
                        escapeHtml(f.value || '') + '</textarea>' +
                        (f.hint ? '<div class="form-hint">' + f.hint + '</div>' : '') +
                        '</div>';
                }
                return '<div class="form-group">' +
                    '<label>' + escapeHtml(f.label) + '</label>' +
                    '<input type="' + (f.type || 'text') + '" ' +
                    'class="' + (f.mono ? 'mono' : '') + '" ' +
                    'data-field="' + escapeHtml(f.name) + '" ' +
                    'value="' + escapeHtml(f.value || '') + '" ' +
                    'placeholder="' + escapeHtml(f.placeholder || '') + '">' +
                    (f.hint ? '<div class="form-hint">' + f.hint + '</div>' : '') +
                    '</div>';
            }).join('');

            return Dialog.open({
                title: title,
                body: body,
                confirmLabel: o.confirmLabel || 'Save',
                danger: o.danger
            }).then((ok) => {
                if (!ok) return null;
                const out = {};
                document.querySelectorAll('#appDialogBody [data-field]').forEach((el) => {
                    out[el.getAttribute('data-field')] = el.type === 'checkbox' ? el.checked : el.value;
                });
                return out;
            });
        }
    };

    function wireDialog() {
        const modal = $('appDialog');
        if (!modal) return;
        modal.querySelectorAll('[data-dialog-cancel]').forEach((el) => {
            el.addEventListener('click', () => Dialog.close(null));
        });
        modal.querySelector('[data-dialog-confirm]').addEventListener('click', (e) => {
            const btn = e.currentTarget;

            // A two-click confirm: the first press arms it, the second does it.
            // Enough that a stray click cannot delete anything, without making
            // people type a word out for something the recycle bin can undo.
            if (btn.dataset.twice && !btn.dataset.armed) {
                btn.dataset.armed = '1';
                btn.dataset.was = btn.textContent;
                btn.textContent = btn.dataset.twice;
                btn.classList.add('armed');
                // Disarms itself, so a dialog left open does not stay one
                // click from deleting.
                Dialog._disarm = setTimeout(() => {
                    if (!btn.dataset.armed) return;
                    delete btn.dataset.armed;
                    btn.textContent = btn.dataset.was || 'Confirm';
                    btn.classList.remove('armed');
                }, 4000);
                return;
            }
            Dialog.close(true);
        });
        modal.addEventListener('click', (e) => {
            const choice = e.target.closest('[data-choice]');
            if (choice) Dialog.close(choice.getAttribute('data-choice'));
        });

        modal.addEventListener('change', (e) => {
            const all = e.target.closest('[data-group-all]');
            if (!all) return;
            const group = all.getAttribute('data-group-all');
            const scope = modal.querySelector('[data-group="' + CSS.escape(group) + '"]');
            if (!scope) return;
            scope.querySelectorAll('[data-perm]').forEach((box) => { box.checked = all.checked; });
        });
        modal.addEventListener('click', (e) => {
            if (e.target === modal) Dialog.close(null);
        });
        modal.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && e.target.tagName === 'INPUT') {
                const btn = modal.querySelector('[data-dialog-confirm]');
                // Enter goes through the same arming, or a two-click confirm
                // would be one keypress after all.
                if (btn && btn.dataset.twice && !btn.dataset.armed) return btn.click();
                Dialog.close(true);
            }
        });
        // Escape cancels. Without this a dialog opened over a destructive
        // action has only one obvious way out, which is the confirm button.
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && modal.classList.contains('show')) Dialog.close(null);
        });
    }

    /* ---------------------------------------------------------------- rail */

    function paintRailIcons() {
        document.querySelectorAll('[data-icon]').forEach((el) => {
            if (el.querySelector('svg')) return;
            const label = el.textContent.trim();
            el.innerHTML = window.Icons.svg(el.getAttribute('data-icon'), 'ic-20') +
                (label ? '<span>' + escapeHtml(label) + '</span>' : '');
        });

        // Toolbar buttons that carry an icon but no data-icon attribute.
        const toolbarIcons = {
            filesRefreshBtn: 'refresh',
            filesUploadBtn: 'upload',
            filesNewFolderBtn: 'folderPlus',
            filesNewFileBtn: 'filePlus',
            filesDeleteBtn: 'trash',
            filesDownloadBtn: 'download',
            sftpBtn: 'plug',
            filesDetailsBtn: 'log',
            filesDockBtn: 'layout',
            splitViewBtn: 'split',
            refreshServersBtn: 'refresh',
            managePanelsBtn: 'sliders'
        };
        Object.keys(toolbarIcons).forEach((id) => {
            const el = $(id);
            if (!el || el.querySelector('svg')) return;
            const label = el.textContent.trim();
            el.innerHTML = window.Icons.svg(toolbarIcons[id]) + (label ? '<span>' + escapeHtml(label) + '</span>' : '');
        });
    }

    function wireRail() {
        const rail = $('rail');
        if (!rail) return;

        rail.addEventListener('click', (e) => {
            const btn = e.target.closest('.tab');
            if (!btn) return;
            showTab(btn.getAttribute('data-tab'));
        });
    }

    /**
     * Single source of truth for tab switching. main-editor.js delegates to
     * this (see its switchTab), so both the rail and any legacy call land here.
     */
    function showTab(name) {
        if (!name) return;

        document.querySelectorAll('.rail .tab').forEach((tab) => {
            tab.classList.toggle('active', tab.getAttribute('data-tab') === name);
        });
        document.querySelectorAll('.tab-content').forEach((panel) => {
            panel.classList.remove('active');
        });
        const panel = $(name + 'Tab');
        if (panel) panel.classList.add('active');

        document.dispatchEvent(new CustomEvent('tab:show', { detail: name }));
    }

    /* --------------------------------------------------------- chip menus */

    let openMenu = null;

    function closeMenu() {
        if (!openMenu) return;
        openMenu.menu.remove();
        openMenu.backdrop.remove();
        openMenu.chip.classList.remove('open');
        openMenu = null;
    }

    /**
     * Builds a menu from a hidden <select>, so main-editor.js keeps owning the
     * option list and the change handlers.
     */
    function openSelectMenu(chip, select, headLabel, extra) {
        closeMenu();

        const options = Array.from(select.options).filter((o) => o.value !== '');
        const menu = document.createElement('div');
        menu.className = 'menu';

        let html = '<div class="menu-head">' + escapeHtml(headLabel) + '</div>';

        if (!options.length) {
            html += '<div class="menu-row" style="cursor:default"><span class="menu-labels">' +
                '<span class="menu-title" style="color:var(--text-dim)">Nothing to choose yet</span></span></div>';
        }

        options.forEach((opt, i) => {
            const selected = opt.value === select.value;
            html += '<div class="menu-row' + (selected ? ' selected' : '') + '" data-value="' + escapeHtml(opt.value) + '" data-index="' + i + '">' +
                window.Icons.svg(headLabel === 'Panels' ? 'server' : 'terminal', 'ic-14') +
                '<span class="menu-labels">' +
                '<span class="menu-title">' + escapeHtml(opt.textContent) + '</span>' +
                (opt.dataset.sub ? '<span class="menu-sub">' + escapeHtml(opt.dataset.sub) + '</span>' : '') +
                '</span>' +
                (selected ? '<span class="check">' + window.Icons.svg('check', 'ic-14') + '</span>' : '') +
                '</div>';
        });

        if (extra) {
            html += '<div class="menu-sep"></div>' +
                '<div class="menu-row" data-extra="1">' + window.Icons.svg('sliders', 'ic-14') +
                '<span class="menu-labels"><span class="menu-title">' + escapeHtml(extra.label) + '</span></span></div>';
        }

        menu.innerHTML = html;

        const backdrop = document.createElement('div');
        backdrop.className = 'menu-backdrop';
        backdrop.addEventListener('click', closeMenu);

        menu.addEventListener('click', (e) => {
            const row = e.target.closest('.menu-row');
            if (!row) return;
            if (row.dataset.extra) {
                closeMenu();
                extra.run();
                return;
            }
            const value = row.getAttribute('data-value');
            if (value == null) return;
            closeMenu();
            select.value = value;
            select.dispatchEvent(new Event('change'));
        });

        chip.parentElement.appendChild(backdrop);
        chip.parentElement.appendChild(menu);
        chip.classList.add('open');
        openMenu = { chip, menu, backdrop };
    }

    function syncChips() {
        const panelSelect = $('panelDropdown');
        const serverSelect = $('serverDropdown');

        if (panelSelect) {
            const opt = panelSelect.selectedOptions[0];
            const name = opt && opt.value ? opt.textContent : 'No panel';
            $('panelChipName').textContent = name;
            $('panelChipUrl').textContent = (opt && opt.dataset.sub) || (opt && opt.value ? 'connected' : 'not connected');
        }

        if (serverSelect) {
            const opt = serverSelect.selectedOptions[0];
            $('serverChipName').textContent = opt && opt.value ? opt.textContent : 'No server';
            $('serverChipId').textContent = opt && opt.value ? opt.value : '—';
        }
    }

    function wireChips() {
        const panelSelect = $('panelDropdown');
        const serverSelect = $('serverDropdown');

        $('panelChip').addEventListener('click', () => {
            if (openMenu && openMenu.chip === $('panelChip')) return closeMenu();
            openSelectMenu($('panelChip'), panelSelect, 'Panels', {
                label: 'Manage panels…',
                run: () => window.app && window.app.showPanelManager()
            });
        });

        $('serverChip').addEventListener('click', () => {
            if (openMenu && openMenu.chip === $('serverChip')) return closeMenu();
            openSelectMenu($('serverChip'), serverSelect, 'Servers', null);
        });

        $('refreshServersBtn').addEventListener('click', () => window.app && window.app.loadServers());
        $('managePanelsBtn').addEventListener('click', () => window.app && window.app.showPanelManager());

        // The selects are repopulated by main-editor.js; mirror them whenever
        // they change or a switch completes.
        [panelSelect, serverSelect].forEach((sel) => {
            if (sel) sel.addEventListener('change', () => setTimeout(syncChips, 0));
        });
        document.addEventListener('shell:refresh', syncChips);
        setInterval(syncChips, 1500);
    }

    /* -------------------------------------------------------- breadcrumbs */

    function renderPath(path) {
        const bar = $('pathBar');
        if (!bar) return;

        const parts = String(path || '/').split('/').filter(Boolean);
        let html = window.Icons.svg('folder');
        html += '<span class="crumb" data-path="/">container</span>';

        let acc = '';
        parts.forEach((part, i) => {
            acc += '/' + part;
            const last = i === parts.length - 1;
            html += '<span class="crumb-sep">/</span>';
            html += '<span class="crumb' + (last ? ' current' : '') + '" data-path="' + escapeHtml(acc) + '">' + escapeHtml(part) + '</span>';
        });

        bar.innerHTML = html;
    }

    function wirePath() {
        const bar = $('pathBar');
        if (!bar) return;
        bar.addEventListener('click', (e) => {
            const crumb = e.target.closest('.crumb');
            if (!crumb || crumb.classList.contains('current')) return;
            if (window.app) window.app.loadFiles(crumb.getAttribute('data-path'));
        });
        document.addEventListener('path:changed', (e) => renderPath(e.detail));
        renderPath('/');
    }

    /* ----------------------------------------------------- resource strip */

    let resourceTimer = null;

    async function pollResources() {
        const api = go();
        if (!api || !api.GetServerResources) return;

        try {
            const res = await api.GetServerResources();
            $('resCpu').textContent = (Number(res.cpu_absolute) || 0).toFixed(1) + '%';
            $('resMem').textContent = bytes(res.memory_bytes);
            $('resDisk').textContent = bytes(res.disk_bytes);
            $('resUptime').textContent = duration(res.uptime);
            setState(res.current_state);
        } catch (err) {
            $('resCpu').textContent = '—';
            $('resMem').textContent = '—';
            $('resDisk').textContent = '—';
            $('resUptime').textContent = '—';
        }
    }

    function setState(state) {
        const pill = $('statusIndicator');
        const text = $('statusText');
        const dot = $('serverChipDot');
        if (!pill) return;

        pill.classList.remove('state-running', 'state-starting', 'state-stopping');
        const known = ['running', 'starting', 'stopping'];
        if (known.indexOf(state) !== -1) pill.classList.add('state-' + state);
        if (text) text.textContent = state ? state : 'offline';
        if (dot) {
            dot.style.background = state === 'running' ? 'var(--success)'
                : (state === 'starting' || state === 'stopping') ? 'var(--warning)'
                    : 'var(--danger)';
        }
    }

    function startResourcePolling() {
        if (resourceTimer) clearInterval(resourceTimer);
        pollResources();
        resourceTimer = setInterval(pollResources, 5000);
    }

    /* --------------------------------------------------------------- boot */

    function boot() {
        paintRailIcons();
        wireRail();
        wireChips();
        wirePath();
        wireDialog();
        syncChips();
        startResourcePolling();

        if (window.runtime && window.runtime.EventsOn) {
            window.runtime.EventsOn('server-changed', () => {
                syncChips();
                pollResources();
                document.dispatchEvent(new CustomEvent('tab:show', { detail: currentTab() }));
            });
        }
    }

    function currentTab() {
        const active = document.querySelector('.rail .tab.active');
        return active ? active.getAttribute('data-tab') : 'console';
    }

    window.Shell = {
        showTab,
        currentTab,
        renderPath,
        syncChips,
        setState,
        pollResources,
        dialog: Dialog,
        fmt: { bytes, duration, escapeHtml }
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', boot);
    } else {
        boot();
    }
})();
