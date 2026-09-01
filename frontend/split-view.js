/**
 * Split editor: one file tree, two panes, every divider draggable.
 *
 * The previous version gave each pane its own browser that slid over the
 * editor, so opening a file hid the thing you were browsing and the only way
 * back was the Browse button. Here the tree is a column of its own and stays
 * put; it follows whichever pane has focus, and a click sends the file into
 * that pane.
 *
 * Panes can sit on different servers, including servers on different panels.
 * That is why the tree reads through ListFilesFromServer and the panes save
 * through SafeSaveFileContentToServer: those resolve the panel behind the
 * scenes without disturbing the server the rest of the app is pointed at.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const icon = (n, c) => window.Icons.svg(n, c);

    let active = false;
    let servers = [];
    let focused = 'left';
    const panes = {};

    // Column widths as flex-grow values: [tree, left pane, right pane].
    let layout = [22, 39, 39];

    function paneState(side) {
        if (!panes[side]) {
            panes[side] = {
                side: side, serverID: '', serverName: '', panel: '',
                path: '/', file: '', original: '', dirty: false
            };
        }
        return panes[side];
    }

    /* ------------------------------------------------------------- layout */

    function loadLayout() {
        try {
            const stored = JSON.parse(localStorage.getItem('splitLayout') || 'null');
            if (Array.isArray(stored) && stored.length === 3 && stored.every(n => typeof n === 'number' && n > 4)) {
                layout = stored;
            }
        } catch (err) { /* private mode */ }
    }

    function saveLayout() {
        try { localStorage.setItem('splitLayout', JSON.stringify(layout)); } catch (err) { /* private mode */ }
    }

    function applyLayout() {
        const root = $('splitRoot');
        if (!root) return;
        const cols = root.querySelectorAll('[data-col]');
        cols.forEach((col, i) => { col.style.flexGrow = layout[i]; });
    }

    /* ------------------------------------------------------------- markup */

    function paneMarkup(side) {
        return '' +
            '<section class="split-pane" data-col="' + (side === 'left' ? 1 : 2) + '" data-side="' + side + '">' +
            '  <header class="split-head">' +
            '    <span class="split-focus" title="Files open into the focused pane"></span>' +
            '    <select data-role="server" class="split-server"><option value="">Choose a server…</option></select>' +
            '    <span class="split-file mono" data-role="filename">no file open</span>' +
            '    <span class="split-dirty" data-role="dirty" hidden></span>' +
            '    <span class="spacer"></span>' +
            '    <button class="sm primary" data-role="save" type="button" disabled>Save</button>' +
            '  </header>' +
            '  <div class="split-body">' +
            '    <textarea class="editor-textarea mono" data-role="editor" spellcheck="false" ' +
            '      placeholder="Pick a server, then choose a file from the tree."></textarea>' +
            '  </div>' +
            '</section>';
    }

    function rootMarkup() {
        return '' +
            '<div class="split-tree" data-col="0">' +
            '  <div class="split-tree-head">' +
            '    <span class="eyebrow" data-role="treeserver">no server</span>' +
            '    <span class="spacer"></span>' +
            '    <button class="sm icon-only" data-role="treerefresh" type="button" title="Refresh"></button>' +
            '  </div>' +
            '  <div class="split-tree-path mono" data-role="treepath">/</div>' +
            '  <div class="split-tree-list" data-role="treelist"></div>' +
            '</div>' +
            '<div class="split-grip" data-grip="0"></div>' +
            paneMarkup('left') +
            '<div class="split-grip" data-grip="1"></div>' +
            paneMarkup('right');
    }

    /* ------------------------------------------------------------ servers */

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
        // Grouped by panel, so it is obvious when a pane is on another one.
        const byPanel = {};
        servers.forEach((srv) => {
            (byPanel[srv.panel] = byPanel[srv.panel] || []).push(srv);
        });

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

    /* --------------------------------------------------------------- tree */

    function setFocus(side) {
        focused = side;
        document.querySelectorAll('#splitRoot .split-pane').forEach((pane) => {
            pane.classList.toggle('focused', pane.getAttribute('data-side') === side);
        });
        renderTree();
    }

    async function renderTree(path) {
        const root = $('splitRoot');
        if (!root) return;

        const list = root.querySelector('[data-role="treelist"]');
        const label = root.querySelector('[data-role="treeserver"]');
        const crumb = root.querySelector('[data-role="treepath"]');
        const state = paneState(focused);

        label.textContent = state.serverName
            ? (state.panel ? state.panel + ' · ' + state.serverName : state.serverName)
            : 'no server';

        if (!state.serverID) {
            crumb.textContent = '/';
            list.innerHTML = '<div class="empty-state"><div class="empty-state-title">Pick a server</div>' +
                '<div class="empty-state-hint">Each pane chooses its own, and the tree follows the focused one.</div></div>';
            return;
        }

        const target = path === undefined ? (state.path || '/') : path;
        crumb.textContent = target;
        list.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '</div>';

        let files;
        try {
            files = await go().ListFilesFromServer(state.serverID, target);
        } catch (err) {
            list.innerHTML = '<div class="error">' + esc(String(err)) + '</div>';
            return;
        }

        state.path = target;

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
            html += '<div class="file-item" data-name="' + esc(f.name) + '" data-dir="' + (isDir ? '1' : '') + '">' +
                '<span class="file-icon kind-' + window.Icons.kindFor(f.name, isDir) + '">' +
                window.Icons.forFile(f.name, isDir) + '</span>' +
                '<span class="file-name">' + esc(f.name) + '</span>' +
                '<span class="file-size">' + (isDir ? '' : window.Shell.fmt.bytes(f.size)) + '</span></div>';
        });

        list.innerHTML = html || '<div class="preview-empty">Empty folder</div>';
    }

    /* --------------------------------------------------------------- files */

    async function openFile(side, name) {
        const state = paneState(side);
        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        const editor = root.querySelector('[data-role="editor"]');

        if (state.dirty && !(await confirmDiscard(side))) return;

        const full = state.path === '/' ? '/' + name : state.path + '/' + name;
        editor.value = 'Loading…';
        editor.disabled = true;

        try {
            // Same guard as the main editor: a binary or oversized file loaded
            // into a textarea comes back mangled on save.
            const read = await go().ReadFileForEdit(state.serverID, full);
            if (read.binary || read.too_big) {
                editor.value = '';
                state.file = '';
                state.original = '';
                window.UX.toast.warn(read.binary
                    ? name + ' is binary; editing it here would corrupt it'
                    : name + ' is over the 8 MB editor limit');
            } else {
                state.file = full;
                state.original = read.content;
                editor.value = read.content;
            }
        } catch (err) {
            editor.value = '';
            state.file = '';
            state.original = '';
            window.UX.toast.bad(String(err));
        } finally {
            editor.disabled = false;
        }

        root.querySelector('[data-role="filename"]').textContent = state.file || 'no file open';
        markDirty(side, false);
    }

    /** Shared prompt for the three ways a pane can lose unsaved edits. */
    function confirmDiscard(side) {
        const state = paneState(side);
        return window.Shell.dialog.confirm('Unsaved changes',
            'The ' + side + ' pane has unsaved changes to <span class="mono">' + esc(state.file) + '</span>. ' +
            'Continuing discards them.',
            { danger: true, confirmLabel: 'Discard them' });
    }

    /**
     * Writes one pane back to its server.
     *
     * Goes through SafeSaveFileContentToServer, so the state being replaced is
     * filed in the local history first and a file that changed on the panel
     * since this pane opened it is refused rather than clobbered. That matters
     * more here than in the main editor: both panes can hold the same file on
     * the same server, and saving one would otherwise wipe the other's work.
     */
    async function save(side, force) {
        const state = paneState(side);
        if (!state.serverID || !state.file) return;

        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        const editor = root.querySelector('[data-role="editor"]');
        const btn = root.querySelector('[data-role="save"]');

        btn.disabled = true;
        btn.textContent = 'Saving…';
        try {
            const res = await go().SafeSaveFileContentToServer(
                state.serverID, state.file, editor.value, state.original, !!force);

            if (res && res.conflict) {
                btn.textContent = 'Save';
                btn.disabled = false;

                let diff = '';
                if (res.remote_content && window.Vault && window.Vault.diffHtml) {
                    diff = '<div style="margin-top:12px">' +
                        window.Vault.diffHtml(res.remote_content, editor.value) + '</div>';
                }

                const ok = await window.Shell.dialog.confirm('The panel copy changed',
                    '<span class="mono">' + esc(state.file) + '</span> ' + esc(res.reason) + '.' +
                    '<br><br>Saving now replaces it with what is in this pane. The panel copy is filed in the ' +
                    'local history first, so it can be restored from Vault.' + diff,
                    { danger: true, confirmLabel: 'Overwrite anyway' });
                if (!ok) return;
                return save(side, true);
            }

            state.original = editor.value;
            markDirty(side, false);
            btn.textContent = 'Saved';
            setTimeout(() => { btn.textContent = 'Save'; }, 1200);
        } catch (err) {
            btn.textContent = 'Save';
            btn.disabled = false;
            window.UX.toast.bad('Save failed: ' + err);
        }
    }

    function markDirty(side, dirty) {
        const state = paneState(side);
        state.dirty = dirty;
        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        if (!root) return;
        root.querySelector('[data-role="dirty"]').hidden = !dirty;
        root.querySelector('[data-role="save"]').disabled = !dirty || !state.file;
    }

    /* ------------------------------------------------------------ resizing */

    function startDrag(gripIndex, startEvent) {
        const root = $('splitRoot');
        if (!root) return;

        const cols = Array.from(root.querySelectorAll('[data-col]'));
        const a = gripIndex;          // column before the grip
        const b = gripIndex + 1;      // column after it

        const rect = root.getBoundingClientRect();
        const total = layout[a] + layout[b];
        const startX = startEvent.clientX;
        const startA = layout[a];
        const pxPerUnit = rect.width / layout.reduce((sum, n) => sum + n, 0);

        // A minimum keeps a column from collapsing to a sliver that cannot be
        // grabbed again.
        const MIN = 8;

        function onMove(e) {
            const delta = (e.clientX - startX) / pxPerUnit;
            let nextA = Math.max(MIN, Math.min(total - MIN, startA + delta));
            layout[a] = nextA;
            layout[b] = total - nextA;
            cols[a].style.flexGrow = layout[a];
            cols[b].style.flexGrow = layout[b];
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

    /* -------------------------------------------------------------- toggle */

    function open() {
        const manager = document.querySelector('.file-manager');
        const tree = document.querySelector('.file-tree');
        const editor = document.querySelector('.editor-container');
        if (!manager || !editor) return;

        loadLayout();

        tree.style.display = 'none';
        editor.style.display = 'none';
        manager.classList.add('split-active');

        const root = document.createElement('div');
        root.id = 'splitRoot';
        root.innerHTML = rootMarkup();
        manager.appendChild(root);

        applyLayout();

        const refreshBtn = root.querySelector('[data-role="treerefresh"]');
        if (refreshBtn) refreshBtn.innerHTML = window.Icons.svg('refresh');

        loadServers().then(() => {
            fillServerSelects();

            // Seed the left pane with whatever the main editor is pointed at,
            // so the split opens on something rather than two empty panes.
            const api = go();
            if (!api || !api.GetConfig) return;
            api.GetConfig().then((cfg) => {
                if (!cfg || !cfg.serverID) return;
                const sel = root.querySelector('[data-side="left"] [data-role="server"]');
                sel.value = cfg.serverID;
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }).catch(() => {});
        });

        setFocus('left');

        const btn = $('splitViewBtn');
        if (btn) btn.classList.add('toggled');
        active = true;
    }

    async function close() {
        // Closing removes the textareas, so unsaved work has to be acknowledged.
        for (const side of ['left', 'right']) {
            if (paneState(side).dirty && !(await confirmDiscard(side))) return;
        }

        const root = $('splitRoot');
        if (root) root.remove();

        const manager = document.querySelector('.file-manager');
        const tree = document.querySelector('.file-tree');
        const editor = document.querySelector('.editor-container');
        if (manager) manager.classList.remove('split-active');
        if (tree) tree.style.display = '';
        if (editor) editor.style.display = '';

        const btn = $('splitViewBtn');
        if (btn) btn.classList.remove('toggled');
        active = false;
    }

    function toggle() {
        if (active) return close();
        open();
    }

    /* -------------------------------------------------------------- events */

    document.addEventListener('mousedown', (e) => {
        if (!active) return;
        const grip = e.target.closest('#splitRoot .split-grip');
        if (grip) startDrag(Number(grip.dataset.grip), e);
    });

    document.addEventListener('click', async (e) => {
        if (e.target.closest('#splitViewBtn')) {
            e.preventDefault();
            return toggle();
        }
        if (!active) return;

        // Focus follows the pane you click into.
        const pane = e.target.closest('#splitRoot .split-pane');
        if (pane) {
            const side = pane.getAttribute('data-side');
            if (side !== focused) setFocus(side);

            const btn = e.target.closest('button');
            if (btn && btn.dataset.role === 'save') return save(side);
            return;
        }

        const treeCol = e.target.closest('#splitRoot .split-tree');
        if (!treeCol) return;

        if (e.target.closest('[data-role="treerefresh"]')) return renderTree();

        const row = e.target.closest('.file-item');
        if (!row) return;

        const state = paneState(focused);
        if (row.dataset.updir) {
            const parts = String(state.path).split('/').filter(Boolean);
            parts.pop();
            return renderTree(parts.length ? '/' + parts.join('/') : '/');
        }
        if (row.dataset.dir) {
            const next = state.path === '/' ? '/' + row.dataset.name : state.path + '/' + row.dataset.name;
            return renderTree(next);
        }
        return openFile(focused, row.dataset.name);
    });

    document.addEventListener('change', async (e) => {
        if (!active) return;
        const sel = e.target.closest('#splitRoot [data-role="server"]');
        if (!sel) return;

        const pane = sel.closest('.split-pane');
        const side = pane.getAttribute('data-side');
        const state = paneState(side);
        const opt = sel.selectedOptions[0];

        // Switching servers clears the pane. Ask before throwing edits away,
        // and put the picker back if the answer is no.
        if (state.dirty && !(await confirmDiscard(side))) {
            sel.value = state.serverID;
            return;
        }

        state.serverID = sel.value;
        state.serverName = opt ? opt.textContent : '';
        state.panel = opt ? (opt.dataset.panel || '') : '';
        state.path = '/';
        state.file = '';
        state.original = '';

        pane.querySelector('[data-role="editor"]').value = '';
        pane.querySelector('[data-role="filename"]').textContent = 'no file open';
        markDirty(side, false);

        setFocus(side);
    });

    document.addEventListener('input', (e) => {
        if (!active) return;
        const editor = e.target.closest('#splitRoot [data-role="editor"]');
        if (!editor) return;
        const side = editor.closest('.split-pane').getAttribute('data-side');
        markDirty(side, editor.value !== paneState(side).original);
    });

    document.addEventListener('focusin', (e) => {
        if (!active) return;
        const pane = e.target.closest && e.target.closest('#splitRoot .split-pane');
        if (pane) {
            const side = pane.getAttribute('data-side');
            if (side !== focused) setFocus(side);
        }
    });

    // Ctrl/Cmd+S saves the pane the caret is in; Shift saves both.
    //
    // stopPropagation, not just preventDefault: the hotkey manager also binds
    // Ctrl+S, and preventDefault alone does not stop it — saving a pane used to
    // save the main editor's file too.
    document.addEventListener('keydown', (e) => {
        if (!active) return;
        const isSave = (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's';
        if (!isSave) return;

        const pane = document.activeElement && document.activeElement.closest
            ? document.activeElement.closest('#splitRoot .split-pane')
            : null;

        if (e.shiftKey) {
            e.preventDefault();
            e.stopPropagation();
            save('left');
            save('right');
            return;
        }
        if (pane) {
            e.preventDefault();
            e.stopPropagation();
            save(pane.getAttribute('data-side'));
        }
    }, true);

    window.SplitView = { toggle, open, close, save, isActive: () => active, focus: setFocus };
})();
