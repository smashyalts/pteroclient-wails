/**
 * Cross-cutting interaction: toasts, hotkeys, the command palette, zoom, and
 * the dismissal rules every overlay shares.
 *
 * Hotkeys live here rather than next to the things they act on. Three files
 * had their own document-level keydown handlers, which is how Ctrl+S in the
 * split editor also saved the main editor's file — nothing could see the whole
 * set. Commands register themselves; this decides what fires.
 */
(function () {
    'use strict';

    const $ = (id) => document.getElementById(id);
    const esc = (v) => (window.Shell ? window.Shell.fmt.escapeHtml(v) : String(v == null ? '' : v));

    /* ---------------------------------------------------------------- toasts */

    const Toast = {
        show(message, opts) {
            const o = opts || {};
            const host = $('toastHost');
            if (!host) return null;

            const el = document.createElement('div');
            el.className = 'toast' + (o.tone ? ' toast-' + o.tone : '');

            let html = '<span class="toast-body">' + (o.html ? message : esc(message)) + '</span>';
            if (o.action) html += '<button class="toast-action" type="button">' + esc(o.action.label) + '</button>';
            html += '<button class="toast-close" type="button" aria-label="Dismiss">&times;</button>';
            el.innerHTML = html;

            let timer = null;
            const dismiss = () => {
                if (timer) clearTimeout(timer);
                el.classList.add('leaving');
                setTimeout(() => el.remove(), 200);
            };

            const actionBtn = el.querySelector('.toast-action');
            if (actionBtn) {
                actionBtn.addEventListener('click', () => {
                    dismiss();
                    o.action.run();
                });
            }
            el.querySelector('.toast-close').addEventListener('click', dismiss);

            // An action nobody has taken yet should not vanish on a timer.
            const life = o.action ? (o.duration || 9000) : (o.duration || 4200);
            timer = setTimeout(dismiss, life);

            host.appendChild(el);
            requestAnimationFrame(() => el.classList.add('in'));
            return { dismiss };
        },

        ok(message, opts) { return Toast.show(message, Object.assign({ tone: 'ok' }, opts)); },
        warn(message, opts) { return Toast.show(message, Object.assign({ tone: 'warn' }, opts)); },
        bad(message, opts) { return Toast.show(message, Object.assign({ tone: 'bad' }, opts)); }
    };

    /* ----------------------------------------------------------------- zoom */

    const ZOOM_STEPS = [0.7, 0.8, 0.9, 1, 1.1, 1.25, 1.4, 1.6, 1.8, 2];
    let zoomIndex = 3;

    function applyZoom(persist) {
        const scale = ZOOM_STEPS[zoomIndex];
        // A CSS variable, not an inline zoom: the stylesheet divides every
        // viewport-sized rule by it, so the layout shrinks with the content
        // instead of overflowing past the bottom of the window.
        document.documentElement.style.setProperty('--ui-scale', String(scale));
        if (persist !== false) {
            try { localStorage.setItem('uiZoom', String(scale)); } catch (err) { /* private mode */ }
        }
        return scale;
    }

    function nudgeZoom(delta) {
        const next = Math.min(ZOOM_STEPS.length - 1, Math.max(0, zoomIndex + delta));
        if (next === zoomIndex) return;
        zoomIndex = next;
        Toast.show(Math.round(applyZoom() * 100) + '%', { duration: 1100 });
    }

    function resetZoom() {
        zoomIndex = ZOOM_STEPS.indexOf(1);
        Toast.show('Zoom reset to 100%', { duration: 1100 });
        applyZoom();
    }

    function restoreZoom() {
        let stored = null;
        try { stored = localStorage.getItem('uiZoom'); } catch (err) { /* private mode */ }
        const scale = parseFloat(stored);
        const found = ZOOM_STEPS.indexOf(scale);
        if (found !== -1) zoomIndex = found;
        applyZoom(false);
    }

    /* -------------------------------------------------------------- hotkeys */

    // Canonical form: modifiers in a fixed order, then the key. Building this
    // from both the event and the stored binding means they compare directly.
    function normalizeKey(key) {
        if (key === ' ') return 'Space';
        if (key === '+') return '=';          // Ctrl+Shift+= reports as '+'
        if (key === '_') return '-';
        if (key.length === 1) return key.toUpperCase();
        return key;
    }

    function comboOf(e) {
        const parts = [];
        if (e.ctrlKey || e.metaKey) parts.push('Ctrl');
        if (e.altKey) parts.push('Alt');
        if (e.shiftKey) parts.push('Shift');
        const key = normalizeKey(e.key);
        // A bare modifier is not a combo.
        if (['Control', 'Meta', 'Alt', 'Shift'].indexOf(e.key) !== -1) return null;
        parts.push(key);
        return parts.join('+');
    }

    function normalizeCombo(combo) {
        const parts = String(combo).split('+').map((p) => p.trim()).filter(Boolean);
        const mods = { Ctrl: false, Alt: false, Shift: false };
        let key = '';
        parts.forEach((p) => {
            const lower = p.toLowerCase();
            if (lower === 'ctrl' || lower === 'cmd' || lower === 'meta') mods.Ctrl = true;
            else if (lower === 'alt') mods.Alt = true;
            else if (lower === 'shift') mods.Shift = true;
            else key = normalizeKey(p);
        });
        if (!key) return null;
        const out = [];
        if (mods.Ctrl) out.push('Ctrl');
        if (mods.Alt) out.push('Alt');
        if (mods.Shift) out.push('Shift');
        out.push(key);
        return out.join('+');
    }

    const commands = new Map();   // id -> {id, label, group, run, when, defaultKey, allowInField}
    let bindings = {};            // id -> combo (user overrides merged over defaults)

    function loadBindings() {
        let stored = {};
        try { stored = JSON.parse(localStorage.getItem('hotkeys') || '{}'); } catch (err) { stored = {}; }
        bindings = {};
        commands.forEach((cmd, id) => {
            const override = stored[id];
            bindings[id] = override === undefined ? cmd.defaultKey : override;
        });
    }

    function saveBindings() {
        const overrides = {};
        commands.forEach((cmd, id) => {
            if (bindings[id] !== cmd.defaultKey) overrides[id] = bindings[id];
        });
        try { localStorage.setItem('hotkeys', JSON.stringify(overrides)); } catch (err) { /* private mode */ }
    }

    /**
     * register({id, label, group, key, run, when, allowInField})
     *
     * `when` gates the command on app state; a command whose `when` is false is
     * skipped and the keystroke falls through, so Ctrl+W can close a file tab
     * in Files without swallowing the key on the Console tab.
     */
    function register(cmd) {
        commands.set(cmd.id, {
            id: cmd.id,
            label: cmd.label,
            group: cmd.group || 'General',
            run: cmd.run,
            when: cmd.when || (() => true),
            defaultKey: normalizeCombo(cmd.key) || '',
            allowInField: !!cmd.allowInField,
            hidden: !!cmd.hidden
        });
        if (bindings[cmd.id] === undefined) bindings[cmd.id] = commands.get(cmd.id).defaultKey;
    }

    function isTypingTarget(el) {
        if (!el) return false;
        const tag = el.tagName;
        return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable;
    }

    // Monaco swallows its own keys; anything inside the editor container is
    // left alone apart from the commands that opt in.
    function inCodeEditor(el) {
        return !!(el && el.closest && el.closest('#editor, .monaco-editor'));
    }

    let recording = null;   // set while the hotkey editor is capturing a combo

    document.addEventListener('keydown', (e) => {
        if (recording) {
            const combo = comboOf(e);
            if (!combo) return;
            e.preventDefault();
            e.stopPropagation();
            recording(combo === 'Escape' ? null : combo);
            return;
        }

        const combo = comboOf(e);
        if (!combo) return;

        const typing = isTypingTarget(e.target) || inCodeEditor(e.target);

        for (const cmd of commands.values()) {
            if (bindings[cmd.id] !== combo) continue;
            if (typing && !cmd.allowInField) continue;
            if (!cmd.when()) continue;
            e.preventDefault();
            e.stopPropagation();
            try {
                cmd.run();
            } catch (err) {
                console.error('Hotkey ' + cmd.id + ' failed:', err);
                Toast.bad(cmd.label + ' failed: ' + err);
            }
            return;
        }
    }, true);   // capture: Monaco eats Ctrl+K and friends on the way up

    /* ------------------------------------------------------- command palette */

    let paletteItems = [];
    let paletteIndex = 0;

    function paletteOpen(mode) {
        const overlay = $('palette');
        if (!overlay) return;

        overlay.classList.add('show');
        const input = $('paletteInput');
        refreshAllServers();
        // No '>' prefix any more. Ctrl+K opened on servers only, so jumping to
        // a folder meant a different shortcut; now both are one list and '>'
        // is still there for anyone who wants servers alone.
        input.value = '';
        input.placeholder = mode === 'servers'
            ? 'Go to a server or a folder — or type a path like /plugins'
            : 'Type a command, a server, or a path…';
        paletteBuild('');
        setTimeout(() => input.focus(), 20);
    }

    function paletteClose() {
        const overlay = $('palette');
        if (overlay) overlay.classList.remove('show');
    }

    // Every server on every configured panel. Ctrl+K used to read the active
    // panel's dropdown only, so reaching a server on another panel meant
    // switching panels by hand first.
    let allServers = [];

    function refreshAllServers() {
        if (!(window.go && window.go.main && window.go.main.App.ListAllServers)) return;
        window.go.main.App.ListAllServers().then((refs) => {
            allServers = refs || [];
            // The list arrives after the palette has drawn, so redraw it.
            const overlay = $('palette');
            if (overlay && overlay.classList.contains('show')) {
                const input = $('paletteInput');
                paletteBuild(input ? input.value : '');
            }
        }).catch(() => { /* one unreachable panel should not empty the list */ });
    }

    /**
     * The explorer Ctrl+K acts on.
     *
     * With the split view open there are two, and the one being worked in is
     * the one that should move. Without it there is only the main window's.
     */
    function navTarget() {
        const split = window.SplitView;
        if (split && split.isActive() && split.serverID()) {
            return {
                split: true,
                serverID: split.serverID(),
                currentPath: split.currentPath(),
                where: 'the ' + split.focusedSide() + ' workspace',
                goTo: (path) => split.goTo(path)
            };
        }
        const app = window.app;
        if (!app) return null;
        const sel = $('serverDropdown');
        return {
            split: false,
            serverID: sel ? sel.value : '',
            currentPath: app.currentPath || '/',
            where: '',
            goTo: (path) => {
                window.Shell.showTab('files');
                app.loadFiles(path);
            }
        };
    }

    function folderItems(target) {
        if (!target || !target.serverID || !window.Folders) return [];
        // Most recently seen first, so somewhere just visited is at the top
        // before a single character is typed.
        return window.Folders.list(target.serverID).slice(0, 400).map((path) => ({
            kind: 'folder',
            icon: 'folder',
            label: path,
            hint: target.where,
            current: path === target.currentPath,
            run: () => target.goTo(path)
        }));
    }

    function paletteSources() {
        const items = [];
        const split = window.SplitView;
        const inSplit = !!(split && split.isActive());

        // With the split open a server goes into the workspace being worked
        // in, not into the main window behind it.
        const useServer = (id, name, panel) => {
            if (inSplit) return split.useServer(id, name, panel);
            const sel = $('serverDropdown');
            if (sel && Array.from(sel.options).some(o => o.value === id)) {
                sel.value = id;
                sel.dispatchEvent(new Event('change'));
                return;
            }
            // Not on the active panel: SwitchServer moves to the right one
            // first, so picking it from here just works.
            if (window.app) window.app.switchServer(id);
        };

        // Servers first: switching is the thing this is opened for most.
        const servers = $('serverDropdown');
        const onThisPanel = new Set();
        const where = inSplit ? ' · ' + split.focusedSide() : '';
        if (servers) {
            Array.from(servers.options).forEach((opt) => {
                if (!opt.value) return;
                onThisPanel.add(opt.value);
                items.push({
                    kind: 'server',
                    icon: 'server',
                    label: opt.textContent,
                    hint: opt.value + where,
                    current: !inSplit && opt.value === servers.value,
                    run: () => useServer(opt.value, opt.textContent, opt.dataset.panel || '')
                });
            });
        }

        // Then everything on the other panels.
        allServers.forEach((ref) => {
            if (!ref || !ref.id || onThisPanel.has(ref.id)) return;
            items.push({
                kind: 'server',
                icon: 'server',
                label: (ref.name || ref.id) + ' · ' + (ref.panel || ''),
                hint: ref.id + where,
                run: () => useServer(ref.id, ref.name || ref.id, ref.panel || '')
            });
        });

        // Folders next: the other thing this is opened for.
        folderItems(navTarget()).forEach((item) => items.push(item));

        const panels = $('panelDropdown');
        if (panels) {
            Array.from(panels.options).forEach((opt) => {
                if (!opt.value) return;
                items.push({
                    kind: 'panel',
                    icon: 'sliders',
                    label: 'Panel: ' + opt.textContent,
                    hint: opt.dataset.sub || '',
                    current: opt.value === panels.value,
                    run: () => {
                        panels.value = opt.value;
                        panels.dispatchEvent(new Event('change'));
                    }
                });
            });
        }

        commands.forEach((cmd) => {
            if (cmd.hidden) return;
            items.push({
                kind: 'command',
                icon: 'bolt',
                label: cmd.label,
                hint: bindings[cmd.id] || '',
                run: cmd.run
            });
        });

        return items;
    }

    /**
     * Does this row match what was typed, and how well?
     *
     * A plain substring test meant "waffle survival" found nothing on a server
     * called "[WaffleSMP] Survival" — the words are there but not adjacent, and
     * nobody types the brackets. Every whitespace-separated word has to appear
     * somewhere in the row, in any order.
     *
     * Returns a score so better matches sort first, or -1 for no match:
     *   - a word starting a word in the row beats one buried mid-word
     *   - matches early in the row beat matches late in it
     *   - a shorter row beats a longer one that happens to contain the same
     *     words, so "plugins" ranks above "plugins/Essentials/config"
     */
    function scoreMatch(haystack, words) {
        if (!words.length) return 0;

        const lower = haystack.toLowerCase();
        let score = 0;

        for (const word of words) {
            const at = lower.indexOf(word);
            if (at === -1) return -1;

            // Starting a word is what somebody typing an abbreviation means.
            const before = at === 0 ? '' : lower.charAt(at - 1);
            if (at === 0) score += 40;
            else if (/[^a-z0-9]/.test(before)) score += 25;

            // Earlier is better, but only mildly.
            score += Math.max(0, 12 - Math.floor(at / 4));
        }

        // Prefer the shorter of two rows that both matched.
        score += Math.max(0, 30 - Math.floor(lower.length / 3));
        return score;
    }

    function paletteBuild(query) {
        const list = $('paletteList');
        if (!list) return;

        let q = String(query || '').trim();
        let only = null;
        if (q.indexOf('>') === 0) {
            only = 'server';
            q = q.slice(1).trim();
        }

        // A path, typed out. '/' is from the root, '~/' from wherever the
        // explorer is now, which is the shorter one to type most of the time.
        const target = navTarget();
        const typedPath = window.Folders
            ? window.Folders.resolve(q, target ? target.currentPath : '/')
            : null;

        // Words, not one run of characters: "waffle survival" should find
        // "[WaffleSMP] Survival".
        const words = q.toLowerCase().split(/\s+/).filter(Boolean);
        const scored = [];
        paletteSources().forEach((item) => {
            if (only && item.kind !== only) return;
            if (!words.length) {
                scored.push({ item: item, score: 0 });
                return;
            }
            const score = scoreMatch(item.label + ' ' + item.hint, words);
            if (score >= 0) scored.push({ item: item, score: score });
        });

        // Stable within a score, so the source order still decides ties —
        // servers before folders before commands.
        scored.sort((a, b) => b.score - a.score);
        paletteItems = scored.map((s) => s.item);

        if (typedPath && target) {
            // First, and only once: a path already in the list is the same
            // destination, so it is promoted rather than duplicated.
            const already = paletteItems.findIndex(
                (item) => item.kind === 'folder' && item.label === typedPath);
            if (already !== -1) paletteItems.splice(already, 1);

            paletteItems.unshift({
                kind: 'folder',
                icon: 'folder',
                label: typedPath,
                hint: 'go there' + (target.where ? ' in ' + target.where : ''),
                run: () => target.goTo(typedPath)
            });
        }

        paletteIndex = 0;
        paletteRender();
    }

    function paletteRender() {
        const list = $('paletteList');
        if (!list) return;

        if (!paletteItems.length) {
            list.innerHTML = '<div class="palette-empty">Nothing matches that</div>';
            return;
        }

        list.innerHTML = paletteItems.map((item, i) => (
            '<div class="palette-row' + (i === paletteIndex ? ' active' : '') + '" data-index="' + i + '">' +
            (window.Icons ? window.Icons.svg(item.icon, 'ic-14') : '') +
            '<span class="palette-label">' + esc(item.label) + '</span>' +
            (item.current ? '<span class="palette-current">current</span>' : '') +
            (item.hint ? '<span class="palette-hint mono">' + esc(item.hint) + '</span>' : '') +
            '</div>'
        )).join('');

        const active = list.querySelector('.palette-row.active');
        if (active && active.scrollIntoView) active.scrollIntoView({ block: 'nearest' });
    }

    function paletteMove(delta) {
        if (!paletteItems.length) return;
        paletteIndex = (paletteIndex + delta + paletteItems.length) % paletteItems.length;
        paletteRender();
    }

    /**
     * Tab: the highlighted row, in the box.
     *
     * For a folder that means the path, with a trailing slash so the next
     * keystroke carries on down the tree rather than replacing it. For anything
     * else it is the label, which is at least a starting point.
     */
    function paletteComplete() {
        const item = paletteItems[paletteIndex];
        const input = $('paletteInput');
        if (!item || !input) return;

        let text = item.kind === 'folder' ? item.label : item.label;
        if (item.kind === 'folder' && text.charAt(text.length - 1) !== '/') text += '/';

        input.value = text;
        input.setSelectionRange(text.length, text.length);
        paletteBuild(text);
    }

    function paletteRun() {
        const item = paletteItems[paletteIndex];
        if (!item) return;
        paletteClose();
        item.run();
    }

    function wirePalette() {
        const overlay = $('palette');
        if (!overlay) return;

        const input = $('paletteInput');
        input.addEventListener('input', () => paletteBuild(input.value));
        input.addEventListener('keydown', (e) => {
            if (e.key === 'ArrowDown') { e.preventDefault(); paletteMove(1); }
            else if (e.key === 'ArrowUp') { e.preventDefault(); paletteMove(-1); }
            else if (e.key === 'Enter') { e.preventDefault(); paletteRun(); }
            else if (e.key === 'Escape') { e.preventDefault(); paletteClose(); }
            else if (e.key === 'Tab') {
                // Tab completes rather than jumping focus out of the box: put
                // the highlighted row into the input so it can be edited or
                // gone deeper into, which is what a path list wants. Enter
                // still goes straight there.
                e.preventDefault();
                paletteComplete();
            }
        });

        $('paletteList').addEventListener('click', (e) => {
            const row = e.target.closest('.palette-row');
            if (!row) return;
            paletteIndex = Number(row.dataset.index) || 0;
            paletteRun();
        });

        overlay.addEventListener('mousedown', (e) => {
            if (e.target === overlay) paletteClose();
        });
    }

    /* -------------------------------------------------------- hotkey editor */

    function hotkeyGroups() {
        const groups = new Map();
        commands.forEach((cmd) => {
            if (!groups.has(cmd.group)) groups.set(cmd.group, []);
            groups.get(cmd.group).push(cmd);
        });
        return groups;
    }

    function renderHotkeys() {
        const body = $('hotkeyBody');
        if (!body) return;

        // A combo bound twice fires whichever command registered first, so the
        // clash is worth showing rather than leaving to be discovered.
        const used = {};
        Object.keys(bindings).forEach((id) => {
            if (!bindings[id]) return;
            (used[bindings[id]] = used[bindings[id]] || []).push(id);
        });

        let html = '';
        hotkeyGroups().forEach((cmds, group) => {
            html += '<div class="eyebrow" style="margin:16px 0 8px">' + esc(group) + '</div><div class="card">';
            cmds.forEach((cmd) => {
                const combo = bindings[cmd.id];
                const clash = combo && used[combo] && used[combo].length > 1;
                html += '<div class="list-row">' +
                    '<span class="list-main"><span class="list-title">' + esc(cmd.label) + '</span>' +
                    (clash ? '<span class="list-sub" style="color:var(--warning)">shares this key with another command</span>' : '') +
                    '</span>' +
                    '<span class="list-actions">' +
                    '<button class="sm mono" data-hotkey-record="' + esc(cmd.id) + '">' +
                    (combo ? esc(combo) : 'unbound') + '</button>' +
                    (combo !== cmd.defaultKey
                        ? '<button class="sm" data-hotkey-reset="' + esc(cmd.id) + '">Reset</button>'
                        : '') +
                    '<button class="sm" data-hotkey-clear="' + esc(cmd.id) + '">Clear</button>' +
                    '</span></div>';
            });
            html += '</div>';
        });

        body.innerHTML = html;
    }

    function openHotkeys() {
        const modal = $('hotkeyModal');
        if (!modal) return;
        renderHotkeys();
        modal.classList.add('show');
    }

    function wireHotkeyEditor() {
        const modal = $('hotkeyModal');
        if (!modal) return;

        modal.addEventListener('click', (e) => {
            const btn = e.target.closest('button');
            if (!btn) return;

            if (btn.dataset.hotkeyRecord) {
                const id = btn.dataset.hotkeyRecord;
                btn.textContent = 'press a key…';
                btn.classList.add('recording');
                recording = (combo) => {
                    recording = null;
                    if (combo !== null) bindings[id] = combo;
                    saveBindings();
                    renderHotkeys();
                };
                return;
            }
            if (btn.dataset.hotkeyReset) {
                const cmd = commands.get(btn.dataset.hotkeyReset);
                bindings[cmd.id] = cmd.defaultKey;
                saveBindings();
                renderHotkeys();
                return;
            }
            if (btn.dataset.hotkeyClear) {
                bindings[btn.dataset.hotkeyClear] = '';
                saveBindings();
                renderHotkeys();
                return;
            }
            if (btn.dataset.hotkeyResetAll) {
                commands.forEach((cmd) => { bindings[cmd.id] = cmd.defaultKey; });
                saveBindings();
                renderHotkeys();
                Toast.ok('Hotkeys back to their defaults');
            }
        });
    }

    /* ------------------------------------------------------ overlay dismissal */

    /**
     * Escape closes the topmost overlay, and a click on the backdrop closes the
     * one it belongs to. Previously only the generic dialog honoured either,
     * so the panel manager could only be closed with its × button.
     */
    function wireOverlays() {
        document.addEventListener('mousedown', (e) => {
            const modal = e.target.classList && e.target.classList.contains('modal') ? e.target : null;
            if (!modal || !modal.classList.contains('show')) return;
            // appDialog resolves a promise on close; let its own handler do it.
            if (modal.id === 'appDialog') return;
            modal.classList.remove('show');
        });

        document.addEventListener('keydown', (e) => {
            if (e.key !== 'Escape' || recording) return;

            const palette = $('palette');
            if (palette && palette.classList.contains('show')) {
                paletteClose();
                e.stopPropagation();
                return;
            }

            const open = Array.from(document.querySelectorAll('.modal.show'));
            if (!open.length) return;
            const top = open[open.length - 1];
            if (top.id === 'appDialog') return;   // handled where the promise lives
            top.classList.remove('show');
            e.stopPropagation();
        }, true);
    }

    /* ---------------------------------------------------------------- boot */

    function boot() {
        restoreZoom();
        wirePalette();
        wireHotkeyEditor();
        wireOverlays();
        loadBindings();
    }

    window.UX = {
        toast: Toast,
        registerCommand: register,
        bindingFor: (id) => bindings[id] || '',
        openPalette: paletteOpen,
        closePalette: paletteClose,
        openHotkeys: openHotkeys,
        zoomIn: () => nudgeZoom(1),
        zoomOut: () => nudgeZoom(-1),
        zoomReset: resetZoom,
        // body carries a CSS `zoom`, so a pointer event's clientX/clientY are
        // in unzoomed viewport pixels while anything positioned inside the body
        // is measured in zoomed ones. Anything placing an element at a pointer
        // has to divide by this.
        scale: () => ZOOM_STEPS[zoomIndex],
        refreshBindings: loadBindings
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', boot);
    } else {
        boot();
    }
})();
