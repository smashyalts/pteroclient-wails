/**
 * Split editor. Each side picks its own server — including one on a different
 * panel — browses that server's files, and saves independently.
 *
 * The cross-panel part is why this talks to *FromServer bindings rather than
 * the plain ones: ListFilesFromServer, GetFileContentFromServer and
 * SaveFileContentToServer each resolve which panel a server belongs to and
 * switch the client behind the scenes, so neither side disturbs the active
 * server the rest of the app is pointed at.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const icon = (n, c) => window.Icons.svg(n, c);

    let active = false;
    let servers = [];
    const panes = {};

    function paneState(side) {
        if (!panes[side]) {
            panes[side] = { side, serverID: '', serverName: '', panel: '', path: '/', file: '', original: '', dirty: false };
        }
        return panes[side];
    }

    /* ------------------------------------------------------------ markup */

    function paneMarkup(side) {
        return '' +
            '<section class="split-pane" data-side="' + side + '">' +
            '  <header class="split-head">' +
            '    <span class="eyebrow">' + (side === 'left' ? 'Left' : 'Right') + '</span>' +
            '    <select data-role="server" class="split-server"><option value="">Choose a server…</option></select>' +
            '    <span class="split-file mono" data-role="filename">no file open</span>' +
            '    <span class="split-dirty" data-role="dirty" hidden></span>' +
            '    <span class="spacer"></span>' +
            '    <button class="sm" data-role="browse" type="button">Browse</button>' +
            '    <button class="sm primary" data-role="save" type="button" disabled>Save</button>' +
            '  </header>' +
            '  <div class="split-body">' +
            '    <div class="split-browser" data-role="browser" hidden></div>' +
            '    <textarea class="editor-textarea mono" data-role="editor" spellcheck="false" placeholder="Pick a server, then Browse to open a file."></textarea>' +
            '  </div>' +
            '</section>';
    }

    function ensureStyles() {
        if ($('splitViewStyles')) return;
        const style = document.createElement('style');
        style.id = 'splitViewStyles';
        style.textContent = [
            '#splitRoot { display: flex; flex: 1; min-width: 0; min-height: 0; }',
            '.split-pane { display: flex; flex-direction: column; flex: 1; min-width: 0; border-left: 1px solid var(--line-strong); }',
            '.split-pane:first-child { border-left: 0; }',
            '.split-head { display: flex; align-items: center; gap: 9px; height: 40px; flex: none; padding: 0 12px; background: var(--bg-secondary); border-bottom: 1px solid var(--line); }',
            '.split-server { width: auto; min-width: 170px; max-width: 240px; height: 26px; font-size: 11.5px; }',
            '.split-file { font-size: 11.5px; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px; }',
            '.split-dirty { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); flex: none; }',
            '.split-body { position: relative; flex: 1; min-height: 0; display: flex; }',
            '.split-browser { position: absolute; inset: 0; z-index: 3; overflow-y: auto; padding: 8px; background: var(--bg-secondary); border-right: 1px solid var(--line); }',
            '.split-browser[hidden] { display: none; }',
            '.split-body .editor-textarea { flex: 1; }'
        ].join('\n');
        document.head.appendChild(style);
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
        // Group by panel so it is obvious when a side is on another panel.
        const byPanel = {};
        servers.forEach((s) => {
            (byPanel[s.panel] = byPanel[s.panel] || []).push(s);
        });

        document.querySelectorAll('#splitRoot [data-role="server"]').forEach((sel) => {
            const current = sel.value;
            let html = '<option value="">Choose a server…</option>';
            Object.keys(byPanel).forEach((panel) => {
                html += '<optgroup label="' + esc(panel) + '">';
                byPanel[panel].forEach((s) => {
                    html += '<option value="' + esc(s.id) + '" data-panel="' + esc(panel) + '">' + esc(s.name) + '</option>';
                });
                html += '</optgroup>';
            });
            sel.innerHTML = html;
            if (current) sel.value = current;
        });
    }

    /* ------------------------------------------------------------ browsing */

    async function browse(side, path) {
        const state = paneState(side);
        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        const browser = root.querySelector('[data-role="browser"]');
        const api = go();

        if (!state.serverID) {
            browser.innerHTML = '<div class="empty-state"><div class="empty-state-title">Pick a server first</div></div>';
            browser.hidden = false;
            return;
        }

        browser.hidden = false;
        browser.innerHTML = '<div class="loading">' + icon('refresh', 'spin') + '</div>';

        let files;
        try {
            files = await api.ListFilesFromServer(state.serverID, path);
        } catch (err) {
            browser.innerHTML = '<div class="error">' + esc(String(err)) + '</div>';
            return;
        }

        state.path = path;

        let html = '<div class="file-item" data-updir="1">' + '<span class="file-icon kind-dir">' + icon('folder') + '</span>' +
            '<span class="file-name">' + esc(path === '/' ? '/' : path) + '</span>' +
            '<span class="file-size">' + (path === '/' ? '' : 'up') + '</span></div>';

        (files || []).forEach((f) => {
            const isDir = f.isDir || f.is_dir || f.isDirectory;
            html += '<div class="file-item" data-name="' + esc(f.name) + '" data-dir="' + (isDir ? '1' : '') + '">' +
                '<span class="file-icon ' + 'kind-' + window.Icons.kindFor(f.name, isDir) + '">' + window.Icons.forFile(f.name, isDir) + '</span>' +
                '<span class="file-name">' + esc(f.name) + '</span>' +
                '<span class="file-size">' + (isDir ? '' : window.Shell.fmt.bytes(f.size)) + '</span></div>';
        });

        browser.innerHTML = html;
    }

    async function openFile(side, name) {
        const state = paneState(side);
        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        const editor = root.querySelector('[data-role="editor"]');
        const api = go();

        // Opening over unsaved work in this pane would drop it silently.
        if (state.dirty && !(await confirmDiscard(side))) return;

        const full = state.path === '/' ? '/' + name : state.path + '/' + name;
        editor.value = 'Loading…';
        editor.disabled = true;

        try {
            // Same guard as the main editor: a binary or oversized file loaded
            // into a textarea comes back mangled on save.
            const read = await api.ReadFileForEdit(state.serverID, full);
            if (read.binary || read.too_big) {
                editor.value = '';
                state.file = '';
                state.original = '';
                await window.Shell.dialog.confirm('Cannot edit this file',
                    read.binary
                        ? esc(name) + ' is not valid UTF-8, so editing it here would corrupt it.'
                        : esc(name) + ' is larger than the 8 MB editor limit.',
                    { confirmLabel: 'OK' });
            } else {
                state.file = full;
                state.original = read.content;
                editor.value = read.content;
            }
        } catch (err) {
            editor.value = '';
            state.file = '';
            state.original = '';
            window.Shell.dialog.confirm('Could not open file', esc(String(err)), { confirmLabel: 'OK' });
        } finally {
            editor.disabled = false;
        }

        root.querySelector('[data-role="browser"]').hidden = true;
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
     * more here than in the main editor: the two panes can hold the same file
     * on the same server, and saving one would otherwise wipe the other's work.
     */
    async function save(side, force) {
        const state = paneState(side);
        if (!state.serverID || !state.file) return;

        const root = document.querySelector('#splitRoot [data-side="' + side + '"]');
        const editor = root.querySelector('[data-role="editor"]');
        const btn = root.querySelector('[data-role="save"]');
        const api = go();

        btn.disabled = true;
        btn.textContent = 'Saving…';
        try {
            const res = await api.SafeSaveFileContentToServer(
                state.serverID, state.file, editor.value, state.original, !!force);

            if (res && res.conflict) {
                btn.textContent = 'Save';
                btn.disabled = false;
                const ok = await window.Shell.dialog.confirm('The panel copy changed',
                    '<span class="mono">' + esc(state.file) + '</span> ' + esc(res.reason) + '.' +
                    '<br><br>Saving now replaces it with what is in this pane. The panel copy is filed in the ' +
                    'local history first, so it can be restored from Vault.',
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
            window.Shell.dialog.confirm('Save failed', esc(String(err)), { confirmLabel: 'OK' });
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

    /* -------------------------------------------------------------- toggle */

    function open() {
        ensureStyles();

        const manager = document.querySelector('.file-manager');
        const tree = document.querySelector('.file-tree');
        const editor = document.querySelector('.editor-container');
        if (!manager || !editor) return;

        tree.style.display = 'none';
        editor.style.display = 'none';

        const root = document.createElement('div');
        root.id = 'splitRoot';
        root.innerHTML = paneMarkup('left') + paneMarkup('right');
        manager.appendChild(root);

        loadServers().then(fillServerSelects);

        // Seed the left pane with whatever the main editor is pointed at.
        const api = go();
        if (api && api.GetConfig) {
            api.GetConfig().then((cfg) => {
                if (!cfg || !cfg.serverID) return;
                const sel = root.querySelector('[data-side="left"] [data-role="server"]');
                setTimeout(() => {
                    sel.value = cfg.serverID;
                    sel.dispatchEvent(new Event('change'));
                }, 150);
            }).catch(() => {});
        }

        const btn = $('splitViewBtn');
        if (btn) btn.classList.add('toggled');
        active = true;
    }

    async function close() {
        // Closing the split view removes the textareas, so unsaved work in
        // either pane has to be acknowledged before it goes.
        for (const side of ['left', 'right']) {
            if (paneState(side).dirty && !(await confirmDiscard(side))) return;
        }

        const root = $('splitRoot');
        if (root) root.remove();

        const tree = document.querySelector('.file-tree');
        const editor = document.querySelector('.editor-container');
        if (tree) tree.style.display = '';
        if (editor) editor.style.display = '';

        const btn = $('splitViewBtn');
        if (btn) btn.classList.remove('toggled');
        active = false;
    }

    function toggle() {
        if (active) close();
        else open();
    }

    /* ------------------------------------------------------------- events */

    document.addEventListener('click', (e) => {
        if (e.target.closest('#splitViewBtn')) {
            e.preventDefault();
            return toggle();
        }
        if (!active) return;

        const pane = e.target.closest('#splitRoot .split-pane');
        if (!pane) return;
        const side = pane.getAttribute('data-side');

        const btn = e.target.closest('button');
        if (btn && btn.dataset.role === 'browse') {
            const browser = pane.querySelector('[data-role="browser"]');
            if (browser.hidden) browse(side, paneState(side).path || '/');
            else browser.hidden = true;
            return;
        }
        if (btn && btn.dataset.role === 'save') return save(side);

        const row = e.target.closest('.file-item');
        if (row) {
            const state = paneState(side);
            if (row.dataset.updir) {
                if (state.path === '/') return;
                const parts = state.path.split('/').filter(Boolean);
                parts.pop();
                return browse(side, parts.length ? '/' + parts.join('/') : '/');
            }
            if (row.dataset.dir) {
                const next = state.path === '/' ? '/' + row.dataset.name : state.path + '/' + row.dataset.name;
                return browse(side, next);
            }
            return openFile(side, row.dataset.name);
        }
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
        pane.querySelector('[data-role="filename"]').textContent = state.panel ? state.panel + ' · no file open' : 'no file open';
        markDirty(side, false);

        if (state.serverID) browse(side, '/');
    });

    document.addEventListener('input', (e) => {
        if (!active) return;
        const editor = e.target.closest('#splitRoot [data-role="editor"]');
        if (!editor) return;
        const side = editor.closest('.split-pane').getAttribute('data-side');
        markDirty(side, editor.value !== paneState(side).original);
    });

    // Ctrl/Cmd+S saves the pane the caret is in; Shift saves both.
    document.addEventListener('keydown', (e) => {
        if (!active) return;
        const isSave = (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's';
        if (!isSave) return;

        const pane = document.activeElement && document.activeElement.closest
            ? document.activeElement.closest('#splitRoot .split-pane')
            : null;

        // stopPropagation, not just preventDefault: main-editor.js has its own
        // document-level Ctrl+S handler, and preventDefault does not stop it.
        // Without this, saving a pane also wrote the main editor's active file.
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

    window.SplitView = { toggle, open, close, save, isActive: () => active };
})();
