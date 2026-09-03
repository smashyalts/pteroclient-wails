/**
 * App settings.
 *
 * The handful of choices that are about how the app behaves rather than about
 * a panel or a server. Deliberately small: a setting is a promise to keep
 * supporting both answers, so each one here exists because two reasonable
 * people wanted different things, not because a default was hard to pick.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const go = () => (window.go && window.go.main && window.go.main.App) || null;
    const esc = (v) => window.Shell.fmt.escapeHtml(v);

    let settings = null;

    async function render() {
        const target = $('appSettingsBody');
        if (!target) return;

        const api = go();
        if (!api || !api.GetAppSettings) {
            target.innerHTML = '<div class="preview-empty">Not connected</div>';
            return;
        }

        try {
            settings = await api.GetAppSettings();
        } catch (err) {
            target.innerHTML = '<div class="empty-state">' +
                '<div class="empty-state-title">Settings could not be read</div>' +
                '<div class="empty-state-hint">' + esc(String(err)) + '</div></div>';
            return;
        }

        const typed = settings.delete_confirm !== 'double';

        target.innerHTML =
            '<div class="eyebrow" style="margin-bottom:9px">Deleting</div>' +
            '<div class="card card-pad">' +
                '<div class="list-sub" style="margin-bottom:12px">' +
                'A delete always shows what would go, and always keeps a copy when this ' +
                'server’s recycle bin is on. This is only about how much is asked of you ' +
                'before it runs.</div>' +

                option('deleteConfirm', 'typed', typed,
                    'Type DELETE when it cannot be undone',
                    'Two clicks for anything the recycle bin has a full copy of. The word ' +
                    'is asked for when the bin is off for this server, when the selection ' +
                    'is too big for it, or when the file is one the server needs.') +

                option('deleteConfirm', 'double', !typed,
                    'Always just two clicks',
                    'Never asks for the word. The dialog still lists what is going and ' +
                    'still says plainly when none of it can be restored — you just do not ' +
                    'have to type anything.') +
            '</div>' +

            '<div class="eyebrow" style="margin:20px 0 9px">Recycle bin</div>' +
            '<div class="card card-pad">' +
                '<label class="check"><input type="checkbox" id="binDefault"' +
                (settings.recycle_bin_default ? ' checked' : '') + '>' +
                '<span>Keep a local copy of deleted files by default</span></label>' +
                '<div class="form-hint" style="margin-top:6px">' +
                'For servers you have not set one way or the other. Individual servers are ' +
                'set in the Vault.</div>' +
            '</div>' +

            '<div class="eyebrow" style="margin:20px 0 9px">Transfers</div>' +
            '<div class="card card-pad">' +
                '<div class="form-group" style="max-width:220px">' +
                '<label>Parallel SFTP transfers</label>' +
                '<input type="number" id="sftpStreams" min="1" max="16" value="' +
                esc(String(settings.sftp_streams)) + '"></div>' +
                '<div class="form-hint">' +
                'How many files move at once, and how many connections a large file is ' +
                'split across. Six suits most connections; more is not always faster, and ' +
                'a panel may not welcome it.</div>' +
            '</div>';
    }

    function option(name, value, on, title, hint) {
        return '<label class="setting-option' + (on ? ' is-on' : '') + '">' +
            '<input type="radio" name="' + esc(name) + '" value="' + esc(value) + '"' +
            (on ? ' checked' : '') + '>' +
            '<span class="list-main">' +
            '<span class="list-title">' + esc(title) + '</span>' +
            '<span class="list-sub">' + hint + '</span>' +
            '</span></label>';
    }

    async function save(key, value) {
        const api = go();
        if (!api) return;
        try {
            await api.SetAppSetting(key, String(value));
            // Re-read rather than assume: the Go side is what decides whether a
            // value was acceptable, and it is the only thing that knows.
            settings = await api.GetAppSettings();
            window.UX.toast.ok('Saved', { duration: 1400 });
        } catch (err) {
            window.UX.toast.bad(String(err));
            render();
        }
    }

    document.addEventListener('change', async (e) => {
        const el = e.target;
        if (!el || !$('appSettingsBody') || !$('appSettingsBody').contains(el)) return;

        if (el.name === 'deleteConfirm') {
            await save('deleteConfirm', el.value);
            render();
            return;
        }
        if (el.id === 'binDefault') {
            try {
                await go().SetRecycleBinDefault(el.checked);
                window.UX.toast.ok('Saved', { duration: 1400 });
            } catch (err) {
                window.UX.toast.bad(String(err));
                render();
            }
            return;
        }
        if (el.id === 'sftpStreams') {
            await save('sftpStreams', el.value);
        }
    });

    document.addEventListener('tab:show', (e) => {
        if (e.detail !== 'appsettings') return;
        render();
    });

    window.AppSettings = {
        render: render,
        // Read by the delete flow. Cached, because it is wanted at the moment a
        // dialog goes up and a round trip there would be a visible pause — the
        // plan carries the same value, so this is only the fallback.
        deleteConfirm: () => (settings && settings.delete_confirm) || 'typed',
        sftpStreams: () => (settings && settings.sftp_streams) || 6,
        reload: async () => {
            const api = go();
            if (!api || !api.GetAppSettings) return;
            try { settings = await api.GetAppSettings(); } catch (err) { /* defaults */ }
        }
    };
}());
