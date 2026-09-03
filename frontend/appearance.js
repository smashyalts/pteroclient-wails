/**
 * The Look tab: pick a theme, or build one.
 *
 * Editing writes straight to the live tokens so the whole window is the
 * preview — a swatch of six squares tells you nothing about whether the file
 * list is readable. Cancelling puts back whatever was actually selected.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const esc = (v) => String(v == null ? '' : v).replace(/[&<>"']/g,
        (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

    // The theme being edited, or null when only picking. Held here rather than
    // saved on every keystroke, so a half-finished edit is not persisted.
    let draft = null;

    function contrast(fg, bg) {
        const lum = (hex) => {
            const m = /^#?([0-9a-f]{6})$/i.exec(String(hex || '').trim());
            if (!m) return 0;
            const n = parseInt(m[1], 16);
            const parts = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map(v => v / 255)
                .map(v => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
            return 0.2126 * parts[0] + 0.7152 * parts[1] + 0.0722 * parts[2];
        };
        const a = lum(fg), b = lum(bg);
        const hi = Math.max(a, b), lo = Math.min(a, b);
        return (hi + 0.05) / (lo + 0.05);
    }

    function render() {
        const target = $('appearanceBody');
        if (!target || !window.Themes) return;

        const themes = window.Themes.list();
        const currentID = window.Themes.current();
        let html = '';

        /* ---- the picker ---- */
        html += '<div class="eyebrow" style="margin-bottom:9px">Theme</div>';
        // 250px, not 215: at the smaller size "Copy and edit" beside "Use" ran
        // out of the card, and the name and its badge were clipped.
        html += '<div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(250px,1fr));gap:10px">';
        themes.forEach((t) => {
            const on = t.id === currentID && !draft;
            const k = t.tokens;
            html += '<div class="card theme-card' + (on ? ' is-current' : '') + '" data-theme="' + esc(t.id) + '">' +
                '<div class="theme-swatch" style="background:' + esc(k['bg-primary']) + '">' +
                '<div style="background:' + esc(k['bg-secondary']) + ';border-color:' + esc(k['line-strong']) + '">' +
                '<span style="color:' + esc(k['text-primary']) + '">Aa</span>' +
                '<span style="color:' + esc(k['text-dim']) + '">1.2 KB</span>' +
                '</div>' +
                '<div class="theme-dots">' +
                ['accent', 'success', 'warning', 'danger'].map(key =>
                    '<i style="background:' + esc(k[key]) + '"></i>').join('') +
                '</div></div>' +
                '<div class="card-pad" style="padding:9px 11px 11px">' +
                '<div class="list-title">' + esc(t.name) +
                (on ? ' <span class="list-badge">in use</span>' : '') +
                (t.builtIn ? '' : ' <span class="list-badge">yours</span>') + '</div>' +
                '<div class="list-sub" style="margin-top:3px">' + esc(t.note) + '</div>' +
                '<div style="display:flex;flex-wrap:wrap;gap:6px;margin-top:9px">' +
                '<button class="sm" data-use="' + esc(t.id) + '" type="button">Use</button>' +
                '<button class="sm" data-edit="' + esc(t.id) + '" type="button">' +
                (t.builtIn ? 'Copy and edit' : 'Edit') + '</button>' +
                (t.builtIn ? '' : '<button class="sm danger" data-del="' + esc(t.id) + '" type="button">Delete</button>') +
                '</div></div></div>';
        });
        html += '</div>';

        /* ---- the editor ---- */
        if (draft) {
            html += '<div class="eyebrow" style="margin:20px 0 9px">Editing</div>';
            html += '<div class="card card-pad">' +
                '<div class="form-group"><label>Name</label>' +
                '<input type="text" id="themeName" value="' + esc(draft.name) + '"></div>' +
                '<div class="list-sub" style="margin-bottom:14px">' +
                'The window is the preview — every change here applies straight away. ' +
                'Nothing is stored until you press Save.</div>';

            let group = '';
            window.Themes.tokens.forEach((tok) => {
                if (tok.group !== group) {
                    group = tok.group;
                    html += '<div class="eyebrow" style="margin:16px 0 8px">' + esc(group) + '</div>';
                }
                const value = draft.tokens[tok.key] || '#000000';
                // Text against the surface it actually sits on, so the number
                // means something rather than being decoration.
                let ratio = '';
                if (tok.key.indexOf('text-') === 0) {
                    const against = draft.tokens['bg-secondary'] || '#000000';
                    const r = contrast(value, against);
                    const bad = tok.key === 'text-ghost' ? false : r < 4.5;
                    ratio = '<span class="theme-ratio' + (bad ? ' is-low' : '') + '" title="' +
                        (bad ? 'Below 4.5:1 against the panel background — hard to read'
                             : 'Contrast against the panel background') + '">' +
                        r.toFixed(1) + ':1</span>';
                }
                html += '<div class="theme-row">' +
                    '<input type="color" class="theme-pick" data-token="' + esc(tok.key) + '" value="' + esc(value) + '">' +
                    '<span class="list-main"><span class="list-title">' + esc(tok.label) + ratio + '</span>' +
                    '<span class="list-sub">' + esc(tok.hint) + '</span></span>' +
                    '<input type="text" class="theme-hex mono" data-token="' + esc(tok.key) + '" ' +
                    'value="' + esc(value) + '" spellcheck="false" autocomplete="off">' +
                    '</div>';
            });

            html += '<div style="display:flex;gap:8px;margin-top:18px">' +
                '<button class="primary" id="themeSaveBtn" type="button">Save</button>' +
                '<button id="themeCancelBtn" type="button">Cancel</button>' +
                '<div class="spacer"></div>' +
                '<button id="themeImportBtn" type="button">Paste JSON</button>' +
                '<button id="themeExportBtn" type="button">Copy as JSON</button>' +
                '</div></div>';
        }

        target.innerHTML = html;
    }

    /* ------------------------------------------------------------- editing */

    function startEdit(id) {
        const theme = window.Themes.get(id);
        if (!theme) return;
        draft = {
            // A built-in is never overwritten; editing one starts a copy.
            id: theme.builtIn ? window.Themes.freeID(theme.name + ' copy') : theme.id,
            name: theme.builtIn ? theme.name + ' copy' : theme.name,
            tokens: Object.assign({}, theme.tokens),
            from: theme.builtIn ? theme.name : ''
        };
        window.Themes.preview(draft.tokens);
        render();
        const body = $('appearanceBody');
        if (body) body.scrollTop = body.scrollHeight;
    }

    function stopEdit() {
        draft = null;
        window.Themes.restore();
        render();
    }

    function setToken(key, value) {
        if (!draft) return;
        if (!/^#[0-9a-f]{6}$/i.test(value)) return;
        draft.tokens[key] = value.toLowerCase();
        window.Themes.preview(draft.tokens);
    }

    /* -------------------------------------------------------------- events */

    document.addEventListener('tab:show', (e) => {
        if (e.detail !== 'appearance') return;
        render();
    });

    document.addEventListener('click', async (e) => {
        const btn = e.target.closest('button');
        if (!btn) return;

        if (btn.dataset.use) {
            draft = null;
            window.Themes.use(btn.dataset.use);
            render();
            return;
        }

        if (btn.dataset.edit) return startEdit(btn.dataset.edit);

        if (btn.dataset.del) {
            const theme = window.Themes.get(btn.dataset.del);
            const ok = await window.Shell.dialog.confirm('Delete this theme?',
                'Removes "' + esc(theme ? theme.name : btn.dataset.del) + '". Built-in themes are not affected.',
                { danger: true, confirmLabel: 'Delete' });
            if (!ok) return;
            window.Themes.remove(btn.dataset.del);
            draft = null;
            render();
            return;
        }

        if (btn.id === 'themeNewBtn') return startEdit(window.Themes.current());
        if (btn.id === 'themeCancelBtn') return stopEdit();

        if (btn.id === 'themeSaveBtn') {
            if (!draft) return;
            const nameBox = $('themeName');
            const name = ((nameBox && nameBox.value) || '').trim();
            if (!name) {
                await window.Shell.dialog.confirm('Name it first', 'A theme needs a name to be saved.',
                    { confirmLabel: 'OK' });
                return;
            }
            draft.name = name;
            window.Themes.save(draft.id, name, draft.tokens, draft.from ? 'Based on ' + draft.from : 'Your own');
            window.Themes.use(draft.id);
            draft = null;
            render();
            window.UX.toast.ok('Saved "' + name + '"');
            return;
        }

        if (btn.id === 'themeImportBtn') {
            if (!draft) return;
            // A box rather than reading the clipboard directly: clipboard read
            // needs a permission this does not otherwise ask for, and a box
            // also takes a theme someone pasted into a chat message.
            const typed = await window.Shell.dialog.form('Paste a theme', [
                { name: 'json', label: 'Theme JSON', type: 'textarea', rows: 10, mono: true,
                  placeholder: '{ "name": "...", "tokens": { ... } }' }
            ], { confirmLabel: 'Apply' });
            if (!typed) return;

            let parsed;
            try {
                parsed = JSON.parse(String(typed.json || ''));
            } catch (err) {
                await window.Shell.dialog.confirm('That is not JSON',
                    esc(String(err)), { confirmLabel: 'OK' });
                return;
            }

            // Accept either the exported shape or a bare token map, since a
            // half-remembered paste is more likely than a wrong one.
            const tokens = (parsed && parsed.tokens) ? parsed.tokens : parsed;
            if (!tokens || typeof tokens !== 'object') {
                await window.Shell.dialog.confirm('Nothing to apply',
                    'That JSON has no colours in it.', { confirmLabel: 'OK' });
                return;
            }

            // Only keys this app knows, and only values that are colours. A
            // paste is untrusted text, and these end up in a style attribute.
            const known = new Set(window.Themes.tokens.map(t => t.key));
            let taken = 0;
            let ignored = 0;
            Object.keys(tokens).forEach((key) => {
                const value = String(tokens[key] || '').trim();
                if (!known.has(key) || !/^#[0-9a-f]{6}$/i.test(value)) {
                    ignored++;
                    return;
                }
                draft.tokens[key] = value.toLowerCase();
                taken++;
            });

            if (!taken) {
                await window.Shell.dialog.confirm('Nothing to apply',
                    'None of those keys are colours this app uses.', { confirmLabel: 'OK' });
                return;
            }

            if (typeof parsed.name === 'string' && parsed.name.trim()) {
                draft.name = parsed.name.trim().slice(0, 60);
            }

            window.Themes.preview(draft.tokens);
            render();
            window.UX.toast.ok(taken + ' colour(s) applied' +
                (ignored ? ', ' + ignored + ' ignored' : '') +
                '. Nothing is stored until you press Save.');
            return;
        }

        if (btn.id === 'themeExportBtn') {
            if (!draft) return;
            const json = JSON.stringify({ name: draft.name, tokens: draft.tokens }, null, 2);
            try {
                await navigator.clipboard.writeText(json);
                window.UX.toast.ok('Copied this theme to the clipboard');
            } catch (err) {
                // Clipboard access can be refused; showing it beats losing it.
                await window.Shell.dialog.confirm('Here it is', '<pre class="mono" style="max-height:260px;' +
                    'overflow:auto;font-size:11.5px">' + esc(json) + '</pre>',
                    { html: true, confirmLabel: 'OK' });
            }
        }
    });

    document.addEventListener('input', (e) => {
        const el = e.target;
        if (!el || !el.dataset || !el.dataset.token || !draft) return;

        if (el.classList.contains('theme-pick')) {
            setToken(el.dataset.token, el.value);
            const hex = document.querySelector('.theme-hex[data-token="' + el.dataset.token + '"]');
            if (hex) hex.value = el.value;
            return;
        }

        if (el.classList.contains('theme-hex')) {
            const value = el.value.trim();
            // Typed one character at a time, so an incomplete value is normal
            // rather than an error worth reporting.
            if (!/^#[0-9a-f]{6}$/i.test(value)) return;
            setToken(el.dataset.token, value);
            const pick = document.querySelector('.theme-pick[data-token="' + el.dataset.token + '"]');
            if (pick) pick.value = value.toLowerCase();
        }
    });

    window.Appearance = { render: render, editing: () => !!draft };
})();
