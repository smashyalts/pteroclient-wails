/**
 * Themes.
 *
 * Every colour in the stylesheet already comes from a custom property on
 * :root, so a theme is nothing more than a set of values for the eighteen
 * tokens below. Applying one writes them as inline properties on the root
 * element, which beats the stylesheet without touching it.
 *
 * The tokens that are derived rather than chosen — the translucent accent
 * fills, the legacy aliases — are computed from the chosen ones, so a custom
 * theme cannot end up with an accent border that belongs to a different
 * accent.
 */
(function () {
    'use strict';

    // The editable set, in the order the editor lists them. `group` is the
    // heading it sits under; `hint` is what it actually affects, since a token
    // name on its own tells a user nothing.
    const TOKENS = [
        { key: 'bg-void',        group: 'Surfaces', label: 'Window backdrop',   hint: 'Behind everything, including the title bar' },
        { key: 'bg-primary',     group: 'Surfaces', label: 'Main background',   hint: 'The editor and the console' },
        { key: 'bg-secondary',   group: 'Surfaces', label: 'Panel background',  hint: 'The file list, cards, the rail' },
        { key: 'bg-raised',      group: 'Surfaces', label: 'Raised surface',    hint: 'Menus, dialogs, toasts' },
        { key: 'bg-hover',       group: 'Surfaces', label: 'Hover',             hint: 'A row or button under the pointer' },

        { key: 'line',           group: 'Lines',    label: 'Border',            hint: 'The ordinary divider between areas' },
        { key: 'line-strong',    group: 'Lines',    label: 'Strong border',     hint: 'Inputs and the edges of cards' },
        { key: 'line-bright',    group: 'Lines',    label: 'Bright border',     hint: 'Focus rings and active edges' },

        { key: 'text-primary',   group: 'Text',     label: 'Text',              hint: 'File names, editor content, headings' },
        { key: 'text-secondary', group: 'Text',     label: 'Secondary text',    hint: 'Body copy in dialogs' },
        { key: 'text-dim',       group: 'Text',     label: 'Dim text',          hint: 'Sizes, dates, the status bar' },
        { key: 'text-faint',     group: 'Text',     label: 'Faint text',        hint: 'Hints and captions' },
        { key: 'text-ghost',     group: 'Text',     label: 'Decoration',        hint: 'Separators and disabled glyphs — never words' },

        { key: 'accent',         group: 'Signal',   label: 'Accent',            hint: 'Selection, links, the primary button' },
        { key: 'accent-bright',  group: 'Signal',   label: 'Accent highlight',  hint: 'Hovered accent, focused text' },
        { key: 'success',        group: 'Signal',   label: 'Success',           hint: 'Start, saved, restored' },
        { key: 'danger',         group: 'Signal',   label: 'Danger',            hint: 'Delete, kill, unrecoverable' },
        { key: 'warning',        group: 'Signal',   label: 'Warning',           hint: 'Anything qualified rather than refused' }
    ];

    const BUILT_IN = {
        midnight: {
            name: 'Midnight',
            note: 'The default. Deep navy, blue accent.',
            tokens: {
                'bg-void': '#060911', 'bg-primary': '#080c16', 'bg-secondary': '#0a0f1c',
                'bg-raised': '#0d1421', 'bg-hover': '#172033',
                'line': '#161f31', 'line-strong': '#1d2739', 'line-bright': '#26324a',
                'text-primary': '#edf1f8', 'text-secondary': '#b6c2d6', 'text-dim': '#98a6bf',
                'text-faint': '#8b99b5', 'text-ghost': '#5d6b85',
                'accent': '#4b8ef7', 'accent-bright': '#7db4ff',
                'success': '#10b981', 'danger': '#ef4444', 'warning': '#f59e0b'
            }
        },
        abyss: {
            name: 'Abyss',
            note: 'True black, for OLED. Nothing glows that does not have to.',
            tokens: {
                'bg-void': '#000000', 'bg-primary': '#000000', 'bg-secondary': '#070707',
                'bg-raised': '#0e0e0e', 'bg-hover': '#1c1c1c',
                'line': '#1a1a1a', 'line-strong': '#252525', 'line-bright': '#343434',
                'text-primary': '#f2f2f2', 'text-secondary': '#c2c2c2', 'text-dim': '#a3a3a3',
                'text-faint': '#949494', 'text-ghost': '#616161',
                'accent': '#5b9bff', 'accent-bright': '#8fbcff',
                'success': '#22c55e', 'danger': '#f05252', 'warning': '#f5a524'
            }
        },
        slate: {
            name: 'Slate',
            note: 'Neutral grey, a little lighter. Less blue in the shadows.',
            tokens: {
                'bg-void': '#121417', 'bg-primary': '#171a1f', 'bg-secondary': '#1c2027',
                'bg-raised': '#22272f', 'bg-hover': '#2c323c',
                'line': '#2a3038', 'line-strong': '#343b45', 'line-bright': '#434c58',
                'text-primary': '#f0f2f5', 'text-secondary': '#c4cad3', 'text-dim': '#a6aeba',
                'text-faint': '#98a1ae', 'text-ghost': '#6b7482',
                'accent': '#61a0fa', 'accent-bright': '#93bfff',
                'success': '#34d399', 'danger': '#f87171', 'warning': '#fbbf24'
            }
        },
        contrast: {
            name: 'High contrast',
            note: 'Maximum separation between text and background, brighter lines.',
            tokens: {
                'bg-void': '#000208', 'bg-primary': '#01040c', 'bg-secondary': '#050a15',
                'bg-raised': '#0a1120', 'bg-hover': '#1b2740',
                'line': '#2b3a54', 'line-strong': '#3b4a68', 'line-bright': '#56698d',
                'text-primary': '#ffffff', 'text-secondary': '#dce4f0', 'text-dim': '#c3cee0',
                'text-faint': '#b3c0d6', 'text-ghost': '#7e8da8',
                'accent': '#79b1ff', 'accent-bright': '#a9cdff',
                'success': '#4ade80', 'danger': '#ff8080', 'warning': '#ffc94d'
            }
        },
        moss: {
            name: 'Moss',
            note: 'Warm dark green, amber accent. Nothing blue.',
            tokens: {
                'bg-void': '#0a0f0b', 'bg-primary': '#0e1410', 'bg-secondary': '#121a14',
                'bg-raised': '#18211a', 'bg-hover': '#232f26',
                'line': '#1f2a22', 'line-strong': '#28362b', 'line-bright': '#35473a',
                'text-primary': '#eef3ec', 'text-secondary': '#bfcdbc', 'text-dim': '#a3b3a1',
                'text-faint': '#95a693', 'text-ghost': '#66756a',
                'accent': '#d3a04a', 'accent-bright': '#f0c477',
                'success': '#5cc98a', 'danger': '#e06a5a', 'warning': '#e8b04b'
            }
        },
        daylight: {
            name: 'Daylight',
            note: 'Light. The one theme where the surfaces are brighter than the text.',
            tokens: {
                'bg-void': '#e7ebf1', 'bg-primary': '#f7f9fc', 'bg-secondary': '#eef2f7',
                'bg-raised': '#ffffff', 'bg-hover': '#dfe6f0',
                'line': '#d3dae5', 'line-strong': '#bfc9d8', 'line-bright': '#9fadc2',
                'text-primary': '#111826', 'text-secondary': '#2f3a4d', 'text-dim': '#4a5670',
                'text-faint': '#5b6880', 'text-ghost': '#8b96a8',
                'accent': '#1d63d1', 'accent-bright': '#0f4bb0',
                'success': '#0f7b4f', 'danger': '#c02626', 'warning': '#9a6100'
            }
        }
    };

    const STORE_KEY = 'themeChoice';
    const CUSTOM_KEY = 'themeCustom';

    /* ------------------------------------------------------------ storage */

    function readCustom() {
        try {
            const raw = localStorage.getItem(CUSTOM_KEY);
            const parsed = raw ? JSON.parse(raw) : {};
            return parsed && typeof parsed === 'object' ? parsed : {};
        } catch (err) {
            // A corrupt entry must not leave the app themeless.
            return {};
        }
    }

    function writeCustom(all) {
        try { localStorage.setItem(CUSTOM_KEY, JSON.stringify(all)); } catch (err) { /* private mode */ }
    }

    function all() {
        const out = {};
        Object.keys(BUILT_IN).forEach((id) => {
            out[id] = { id: id, name: BUILT_IN[id].name, note: BUILT_IN[id].note,
                        tokens: BUILT_IN[id].tokens, builtIn: true };
        });
        const custom = readCustom();
        Object.keys(custom).forEach((id) => {
            const t = custom[id];
            if (!t || !t.tokens) return;
            out[id] = { id: id, name: t.name || id, note: t.note || 'Your own', tokens: t.tokens, builtIn: false };
        });
        return out;
    }

    function current() {
        let id = 'midnight';
        try { id = localStorage.getItem(STORE_KEY) || 'midnight'; } catch (err) { /* private mode */ }
        const themes = all();
        return themes[id] ? id : 'midnight';
    }

    /* ------------------------------------------------------------- applying */

    // #rrggbb to "r, g, b", for the tokens that need an alpha of their own.
    function rgb(hex) {
        const m = /^#?([0-9a-f]{6})$/i.exec(String(hex || '').trim());
        if (!m) return '75, 142, 247';
        const n = parseInt(m[1], 16);
        return [(n >> 16) & 255, (n >> 8) & 255, n & 255].join(', ');
    }

    function isLight(hex) {
        const parts = rgb(hex).split(',').map(v => parseInt(v, 10) / 255);
        const lin = parts.map(v => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
        return (0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2]) > 0.45;
    }

    function apply(tokens) {
        const root = document.documentElement;
        TOKENS.forEach((t) => {
            const value = tokens[t.key];
            if (value) root.style.setProperty('--' + t.key, value);
        });

        // Derived, never stored: an accent fill that did not match its accent
        // was the one thing a hand-edited theme could not get right.
        const accent = tokens['accent'] || '#4b8ef7';
        root.style.setProperty('--accent-soft', 'rgba(' + rgb(accent) + ', 0.16)');
        root.style.setProperty('--accent-border', 'rgba(' + rgb(accent) + ', 0.55)');

        // The -text variants are the readable form of each signal, which on a
        // light background means darker rather than lighter.
        const light = isLight(tokens['bg-secondary'] || '#0a0f1c');
        root.style.setProperty('--success-text', tokens['success'] || '#10b981');
        root.style.setProperty('--danger-text', shift(tokens['danger'] || '#ef4444', light ? -0.18 : 0.28));
        root.style.setProperty('--warning-text', shift(tokens['warning'] || '#f59e0b', light ? -0.18 : 0.22));
        root.style.setProperty('--bg-tertiary', tokens['line-strong'] || '#1d2739');

        // Lets a rule ask which way round the theme is without re-deriving it.
        root.setAttribute('data-theme-light', light ? 'true' : 'false');
    }

    // Move a colour toward white (positive) or black (negative).
    function shift(hex, amount) {
        const parts = rgb(hex).split(',').map(v => parseInt(v, 10));
        const target = amount >= 0 ? 255 : 0;
        const k = Math.abs(amount);
        const out = parts.map(v => Math.round(v + (target - v) * k));
        return '#' + out.map(v => Math.max(0, Math.min(255, v)).toString(16).padStart(2, '0')).join('');
    }

    function use(id, persist) {
        const themes = all();
        const theme = themes[id] || themes.midnight;
        apply(theme.tokens);
        if (persist !== false) {
            try { localStorage.setItem(STORE_KEY, theme.id); } catch (err) { /* private mode */ }
        }
        document.dispatchEvent(new CustomEvent('theme:changed', { detail: theme.id }));
        return theme.id;
    }

    /* -------------------------------------------------------------- editing */

    function saveCustom(id, name, tokens, note) {
        const custom = readCustom();
        custom[id] = { name: name, note: note || 'Your own', tokens: tokens };
        writeCustom(custom);
        return id;
    }

    function removeCustom(id) {
        const custom = readCustom();
        delete custom[id];
        writeCustom(custom);
        if (current() === id) use('midnight');
    }

    // A slug that is not already taken, so duplicating twice does not
    // overwrite the first copy.
    function freeID(base) {
        const taken = all();
        let slug = String(base || 'theme').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
        if (!slug) slug = 'theme';
        if (!taken[slug]) return slug;
        for (let i = 2; i < 500; i++) {
            if (!taken[slug + '-' + i]) return slug + '-' + i;
        }
        return slug + '-' + Date.now();
    }

    window.Themes = {
        tokens: TOKENS,
        list: () => Object.values(all()),
        get: (id) => all()[id] || null,
        current: current,
        use: use,
        // Paint a set of values without saving them, for a live preview.
        preview: apply,
        // Put back whatever is actually selected, for cancelling one.
        restore: () => use(current(), false),
        save: saveCustom,
        remove: removeCustom,
        freeID: freeID,
        isLight: isLight
    };

    // Before first paint where possible, so the window does not flash the
    // default palette on the way to the chosen one.
    use(current(), false);
})();
