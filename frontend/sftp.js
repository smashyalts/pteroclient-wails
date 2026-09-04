/**
 * Fast transfers.
 *
 * The client API moves one file per HTTP request and caps both ends, so a
 * plugins folder is hundreds of round trips and a world folder is not on. SFTP
 * is one connection carrying several transfers at once, and this is the front
 * of it: connect, watch, cancel.
 *
 * Everything here is opt-in. Nothing connects on its own, because connecting
 * needs the panel account's password — the API key cannot be used for SFTP —
 * and a password is not something to ask for unprompted. It is held for the
 * life of the connection and never written down.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);
    const bytes = (n) => window.Shell.fmt.bytes(n);

    let status = { connected: false };
    let busy = false;

    /* ------------------------------------------------------------- connect */

    async function connect() {
        const api = go();
        if (!api) return;

        let prompt;
        try {
            prompt = await api.GetSFTPPrompt('');
        } catch (err) {
            window.UX.toast.bad('Could not read this server’s SFTP details: ' + err);
            return;
        }

        const answers = await window.Shell.dialog.form('Connect over SFTP', [
            { name: 'user', label: 'Username', value: prompt.username || '', mono: true },
            { name: 'pass', label: 'Password', type: 'password',
              hint: 'Your panel account’s password. The API key cannot be used for SFTP.' },
            { name: 'streams', label: 'Parallel transfers', type: 'number', value: '6',
              hint: 'How many files move at once. Six is a good default; more is not always faster.' }
        ], {
            confirmLabel: 'Connect',
            intro: '<div class="list-sub">' +
                esc(prompt.host) + ':' + prompt.port +
                (prompt.known ? ' · this host has been connected to before' : '') +
                '</div><div class="list-sub" style="margin-top:6px">' +
                'The password is kept only while the connection is open, and is never stored.' +
                '</div>',
            html: true
        });
        if (!answers) return;

        await attempt(answers.user, answers.pass, Number(answers.streams) || 6, '');
    }

    /**
     * One connection attempt.
     *
     * An unknown host fails on purpose the first time, carrying its
     * fingerprint. Showing it and asking is the only way to know the machine
     * answering is the one that answered last time — without it, anything
     * between here and the node could read every file that moves.
     */
    async function attempt(user, pass, streams, accept) {
        const api = go();
        try {
            status = await api.SFTPConnect('', user, pass, accept, streams);
            paint();
            window.UX.toast.ok('Connected over SFTP · ' + status.streams + ' parallel transfers');
            return true;
        } catch (err) {
            const text = String(err && err.message ? err.message : err);

            // First time on this host.
            const first = text.match(/has not been connected to before \((SHA256:[^)]+)\)/);
            if (first) {
                const ok = await window.Shell.dialog.confirm('New host',
                    'This is the first connection to this SFTP host. Its key fingerprint is:' +
                    '<pre class="mono" style="margin:10px 0;font-size:12px">' + esc(first[1]) + '</pre>' +
                    'Accepting it means every later connection is checked against it, and you are ' +
                    'told if it ever changes.',
                    { html: true, confirmLabel: 'Accept and connect' });
                if (!ok) return false;
                return attempt(user, pass, streams, first[1]);
            }

            // It changed. Not a prompt: that is either a rebuilt node or
            // somebody in the middle, and this cannot tell which.
            if (text.indexOf('host key for') !== -1 && text.indexOf('has changed') !== -1) {
                await window.Shell.dialog.confirm('The host key has changed',
                    '<pre class="mono" style="font-size:12px;white-space:pre-wrap">' + esc(text) + '</pre>' +
                    'If this node really was rebuilt, forget the old key in Settings and connect again. ' +
                    'If it was not, do not.',
                    { html: true, danger: true, confirmLabel: 'OK' });
                return false;
            }

            window.UX.toast.bad('SFTP: ' + text);
            return false;
        }
    }

    async function disconnect() {
        const api = go();
        if (!api) return;
        await api.SFTPDisconnect();
        status = { connected: false };
        paint();
        window.UX.toast.show('SFTP disconnected');
    }

    /* ------------------------------------------------------------ transfers */

    async function upload(localPaths, remoteDir) {
        if (!status.connected) {
            window.UX.toast.warn('Connect over SFTP first');
            return null;
        }
        if (busy) {
            window.UX.toast.warn('A transfer is already running');
            return null;
        }

        // The same question the API upload asks, for the same reason: a
        // replacement without a copy first is the one thing with no way back.
        const answers = await window.Shell.dialog.form('Upload over SFTP', [
            { name: 'keep', label: 'Keep a copy of anything replaced', type: 'checkbox', value: true },
            { name: 'over', label: 'Replace files that already exist', type: 'checkbox', value: false }
        ], {
            confirmLabel: 'Upload',
            intro: '<div class="list-sub">' + localPaths.length +
                ' item(s) into <span class="mono">' + esc(remoteDir) + '</span>. ' +
                'Folders are sent whole.</div>',
            html: true
        });
        if (!answers) return null;

        return run('upload', () => go().SFTPUpload(localPaths, remoteDir, !!answers.over, !!answers.keep));
    }

    async function download(remotePaths) {
        if (!status.connected) {
            window.UX.toast.warn('Connect over SFTP first');
            return null;
        }
        if (busy) {
            window.UX.toast.warn('A transfer is already running');
            return null;
        }

        let dir;
        try {
            dir = await go().PickLocalFolder('Where should these go?');
        } catch (err) {
            window.UX.toast.bad(String(err));
            return null;
        }
        if (!dir) return null;

        return downloadTo(remotePaths, dir);
    }

    /**
     * Download into a folder that is already known.
     *
     * The transfer view has the destination on screen, so asking again would be
     * a dialog whose answer is already visible.
     */
    async function downloadTo(remotePaths, dir) {
        if (!status.connected) {
            window.UX.toast.warn('Connect over SFTP first');
            return null;
        }
        if (busy) {
            window.UX.toast.warn('A transfer is already running');
            return null;
        }
        if (!dir) return null;
        return run('download', () => go().SFTPDownload(remotePaths, dir));
    }

    /** Runs one transfer with the progress bar up. */
    async function run(kind, call) {
        busy = true;
        showBar(kind);
        try {
            const out = await call();
            report(kind, out);
            return out;
        } catch (err) {
            window.UX.toast.bad(String(err));
            return null;
        } finally {
            busy = false;
            hideBar();
        }
    }

    function report(kind, out) {
        if (!out) return;

        const rate = out.seconds > 0.2
            ? ' at ' + bytes(Math.round(out.bytes / out.seconds)) + '/s'
            : '';
        // "over SFTP" said plainly: the same buttons do the same job over the
        // panel API when there is no connection, and the two are very
        // different speeds — so which one just ran is worth knowing.
        const headline = out.moved + ' file(s) ' +
            (kind === 'upload' ? 'uploaded' : 'downloaded') + ' over SFTP' +
            ' · ' + bytes(out.bytes) + rate;

        if (out.cancelled) {
            window.UX.toast.warn('Stopped after ' + headline);
        } else if (out.failed.length) {
            window.UX.toast.warn(headline + ' · ' + out.failed.length + ' failed', {
                action: {
                    label: 'Show',
                    run: () => window.Shell.dialog.confirm('These did not transfer',
                        '<pre class="mono" style="max-height:300px;overflow:auto;font-size:11.5px;' +
                        'white-space:pre-wrap">' + esc(out.failed.join('\n')) + '</pre>',
                        { html: true, confirmLabel: 'OK' })
                }
            });
        } else {
            window.UX.toast.ok(headline);
        }

        if (out.conflicts && out.conflicts.length) {
            window.UX.toast.warn(out.conflicts.length +
                ' left alone because a file of that name is already there');
        }
        if (out.truncated) {
            window.UX.toast.warn('That is more than 20,000 files; the rest were left');
        }
    }

    /* ------------------------------------------------------------ the bar */

    function showBar(kind) {
        const bar = $('sftpBar');
        if (!bar) return;
        bar.hidden = false;
        bar.dataset.kind = kind;
        $('sftpBarLabel').textContent =
            (kind === 'upload' ? 'Uploading' : 'Downloading') + ' over SFTP…';
        $('sftpBarFill').style.width = '0%';
        $('sftpBarDetail').textContent = '';
    }

    function hideBar() {
        const bar = $('sftpBar');
        if (bar) bar.hidden = true;
    }

    function onProgress(p) {
        const bar = $('sftpBar');
        if (!bar || bar.hidden || !p) return;

        const pct = p.total_bytes > 0
            ? Math.min(100, (p.bytes / p.total_bytes) * 100)
            : (p.total > 0 ? (p.done / p.total) * 100 : 0);
        $('sftpBarFill').style.width = pct.toFixed(1) + '%';

        const streams = window.SFTP && window.SFTP.status().streams;
        $('sftpBarLabel').textContent =
            (bar.dataset.kind === 'upload' ? 'Uploading' : 'Downloading') +
            ' ' + p.done + ' of ' + p.total + ' over SFTP' +
            (streams ? ' · ' + streams + ' at a time' : '');
        $('sftpBarDetail').textContent =
            bytes(p.bytes) + ' of ' + bytes(p.total_bytes) +
            (p.current ? ' · ' + p.current : '');
    }

    /* ----------------------------------------------------------- the button */

    function paint() {
        const btn = $('sftpBtn');
        if (btn) {
            btn.classList.toggle('toggled', !!status.connected);
            btn.title = status.connected
                ? 'SFTP connected to ' + status.host + ' as ' + status.user +
                  ' · ' + status.streams + ' parallel transfers · click to disconnect'
                : 'Connect over SFTP for fast folder transfers';
        }
    }

    /* ------------------------------------------------------------- wiring */

    document.addEventListener('click', async (e) => {
        if (e.target.closest('#sftpBtn')) {
            e.preventDefault();
            if (status.connected) return disconnect();
            return connect();
        }
        if (e.target.closest('#sftpBarCancel')) {
            e.preventDefault();
            const api = go();
            if (api) api.SFTPCancel();
        }
    });

    function listen() {
        if (!window.runtime || !window.runtime.EventsOn) return setTimeout(listen, 200);
        window.runtime.EventsOn('sftp-progress', onProgress);
        window.runtime.EventsOn('sftp-status', (next) => {
            status = next || { connected: false };
            paint();
        });
        window.runtime.EventsOn('sftp-finished', () => {
            // The folders it wrote into are not what the explorer has cached.
            if (window.app) {
                window.app.dirCacheClear();
                window.app.refreshFiles();
            }
        });
    }
    listen();

    window.SFTP = {
        connect: connect,
        disconnect: disconnect,
        upload: upload,
        download: download,
        downloadTo: downloadTo,
        isConnected: () => !!status.connected,
        status: () => Object.assign({}, status)
    };
}());
