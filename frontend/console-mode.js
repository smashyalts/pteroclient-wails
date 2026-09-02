/**
 * Console-window mode.
 *
 * When the binary is launched with --console it is the same app, the same
 * frontend and the same bindings — it just has one job. Rather than shipping a
 * second HTML page that would drift out of step with this one, the shell is
 * folded down to the console and everything else is hidden.
 *
 * The mode comes from the Go side rather than a URL, because Wails serves the
 * frontend from the root and there is no query string to read.
 */
(function () {
    'use strict';

    const go = () => (window.go && window.go.main && window.go.main.App) || null;

    async function apply() {
        const api = go();
        if (!api || !api.WindowMode) return false;

        let info;
        try {
            info = await api.WindowMode();
        } catch (err) {
            return false;
        }
        if (!info || info.mode !== 'console') return false;

        document.body.classList.add('console-window');

        // Whatever the session last had open is irrelevant here, and restoring
        // it would fight the console for the tab.
        if (window.Session) window.Session.clear();
        if (window.Shell) window.Shell.showTab('console');

        // The console is the whole window, so it connects without being asked
        // and fills in the history the websocket does not replay.
        const waitForApp = setInterval(() => {
            if (!window.app || !window.app.ensureConsole) return;
            clearInterval(waitForApp);
            window.app.ensureConsole();
            setTimeout(() => {
                if (window.app.loadServerLog) window.app.loadServerLog();
            }, 900);
        }, 100);

        return true;
    }

    function waitForRuntime() {
        if (window.go && window.go.main && window.go.main.App) {
            apply();
            return;
        }
        setTimeout(waitForRuntime, 60);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', waitForRuntime);
    } else {
        waitForRuntime();
    }
})();
