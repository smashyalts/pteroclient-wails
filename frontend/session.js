/**
 * Session persistence: the app comes back the way you left it.
 *
 * Open tabs, the folder you were in, the split view's two workspaces and the
 * text you had not saved yet. Written on every meaningful change and on a
 * short timer, so a crash or a pulled plug costs at most a few seconds of
 * typing rather than the whole edit.
 *
 * Unsaved text is stored as a *draft*. It is restored into the editor, but the
 * baseline the conflict check compares against is re-read from the panel — so a
 * file someone else changed while the app was closed is still caught rather
 * than quietly overwritten with a stale draft.
 */
(function () {
    'use strict';

    const KEY = 'session:v1';
    const SAVE_EVERY = 4000;

    // Drafts are the only part that can grow without bound, and localStorage
    // gives up somewhere north of 5 MB. Past these, the tab is remembered but
    // its unsaved text is not.
    const MAX_DRAFT = 256 * 1024;
    const MAX_TOTAL = 2 * 1024 * 1024;

    let timer = null;
    let dirtySinceSave = false;

    // Read once, at load, before anything else can run. Booting shows a tab,
    // which fires a save, which wrote the empty startup state over the very
    // thing that was about to be restored. Snapshotting first makes the order
    // of those two irrelevant.
    let pending = null;
    try {
        pending = JSON.parse(localStorage.getItem(KEY) || 'null');
    } catch (err) {
        pending = null;
    }

    // Held until the restore has finished, so nothing it does is written back
    // as if it were the user's own state.
    let restoring = true;

    const go = () => (window.go && window.go.main && window.go.main.App) || null;

    /* ------------------------------------------------------------ capture */

    function mainEditorState(app) {
        const tabs = [];
        let budget = MAX_TOTAL;

        app.openFiles.forEach((file, path) => {
            const isDirty = file.modified && file.content !== file.originalContent;
            let draft = null;
            if (isDirty && file.content.length <= MAX_DRAFT && budget - file.content.length > 0) {
                draft = file.content;
                budget -= file.content.length;
            }
            tabs.push({ path: path, name: file.name, draft: draft });
        });

        return {
            path: app.currentPath || '/',
            activeTab: app.activeFile,
            treeDock: app.treeDock,
            tabs: tabs
        };
    }

    function capture() {
        const app = window.app;
        if (!app || !app.openFiles) return null;

        return {
            at: new Date().toISOString(),
            tab: window.Shell ? window.Shell.currentTab() : 'console',
            main: mainEditorState(app),
            split: (window.SplitView && window.SplitView.isActive())
                ? window.SplitView.state()
                : null
        };
    }

    function save() {
        if (restoring) return;
        try {
            const state = capture();
            if (!state) return;
            localStorage.setItem(KEY, JSON.stringify(state));
            dirtySinceSave = false;
        } catch (err) {
            // Quota, private mode, anything else: losing the session is not
            // worth interrupting the work for.
            console.warn('Session not saved:', err);
        }
    }

    function markDirty() {
        dirtySinceSave = true;
    }

    /* ------------------------------------------------------------ restore */

    function stored() {
        return pending;
    }

    async function restoreMain(state) {
        const app = window.app;
        if (!app || !state || !state.main) return;

        const main = state.main;

        if (main.treeDock) app.applyTreeDock(main.treeDock);
        if (main.path && main.path !== '/') {
            try { await app.loadFiles(main.path); } catch (err) { /* folder gone */ }
        }

        let restored = 0;
        for (const tab of (main.tabs || [])) {
            try {
                const read = await go().ReadFileForEdit('', tab.path);
                if (read.binary || read.too_big) continue;

                app.openFiles.set(tab.path, {
                    name: tab.name,
                    path: tab.path,
                    // The draft is what was on screen; originalContent stays the
                    // panel's copy, so the dirty marker and the conflict check
                    // both keep meaning what they say.
                    content: tab.draft != null ? tab.draft : read.content,
                    originalContent: read.content,
                    modified: tab.draft != null && tab.draft !== read.content
                });
                app.addEditorTab(tab.path, tab.name);
                app.updateTabModified(tab.path, app.openFiles.get(tab.path).modified);
                restored++;
            } catch (err) {
                // The file was deleted or renamed while the app was closed.
            }
        }

        if (!restored) return;

        const active = app.openFiles.has(main.activeTab)
            ? main.activeTab
            : app.openFiles.keys().next().value;
        app.switchToFile(active);

        const unsaved = Array.from(app.openFiles.values()).filter(f => f.modified).length;
        if (unsaved) {
            window.UX.toast.warn(restored + ' file(s) reopened, ' + unsaved +
                ' with unsaved changes from last time');
        } else {
            window.UX.toast.show(restored + ' file(s) reopened', { duration: 2600 });
        }
    }

    let restored = false;

    async function restore() {
        if (restored) return;
        restored = true;

        const state = pending;
        if (!state) {
            restoring = false;
            return;
        }

        try {
            await restoreMain(state);
            if (state.tab && window.Shell) window.Shell.showTab(state.tab);
        } catch (err) {
            console.warn('Session restore failed:', err);
        } finally {
            restoring = false;
        }
    }

    /**
     * Called by the split view once its markup exists and servers are loaded.
     *
     * The snapshot is consumed, so closing the split and opening it again in
     * the same session gives you two empty workspaces rather than resurrecting
     * the ones you just closed.
     */
    async function restoreSplit() {
        if (!pending || !pending.split) return false;

        const split = pending.split;
        pending = Object.assign({}, pending, { split: null });

        const wasRestoring = restoring;
        restoring = true;
        try {
            return await window.SplitView.applyState(split);
        } catch (err) {
            console.warn('Split restore failed:', err);
            return false;
        } finally {
            restoring = wasRestoring;
        }
    }

    function clear() {
        pending = null;
        try { localStorage.removeItem(KEY); } catch (err) { /* private mode */ }
    }

    /* --------------------------------------------------------------- boot */

    function start() {
        // A timer rather than a save on every keystroke: the point is to
        // survive an abrupt shutdown, not to write on every character.
        timer = setInterval(() => { if (dirtySinceSave) save(); }, SAVE_EVERY);

        document.addEventListener('input', markDirty, true);
        document.addEventListener('tab:show', save);
        document.addEventListener('path:changed', markDirty);

        // Best-effort on the way out. WebView2 fires these; if it is killed
        // outright, the timer's last write is what survives.
        window.addEventListener('beforeunload', save);
        window.addEventListener('pagehide', save);
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'hidden') save();
        });
    }

    function boot() {
        // The restore needs a connected client, which arrives with this event.
        // Restoring before it would read every file as "not connected".
        let done = false;
        const once = () => {
            if (done) return;
            done = true;
            setTimeout(restore, 400);
        };

        if (window.runtime && window.runtime.EventsOn) {
            window.runtime.EventsOn('connected', (connected) => { if (connected) once(); });
        }
        // If the connection never arrives, saving still has to resume — a
        // session that can never be written is worse than one that restores
        // nothing.
        setTimeout(once, 8000);
        start();
    }

    window.Session = { save, restore, restoreSplit, clear, capture };

    function waitForApp() {
        if (window.app && window.UX && window.Shell) {
            boot();
            return;
        }
        setTimeout(waitForApp, 80);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', waitForApp);
    } else {
        waitForApp();
    }
})();
