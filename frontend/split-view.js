/**
 * Split view: two independent workspaces side by side.
 *
 * Each one is a small copy of the whole file tab — its own server, its own
 * explorer, its own set of open tabs, its own editor — so working on two
 * servers at once means two windows rather than two text boxes sharing one
 * tree. Servers may be on different panels; the *FromServer bindings resolve
 * which panel each belongs to without disturbing the server the rest of the app
 * is pointed at.
 *
 * Files drag from one workspace's explorer into the other's, which copies them
 * across — including across panels. The bytes are moved by the Go side, never
 * through the frontend, so binaries survive the trip.
 *
 * Layout: the divider between the two workspaces drags, and inside each one the
 * explorer can sit beside the editor or under it, with its own divider.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const icon = (n, c) => window.Icons.svg(n, c);
    const SIDES = ['left', 'right'];

    let active = false;
    let servers = [];
    let focused = 'left';
    const spaces = {};

    // One editor per workspace, from the shared kit: Monaco when it loaded,
    // a textarea with the same interface when it did not.
    const editors = {};

    // Outer split as flex-grow for [left, right].
    let outer = [50, 50];

    function space(side) {
        if (!spaces[side]) {
            spaces[side] = {
                side: side,
                serverID: '', serverName: '', panel: '',
                path: '/',
                tabs: new Map(),      // path -> {name, content, original, dirty}
                activeTab: null,
                layout: 'side',       // 'side' | 'stacked'
                explorerSize: 32      // percent of the workspace
            };
        }
        return spaces[side];
    }

    /* -------------------------------------------------------------- layout */

    function loadLayout() {
        try {
            const stored = JSON.parse(localStorage.getItem('splitLayout2') || 'null');
            if (!stored) return;
            if (Array.isArray(stored.outer) && stored.outer.length === 2) outer = stored.outer;
            SIDES.forEach((side) => {
                const saved = stored[side];
                if (!saved) return;
                const st = space(side);
                if (saved.layout === 'side' || saved.layout === 'stacked') st.layout = saved.layout;
                if (typeof saved.explorerSize === 'number') st.explorerSize = saved.explorerSize;
            });
        } catch (err) { /* private mode */ }
    }

    function saveLayout() {
        try {
            localStorage.setItem('splitLayout2', JSON.stringify({
                outer: outer,
                left: { layout: space('left').layout, explorerSize: space('left').explorerSize },
                right: { layout: space('right').layout, explorerSize: space('right').explorerSize }
            }));
        } catch (err) { /* private mode */ }
    }

    function applyLayout() {
        const root = $('splitRoot');
        if (!root) return;

        SIDES.forEach((side, i) => {
            const el = root.querySelector('[data-space="' + side + '"]');
            if (!el) return;
            el.style.flexGrow = outer[i];

            const st = space(side);
            el.classList.toggle('stacked', st.layout === 'stacked');
            const explorer = el.querySelector('.ws-explorer');
            if (explorer) explorer.style.flexBasis = st.explorerSize + '%';
        });
    }

    /* -------------------------------------------------------------- markup */

    function workspaceMarkup(side) {
        return '' +
            '<section class="ws" data-space="' + side + '" data-side="' + side + '">' +
            '  <header class="ws-head">' +
            '    <span class="ws-focus" title="The focused workspace takes the keyboard"></span>' +
            '    <select data-role="server" class="ws-server"><option value="">Choose a server…</option></select>' +
            '    <span class="spacer"></span>' +
            '    <button class="sm icon-only" data-role="wslayout" title="Explorer beside / below the editor"></button>' +
            '  </header>' +
            '  <div class="ws-body">' +
            '    <div class="ws-explorer">' +
            '      <div class="ws-path mono" data-role="path">/</div>' +
            '      <div class="filter-bar sm"><input type="text" data-role="filter" autocomplete="off" ' +
            '        spellcheck="false" placeholder="Filter…"></div>' +
            '      <div class="ws-list" data-role="list"></div>' +
            '    </div>' +
            '    <div class="ws-grip" data-innergrip="' + side + '"></div>' +
            '    <div class="ws-editor">' +
            '      <div class="ws-tabs" data-role="tabs"></div>' +
            '      <div class="ws-code" data-role="editorhost"></div>' +
            '      <div class="ws-foot">' +
            '        <span class="ws-file mono" data-role="filename">no file open</span>' +
            '        <span class="spacer"></span>' +
            '        <button class="sm primary" data-role="save" type="button" disabled>Save</button>' +
            '      </div>' +
            '    </div>' +
            '  </div>' +
            '</section>';
    }

    function rootMarkup() {
        return workspaceMarkup('left') +
            '<div class="split-grip" data-outergrip="1"></div>' +
            workspaceMarkup('right');
    }

    /* ------------------------------------------------------------- servers */

    async function loadServers() {
        const api = go();
        if (!api || !api.ListAllServers) return [];
        try {
            servers = (await api.ListAllServers()) || [];
        } catch (err) {
            servers = [];
        }
        return servers;
    }

    function fillServerSelects() {
        const byPanel = {};
        servers.forEach((srv) => { (byPanel[srv.panel] = byPanel[srv.panel] || []).push(srv); });

        document.querySelectorAll('#splitRoot [data-role="server"]').forEach((sel) => {
            const current = sel.value;
            let html = '<option value="">Choose a server…</option>';
            Object.keys(byPanel).forEach((panel) => {
                html += '<optgroup label="' + esc(panel) + '">';
                byPanel[panel].forEach((srv) => {
                    html += '<option value="' + esc(srv.id) + '" data-panel="' + esc(panel) + '">' +
                        esc(srv.name) + '</option>';
                });
                html += '</optgroup>';
            });
            sel.innerHTML = html;
            if (current) sel.value = current;
        });
    }

    /* ------------------------------------------------------------ explorer */

    function el(side, selector) {
        const root = document.querySelector('#splitRoot [data-space="' + side + '"]');
        return root ? root.querySelector(selector) : null;
    }

    async function browse(side, path) {
        const st = space(side);
        const list = el(side, '[data-role="list"]');
        const crumb = el(side, '[data-role="path"]');
        if (!list) return;

        if (!st.serverID) {
            crumb.textContent = '/';
            list.innerHTML = '<div class="preview-empty">Pick a server</div>';
            return;
        }

        const target = path === undefined ? (st.path || '/') : path;
        crumb.textContent = (st.panel ? st.panel + ' · ' : '') + target;
        list.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '</div>';

        let files;
        try {
            files = await go().ListFilesFromServer(st.serverID, target);
        } catch (err) {
            list.innerHTML = '<div class="error">' + esc(String(err)) + '</div>';
            return;
        }

        st.path = target;
        if (window.Folders) window.Folders.remember(st.serverID, target, files);

        (files || []).sort((a, b) => {
            if (a.isDir !== b.isDir) return b.isDir ? 1 : -1;
            return a.name.localeCompare(b.name);
        });

        let html = '';
        if (target !== '/') {
            html += '<div class="file-item" data-updir="1">' +
                '<span class="file-icon kind-dir">' + icon('folder') + '</span>' +
                '<span class="file-name">..</span></div>';
        }
        (files || []).forEach((f) => {
            const isDir = !!f.isDir;
            const when = window.app ? window.app.formatWhen(f.modTime) : '';
            const whenFull = window.app ? window.app.formatWhenFull(f.modTime) : '';
            html += '<div class="file-item" draggable="' + (isDir ? 'false' : 'true') + '" ' +
                'data-name="' + esc(f.name) + '" data-dir="' + (isDir ? '1' : '') + '">' +
                '<span class="file-icon kind-' + window.Icons.kindFor(f.name, isDir) + '">' +
                window.Icons.forFile(f.name, isDir) + '</span>' +
                '<span class="file-name" title="' + esc(f.name) + '">' + esc(f.name) + '</span>' +
                '<span class="file-size">' + (isDir ? '' : window.Shell.fmt.bytes(f.size)) + '</span>' +
                '<span class="file-date" title="' + esc(whenFull) + '">' + esc(when) + '</span>' +
                '<span class="file-mode">' + esc(f.mode || '') + '</span></div>';
        });

        list.innerHTML = html || '<div class="preview-empty">Empty folder</div>';

        // Browsing elsewhere clears the box, or the new folder comes up
        // looking empty because of a word typed in the old one.
        const box = el(side, '[data-role="filter"]');
        if (box) box.value = '';
        applyFilter(side, '');
    }

    /** Hides rows in one workspace that do not match. `..` always stays. */
    function applyFilter(side, query) {
        const needle = String(query || '').trim().toLowerCase();
        const list = el(side, '[data-role="list"]');
        if (!list) return;

        let shown = 0;
        list.querySelectorAll('.file-item').forEach((row) => {
            if (row.dataset.updir) return;
            const hit = !needle || String(row.dataset.name || '').toLowerCase().indexOf(needle) !== -1;
            row.hidden = !hit;
            if (hit) shown++;
        });

        let none = list.querySelector('.ws-filter-empty');
        if (needle && shown === 0) {
            if (!none) {
                none = document.createElement('div');
                none.className = 'preview-empty ws-filter-empty';
                list.appendChild(none);
            }
            none.textContent = 'Nothing here matches "' + query + '"';
            none.hidden = false;
        } else if (none) {
            none.hidden = true;
        }
    }

    /** Ctrl+F lands in the focused workspace's box. */
    function focusFilter() {
        const box = el(focused, '[data-role="filter"]');
        if (!box) return;
        box.focus();
        box.select();
    }

    function buildEditor(side) {
        const host = el(side, '[data-role="editorhost"]');
        if (!host) return null;

        editors[side] = window.MonacoKit.createEditor(host, {
            value: '',
            language: 'plaintext',
            placeholder: 'Pick a server, then open a file from the explorer.',
            onChange: (value) => {
                const st = space(side);
                if (!st.activeTab) return;
                const tab = st.tabs.get(st.activeTab);
                if (!tab) return;

                tab.content = value;
                const wasDirty = tab.dirty;
                tab.dirty = tab.content !== tab.original;
                if (wasDirty !== tab.dirty) renderTabs(side);
                refreshSave(side);
            }
        });
        return editors[side];
    }

    function editor(side) {
        return editors[side] || buildEditor(side);
    }

    /* ---------------------------------------------------------------- tabs */

    function renderTabs(side) {
        const st = space(side);
        const bar = el(side, '[data-role="tabs"]');
        if (!bar) return;

        if (!st.tabs.size) {
            bar.innerHTML = '<span class="ws-tabs-empty">no files open</span>';
            return;
        }

        let html = '';
        st.tabs.forEach((tab, path) => {
            html += '<div class="ws-tab' + (path === st.activeTab ? ' active' : '') +
                (tab.dirty ? ' modified' : '') + '" data-tab="' + esc(path) + '" title="' + esc(path) + '">' +
                '<span class="ws-tab-name">' + esc(tab.name) + '</span>' +
                '<span class="ws-tab-close" data-close="' + esc(path) + '">&times;</span></div>';
        });
        bar.innerHTML = html;
    }

    function showTab(side, path) {
        const st = space(side);
        const tab = st.tabs.get(path);
        if (!tab) return;

        st.activeTab = path;
        // A model per file, so switching tabs keeps each one's undo history and
        // cursor instead of resetting them.
        editor(side).setModel(path, tab.content, window.MonacoKit.languageFor(tab.name));
        el(side, '[data-role="filename"]').textContent = path;
        renderTabs(side);
        refreshSave(side);
    }

    async function closeTab(side, path) {
        const st = space(side);
        const tab = st.tabs.get(path);
        if (!tab) return;

        if (tab.dirty) {
            const ok = await window.Shell.dialog.confirm('Unsaved changes',
                'Close <span class="mono">' + esc(path) + '</span> and lose its changes?',
                { danger: true, confirmLabel: 'Close anyway' });
            if (!ok) return;
        }

        st.tabs.delete(path);
        editor(side).dropModel(path);

        if (st.activeTab === path) {
            const next = st.tabs.keys().next();
            st.activeTab = next.done ? null : next.value;
        }

        if (st.activeTab) {
            showTab(side, st.activeTab);
        } else {
            editor(side).setValue('');
            el(side, '[data-role="filename"]').textContent = 'no file open';
            renderTabs(side);
            refreshSave(side);
        }
        window.Session && window.Session.save();
    }

    async function openFile(side, name) {
        const st = space(side);
        const full = st.path === '/' ? '/' + name : st.path + '/' + name;

        if (st.tabs.has(full)) return showTab(side, full);

        try {
            // Same guard as the main editor: a binary or oversized file loaded
            // into a textarea comes back mangled on save.
            const read = await go().ReadFileForEdit(st.serverID, full);
            if (read.binary || read.too_big) {
                window.UX.toast.warn(read.binary
                    ? name + ' is binary; editing it here would corrupt it'
                    : name + ' is over the 8 MB editor limit');
                return;
            }
            st.tabs.set(full, { name: name, content: read.content, original: read.content, dirty: false });
            showTab(side, full);
            window.Session && window.Session.save();
        } catch (err) {
            window.UX.toast.bad(String(err));
        }
    }

    function refreshSave(side) {
        const st = space(side);
        const btn = el(side, '[data-role="save"]');
        if (!btn) return;
        const tab = st.activeTab ? st.tabs.get(st.activeTab) : null;
        btn.disabled = !tab || !tab.dirty;
    }

    /**
     * Writes the focused tab back to its server.
     *
     * Goes through SafeSaveFileContentToServer, so the state being replaced is
     * filed in the local history first and a file that changed on the panel
     * since this tab opened it is refused rather than clobbered. Both
     * workspaces can hold the same file on the same server, and saving one
     * would otherwise wipe the other's work.
     */
    async function save(side, force) {
        const st = space(side);
        if (!st.serverID || !st.activeTab) return;

        const tab = st.tabs.get(st.activeTab);
        if (!tab) return;

        const btn = el(side, '[data-role="save"]');
        btn.disabled = true;
        btn.textContent = 'Saving…';

        try {
            // Read straight from the editor rather than trusting the cached
            // copy: with Monaco the two are kept in step by onChange, and a
            // dropped event would otherwise save stale text.
            tab.content = editor(side).getValue();

            const res = await go().SafeSaveFileContentToServer(
                st.serverID, st.activeTab, tab.content, tab.original, !!force);

            if (res && res.conflict) {
                btn.textContent = 'Save';
                btn.disabled = false;

                let diff = '';
                if (res.remote_content && window.Vault && window.Vault.diffHtml) {
                    diff = '<div style="margin-top:12px">' +
                        window.Vault.diffHtml(res.remote_content, tab.content) + '</div>';
                }

                const ok = await window.Shell.dialog.confirm('The panel copy changed',
                    '<span class="mono">' + esc(st.activeTab) + '</span> ' + esc(res.reason) + '.' +
                    '<br><br>Saving now replaces it with what is in this workspace. The panel copy is filed in ' +
                    'the local history first, so it can be restored from Vault.' + diff,
                    { danger: true, confirmLabel: 'Overwrite anyway' });
                if (!ok) return;
                return save(side, true);
            }

            tab.original = tab.content;
            tab.dirty = false;
            renderTabs(side);
            btn.textContent = 'Saved';
            setTimeout(() => { btn.textContent = 'Save'; refreshSave(side); }, 1100);
            window.Session && window.Session.save();
        } catch (err) {
            btn.textContent = 'Save';
            btn.disabled = false;
            window.UX.toast.bad('Save failed: ' + err);
        }
    }

    function saveAll() {
        SIDES.forEach((side) => {
            const st = space(side);
            if (st.activeTab && st.tabs.get(st.activeTab) && st.tabs.get(st.activeTab).dirty) save(side);
        });
    }

    /* --------------------------------------------------------------- focus */

    function setFocus(side) {
        focused = side;
        document.querySelectorAll('#splitRoot .ws').forEach((ws) => {
            ws.classList.toggle('focused', ws.getAttribute('data-side') === side);
        });
    }

    /* ------------------------------------------------------- server change */

    /** The deepest part of `path` that exists on `serverID`, or "/". */
    async function nearestPath(serverID, path) {
        const parts = String(path || '/').split('/').filter(Boolean);
        while (parts.length) {
            const candidate = '/' + parts.join('/');
            try {
                await go().ListFilesFromServer(serverID, candidate);
                return candidate;
            } catch (err) {
                parts.pop();
            }
        }
        return '/';
    }

    async function setServer(side, id, name, panel) {
        const st = space(side);

        const dirty = Array.from(st.tabs.values()).some(t => t.dirty);
        if (dirty) {
            const ok = await window.Shell.dialog.confirm('Unsaved changes',
                'The ' + side + ' workspace has unsaved files. Switching servers closes them.',
                { danger: true, confirmLabel: 'Discard and switch' });
            if (!ok) return false;
        }

        // Carried across the switch, the same way the main explorer does it.
        const wasAt = st.path || '/';

        st.serverID = id;
        st.serverName = name || '';
        st.panel = panel || '';
        st.path = id ? await nearestPath(id, wasAt) : '/';
        st.tabs.forEach((_tab, path) => editor(side).dropModel(path));
        st.tabs.clear();
        st.activeTab = null;

        editor(side).setValue('');
        el(side, '[data-role="filename"]').textContent = 'no file open';
        renderTabs(side);
        refreshSave(side);
        await browse(side, st.path);
        window.Session && window.Session.save();
        return true;
    }

    /* ------------------------------------------------------------ resizing */

    function dragOuter(startEvent) {
        const root = $('splitRoot');
        const rect = root.getBoundingClientRect();
        const startX = startEvent.clientX;
        const startLeft = outer[0];
        const total = outer[0] + outer[1];
        const perPx = total / rect.width;

        function onMove(e) {
            const next = Math.max(10, Math.min(total - 10, startLeft + (e.clientX - startX) * perPx));
            outer = [next, total - next];
            applyLayout();
        }
        function onUp() {
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
            document.body.classList.remove('resizing');
            saveLayout();
        }
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
        document.body.classList.add('resizing');
        startEvent.preventDefault();
    }

    function dragInner(side, startEvent) {
        const st = space(side);
        const body = el(side, '.ws-body');
        const rect = body.getBoundingClientRect();
        const vertical = st.layout === 'stacked';
        const start = vertical ? startEvent.clientY : startEvent.clientX;
        const span = vertical ? rect.height : rect.width;
        const startSize = st.explorerSize;

        function onMove(e) {
            const now = vertical ? e.clientY : e.clientX;
            const delta = ((now - start) / span) * 100;
            // 10%..85% keeps both halves grabbable; the old floor was so high
            // the explorer barely moved.
            st.explorerSize = Math.max(10, Math.min(85, startSize + delta));
            applyLayout();
        }
        function onUp() {
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
            document.body.classList.remove('resizing');
            saveLayout();
        }
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
        document.body.classList.add('resizing');
        startEvent.preventDefault();
    }

    /* -------------------------------------------------------- drag betweeen */

    let dragging = null;

    function wireDragAndDrop(root) {
        root.addEventListener('dragstart', (e) => {
            const row = e.target.closest('.ws-list .file-item');
            if (!row || row.dataset.dir || row.dataset.updir) return;

            const side = row.closest('.ws').getAttribute('data-side');
            const st = space(side);
            dragging = {
                side: side,
                serverID: st.serverID,
                path: st.path === '/' ? '/' + row.dataset.name : st.path + '/' + row.dataset.name,
                name: row.dataset.name
            };
            e.dataTransfer.effectAllowed = 'copy';
            e.dataTransfer.setData('text/plain', dragging.path);
            row.classList.add('dragging');
        });

        root.addEventListener('dragend', (e) => {
            const row = e.target.closest('.file-item');
            if (row) row.classList.remove('dragging');
            root.querySelectorAll('.ws-explorer.drop-target').forEach(x => x.classList.remove('drop-target'));
            dragging = null;
        });

        root.addEventListener('dragover', (e) => {
            if (!dragging) return;
            const explorer = e.target.closest('.ws-explorer');
            if (!explorer) return;
            const side = explorer.closest('.ws').getAttribute('data-side');
            // Dropping a file back where it came from is a no-op, so it is not
            // offered as one.
            if (side === dragging.side) return;
            if (!space(side).serverID) return;
            e.preventDefault();
            e.dataTransfer.dropEffect = 'copy';
            explorer.classList.add('drop-target');
        });

        root.addEventListener('dragleave', (e) => {
            const explorer = e.target.closest('.ws-explorer');
            if (explorer && !explorer.contains(e.relatedTarget)) explorer.classList.remove('drop-target');
        });

        root.addEventListener('drop', async (e) => {
            const explorer = e.target.closest('.ws-explorer');
            if (!explorer || !dragging) return;
            const side = explorer.closest('.ws').getAttribute('data-side');
            if (side === dragging.side) return;

            e.preventDefault();
            explorer.classList.remove('drop-target');

            const target = space(side);
            const from = dragging;
            dragging = null;

            const busy = window.UX.toast.show('Copying ' + from.name + '…', { duration: 60000 });
            try {
                let res = await go().CopyFileBetweenServers(
                    from.serverID, from.path, target.serverID, target.path, false);

                if (res.conflict) {
                    busy.dismiss();
                    const ok = await window.Shell.dialog.confirm('Replace ' + esc(from.name) + '?',
                        esc(res.reason) + '.<br><br>The file being replaced goes to the recycle bin first.',
                        { danger: true, confirmLabel: 'Replace' });
                    if (!ok) return;
                    res = await go().CopyFileBetweenServers(
                        from.serverID, from.path, target.serverID, target.path, true);
                } else {
                    busy.dismiss();
                }

                await browse(side);
                window.UX.toast.ok((res.replaced ? 'Replaced ' : 'Copied ') + from.name + ' → ' + res.path);
            } catch (err) {
                busy.dismiss();
                window.UX.toast.bad('Copy failed: ' + err);
            }
        });
    }

    /**
     * Puts the left workspace on whatever the main window is showing, without
     * waiting for anything.
     *
     * The right one is deliberately left empty: opening a split on two copies
     * of the same server is rarely what was wanted, and picking the second is
     * one click.
     */
    function seedFromMainWindow() {
        const source = $('serverDropdown');
        if (!source || !source.value) return false;

        const id = source.value;
        const name = (source.selectedOptions[0] && source.selectedOptions[0].textContent) || id;
        const panel = (function () {
            const panels = $('panelDropdown');
            if (!panels) return '';
            const opt = panels.selectedOptions && panels.selectedOptions[0];
            return opt ? opt.textContent : '';
        }());

        // A stand-in option so the name is on screen now rather than when the
        // list arrives. fillServerSelects keeps the value it finds.
        const sel = document.querySelector('#splitRoot [data-space="left"] [data-role="server"]');
        if (sel) {
            sel.innerHTML = '<option value="' + esc(id) + '">' + esc(name) + '</option>';
            sel.value = id;
        }

        // Where the main explorer is, not the root: the split opens on the
        // folder that was being looked at.
        const st = space('left');
        if (window.app && window.app.currentPath) st.path = window.app.currentPath;

        setServer('left', id, name, panel);
        return true;
    }

    /* -------------------------------------------------------------- toggle */

    function open() {
        const manager = document.querySelector('.file-manager');
        // The whole pane, not just the tree inside it. Hiding only .file-tree
        // left the pane's filter bar and its resize grip on screen next to the
        // two workspaces, so the window appeared to have three explorers.
        const pane = document.querySelector('.file-manager > .file-pane');
        const grip = document.getElementById('fileGrip');
        const editor = document.querySelector('.editor-container');
        if (!manager || !editor) return;

        loadLayout();

        if (pane) pane.style.display = 'none';
        if (grip) grip.style.display = 'none';
        editor.style.display = 'none';
        manager.classList.add('split-active');

        const root = document.createElement('div');
        root.id = 'splitRoot';
        root.innerHTML = rootMarkup();
        manager.appendChild(root);

        root.querySelectorAll('[data-role="wslayout"]').forEach((btn) => {
            btn.innerHTML = window.Icons.svg('layout');
        });

        applyLayout();
        wireDragAndDrop(root);
        SIDES.forEach((side) => { buildEditor(side); renderTabs(side); });
        setFocus('left');

        // Straight away, from what the main window already has on screen. The
        // server list is a network call, and until it landed both dropdowns
        // read "Choose a server..." — which is what looked like the split
        // closing every server on the way in.
        const seeded = seedFromMainWindow();

        loadServers().then(async () => {
            fillServerSelects();
            // await, not truthiness: restoreSplit is async, so the bare call
            // returned a promise, which is always truthy, and the seeding below
            // never ran — the left workspace opened empty every time.
            if (window.Session && await window.Session.restoreSplit()) return;
            if (seeded) return;

            // Nothing on screen to seed from and nothing to restore.
            const api = go();
            if (!api || !api.GetConfig) return;
            api.GetConfig().then((cfg) => {
                if (!cfg || !cfg.serverID) return;
                const sel = root.querySelector('[data-space="left"] [data-role="server"]');
                sel.value = cfg.serverID;
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }).catch(() => {});
        });

        const btn = $('splitViewBtn');
        if (btn) btn.classList.add('toggled');
        active = true;
    }

    async function close() {
        for (const side of SIDES) {
            const dirty = Array.from(space(side).tabs.values()).some(t => t.dirty);
            if (!dirty) continue;
            const ok = await window.Shell.dialog.confirm('Unsaved changes',
                'The ' + side + ' workspace has unsaved files. Closing the split view loses them.',
                { danger: true, confirmLabel: 'Discard them' });
            if (!ok) return;
        }

        SIDES.forEach((side) => {
            if (!editors[side]) return;
            editors[side].dispose();
            delete editors[side];
        });

        const root = $('splitRoot');
        if (root) root.remove();

        const manager = document.querySelector('.file-manager');
        const pane = document.querySelector('.file-manager > .file-pane');
        const grip = document.getElementById('fileGrip');
        const editor = document.querySelector('.editor-container');
        if (manager) manager.classList.remove('split-active');
        if (pane) pane.style.display = '';
        if (grip) grip.style.display = '';
        if (editor) editor.style.display = '';

        const btn = $('splitViewBtn');
        if (btn) btn.classList.remove('toggled');
        active = false;
        window.Session && window.Session.save();
    }

    function toggle() {
        if (active) return close();
        open();
    }

    /* -------------------------------------------------------------- events */

    document.addEventListener('mousedown', (e) => {
        if (!active) return;
        const outerGrip = e.target.closest('#splitRoot [data-outergrip]');
        if (outerGrip) return dragOuter(e);
        const innerGrip = e.target.closest('#splitRoot [data-innergrip]');
        if (innerGrip) return dragInner(innerGrip.dataset.innergrip, e);
    });

    document.addEventListener('click', async (e) => {
        if (e.target.closest('#splitViewBtn')) {
            e.preventDefault();
            return toggle();
        }
        if (!active) return;

        const ws = e.target.closest('#splitRoot .ws');
        if (!ws) return;
        const side = ws.getAttribute('data-side');
        if (side !== focused) setFocus(side);

        const btn = e.target.closest('button');
        if (btn && btn.dataset.role === 'save') return save(side);
        if (btn && btn.dataset.role === 'wslayout') {
            const st = space(side);
            st.layout = st.layout === 'side' ? 'stacked' : 'side';
            applyLayout();
            saveLayout();
            return;
        }

        const close_ = e.target.closest('.ws-tab-close');
        if (close_) {
            e.stopPropagation();
            return closeTab(side, close_.getAttribute('data-close'));
        }

        const tab = e.target.closest('.ws-tab');
        if (tab) return showTab(side, tab.getAttribute('data-tab'));

        const row = e.target.closest('.ws-list .file-item');
        if (!row) return;

        const st = space(side);
        if (row.dataset.updir) {
            const parts = String(st.path).split('/').filter(Boolean);
            parts.pop();
            return browse(side, parts.length ? '/' + parts.join('/') : '/');
        }
        if (row.dataset.dir) {
            return browse(side, st.path === '/' ? '/' + row.dataset.name : st.path + '/' + row.dataset.name);
        }
        return openFile(side, row.dataset.name);
    });

    // Middle-click closes a workspace tab, same as the main editor's.
    document.addEventListener('auxclick', (e) => {
        if (!active || e.button !== 1) return;
        const tab = e.target.closest('#splitRoot .ws-tab');
        if (!tab) return;
        e.preventDefault();
        closeTab(tab.closest('.ws').getAttribute('data-side'), tab.getAttribute('data-tab'));
    });

    document.addEventListener('change', async (e) => {
        if (!active) return;
        const sel = e.target.closest('#splitRoot [data-role="server"]');
        if (!sel) return;

        const side = sel.closest('.ws').getAttribute('data-side');
        const opt = sel.selectedOptions[0];
        const ok = await setServer(side, sel.value, opt ? opt.textContent : '', opt ? opt.dataset.panel : '');
        if (!ok) sel.value = space(side).serverID;
        setFocus(side);
    });

    document.addEventListener('input', (e) => {
        if (!active) return;
        const box = e.target.closest('#splitRoot [data-role="filter"]');
        if (!box) return;
        applyFilter(box.closest('.ws').getAttribute('data-side'), box.value);
    });

    // Capture, so Escape clears the box before the hotkey manager reads it as
    // "clear the file selection".
    document.addEventListener('keydown', (e) => {
        if (!active || e.key !== 'Escape') return;
        const box = e.target.closest && e.target.closest('#splitRoot [data-role="filter"]');
        if (!box) return;
        e.stopPropagation();
        box.value = '';
        applyFilter(box.closest('.ws').getAttribute('data-side'), '');
        box.blur();
    }, true);

    document.addEventListener('focusin', (e) => {
        if (!active) return;
        const ws = e.target.closest && e.target.closest('#splitRoot .ws');
        if (ws) {
            const side = ws.getAttribute('data-side');
            if (side !== focused) setFocus(side);
        }
    });

    // Ctrl/Cmd+S saves the focused workspace; Shift saves both.
    //
    // stopPropagation, not just preventDefault: the hotkey manager also binds
    // Ctrl+S, and preventDefault alone does not stop it — saving here used to
    // save the main editor's file too.
    document.addEventListener('keydown', (e) => {
        if (!active) return;
        const isSave = (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's';
        if (!isSave) return;

        const ws = document.activeElement && document.activeElement.closest
            ? document.activeElement.closest('#splitRoot .ws')
            : null;
        if (!ws && !e.shiftKey) return;

        e.preventDefault();
        e.stopPropagation();
        if (e.shiftKey) return saveAll();
        save(ws.getAttribute('data-side'));
    }, true);

    window.SplitView = {
        toggle, open, close, save, saveAll,
        isActive: () => active,
        focus: setFocus,
        focusFilter: focusFilter,

        // What the command palette acts on. With the split open there are two
        // of everything, and Ctrl+K has to mean the one being worked in rather
        // than the main window's single explorer behind it.
        focusedSide: () => focused,
        serverID: () => space(focused).serverID,
        currentPath: () => space(focused).path || '/',
        goTo: (path) => browse(focused, path),
        useServer: (id, name, panel) => setServer(focused, id, name, panel),

        // Session restore reaches in through these.
        state: () => ({
            outer: outer,
            focused: focused,
            spaces: SIDES.map((side) => {
                const st = space(side);
                return {
                    side: side, serverID: st.serverID, serverName: st.serverName, panel: st.panel,
                    path: st.path, layout: st.layout, explorerSize: st.explorerSize,
                    activeTab: st.activeTab,
                    tabs: Array.from(st.tabs.entries()).map(([path, tab]) => ({
                        path: path, name: tab.name, dirty: tab.dirty,
                        draft: tab.dirty ? tab.content : null
                    }))
                };
            })
        }),
        applyState: async (state) => {
            if (!state || !state.spaces) return false;
            let any = false;

            for (const saved of state.spaces) {
                if (!saved.serverID) continue;
                const st = space(saved.side);
                const sel = el(saved.side, '[data-role="server"]');
                if (sel) sel.value = saved.serverID;

                st.serverID = saved.serverID;
                st.serverName = saved.serverName || '';
                st.panel = saved.panel || '';
                st.path = saved.path || '/';
                st.layout = saved.layout === 'stacked' ? 'stacked' : 'side';
                st.explorerSize = typeof saved.explorerSize === 'number' ? saved.explorerSize : 32;
                st.tabs.clear();

                for (const tab of (saved.tabs || [])) {
                    try {
                        const read = await go().ReadFileForEdit(st.serverID, tab.path);
                        if (read.binary || read.too_big) continue;
                        st.tabs.set(tab.path, {
                            name: tab.name,
                            // A draft is what was in the box when the app went
                            // away; original stays the panel's copy, so the
                            // dirty marker and the conflict check both still
                            // mean what they say.
                            content: tab.draft != null ? tab.draft : read.content,
                            original: read.content,
                            dirty: tab.draft != null && tab.draft !== read.content
                        });
                    } catch (err) { /* file went away while we were gone */ }
                }

                st.activeTab = st.tabs.has(saved.activeTab) ? saved.activeTab
                    : (st.tabs.size ? st.tabs.keys().next().value : null);

                buildEditor(saved.side);
                renderTabs(saved.side);
                if (st.activeTab) showTab(saved.side, st.activeTab);
                await browse(saved.side, st.path);
                any = true;
            }

            if (Array.isArray(state.outer) && state.outer.length === 2) outer = state.outer;
            applyLayout();
            if (state.focused) setFocus(state.focused);
            return any;
        }
    };
})();
