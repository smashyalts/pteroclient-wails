/**
 * One SVG sprite for the whole app, replacing the emoji that used to stand in
 * for file types and toolbar actions. Every glyph is drawn on a 24px grid with
 * a 1.5 stroke and inherits `currentColor`, so a single icon recolours per
 * context instead of needing a different emoji.
 */
(function () {
    'use strict';

    const PATHS = {
        folder: '<path d="M3.5 6.4a2 2 0 0 1 2-2h3.1l2 2.2h7.9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/>',
        file: '<path d="M6.5 3.5h7L18 8v11.5a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 19.5V5a1.5 1.5 0 0 1 1.5-1.5z"/><path d="M13.5 3.6V8H18"/>',
        code: '<path d="M9 4.5H7.5a2 2 0 0 0-2 2v3.2L3.5 12l2 2.3v3.2a2 2 0 0 0 2 2H9"/><path d="M15 4.5h1.5a2 2 0 0 1 2 2v3.2l2 2.3-2 2.3v3.2a2 2 0 0 1-2 2H15"/>',
        archive: '<path d="M3.6 6.5h16.8v3.4H3.6z"/><path d="M5.4 9.9v9.6h13.2V9.9"/><path d="M10.2 13.4h3.6"/>',
        log: '<path d="M6 3.6h12v16.8H6z"/><path d="M9 8.2h6M9 12h6M9 15.8h4"/>',
        image: '<path d="M4 5h16v14H4z"/><circle cx="9" cy="10" r="1.6"/><path d="M5 17.5 10 12l3.5 3.5L16 13l3 4"/>',
        terminal: '<path d="M3.8 4.8h16.4v14.4H3.8z"/><path d="M7.6 10 10.3 12 7.6 14"/><path d="M12.8 14.8h3.6"/>',
        database: '<ellipse cx="12" cy="6.4" rx="7.2" ry="2.9"/><path d="M4.8 6.4v11.2c0 1.6 3.2 2.9 7.2 2.9s7.2-1.3 7.2-2.9V6.4"/><path d="M4.8 12c0 1.6 3.2 2.9 7.2 2.9s7.2-1.3 7.2-2.9"/>',
        backup: '<path d="M20 12a8 8 0 1 1-2.3-5.6"/><path d="M20 4v5h-5"/><path d="M12 8.4V12l2.6 2.6"/>',
        clock: '<circle cx="12" cy="12" r="8"/><path d="M12 7.4V12l3 2.2"/>',
        users: '<circle cx="9.4" cy="8.6" r="3.4"/><path d="M3.6 19.4a5.8 5.8 0 0 1 11.6 0"/><path d="M16 5.6a3.4 3.4 0 0 1 0 6.6M17.4 14.6a5.8 5.8 0 0 1 3 4.8"/>',
        network: '<circle cx="12" cy="12" r="8"/><path d="M4 12h16"/><path d="M12 4a12 12 0 0 1 0 16 12 12 0 0 1 0-16z"/>',
        sliders: '<path d="M4 8.4h5.2M13.4 8.4H20M4 15.6h9.2M17.4 15.6H20"/><circle cx="11.3" cy="8.4" r="2.1"/><circle cx="15.3" cy="15.6" r="2.1"/>',
        cog: '<circle cx="12" cy="12" r="3"/><path d="M19.2 14.4a1.6 1.6 0 0 0 .32 1.76l.06.06a1.9 1.9 0 1 1-2.7 2.7l-.06-.06a1.6 1.6 0 0 0-1.76-.32 1.6 1.6 0 0 0-.97 1.46V20a1.9 1.9 0 1 1-3.8 0v-.1a1.6 1.6 0 0 0-1.04-1.46 1.6 1.6 0 0 0-1.76.32l-.06.06a1.9 1.9 0 1 1-2.7-2.7l.06-.06a1.6 1.6 0 0 0 .32-1.76 1.6 1.6 0 0 0-1.46-.97H4a1.9 1.9 0 1 1 0-3.8h.1a1.6 1.6 0 0 0 1.46-1.04 1.6 1.6 0 0 0-.32-1.76l-.06-.06a1.9 1.9 0 1 1 2.7-2.7l.06.06a1.6 1.6 0 0 0 1.76.32H9.8a1.6 1.6 0 0 0 .97-1.46V4a1.9 1.9 0 1 1 3.8 0v.1a1.6 1.6 0 0 0 .97 1.46 1.6 1.6 0 0 0 1.76-.32l.06-.06a1.9 1.9 0 1 1 2.7 2.7l-.06.06a1.6 1.6 0 0 0-.32 1.76v.08a1.6 1.6 0 0 0 1.46.97H20a1.9 1.9 0 1 1 0 3.8h-.1a1.6 1.6 0 0 0-1.46.97z"/>',
        activity: '<path d="M3.6 12.4h4.1l2.5-6.6 3.4 12.4 2.4-5.8h4.4"/>',
        refresh: '<path d="M19.9 12.6a8 8 0 1 1-2.2-6.1"/><path d="M20 4.2v4.9h-4.9"/>',
        upload: '<path d="M12 15.5V4"/><path d="M8.1 7.9 12 4l3.9 3.9"/><path d="M4.4 15.6v3.1a1.3 1.3 0 0 0 1.3 1.3h12.6a1.3 1.3 0 0 0 1.3-1.3v-3.1"/>',
        download: '<path d="M12 4v11.5"/><path d="M8.1 11.6 12 15.5l3.9-3.9"/><path d="M4.4 15.6v3.1a1.3 1.3 0 0 0 1.3 1.3h12.6a1.3 1.3 0 0 0 1.3-1.3v-3.1"/>',
        folderPlus: '<path d="M3.5 6.4a2 2 0 0 1 2-2h3.1l2 2.2h7.9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/><path d="M12 10.6v5.2M9.4 13.2h5.2"/>',
        filePlus: '<path d="M6.5 3.5h7L18 8v11.5a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 19.5V5a1.5 1.5 0 0 1 1.5-1.5z"/><path d="M12 11v6M9 14h6"/>',
        trash: '<path d="M4.2 6.9h15.6"/><path d="M9.3 6.9V4.4h5.4v2.5"/><path d="M6.6 6.9 7.6 20h8.8l1-13.1"/><path d="M10.4 10.6v5.7M13.6 10.6v5.7"/>',
        split: '<path d="M4 4.8h16v14.4H4z"/><path d="M12 4.8v14.4"/>',
        save: '<path d="M5 4.8h11.2L19 7.6v11.6H5z"/><path d="M8.2 4.8v4.6h7V4.8"/><path d="M8.2 19.2v-4.9h7.6v4.9"/>',
        x: '<path d="M6.4 6.4 17.6 17.6M17.6 6.4 6.4 17.6"/>',
        chevron: '<path d="M9.6 5.6 16 12l-6.4 6.4"/>',
        play: '<path d="M9 6.4 17.6 12 9 17.6z"/>',
        stop: '<path d="M7.4 7.4h9.2v9.2H7.4z"/>',
        restart: '<path d="M4.1 11.4a8 8 0 1 0 2.2-6.1"/><path d="M4 4.2v4.9h4.9"/>',
        bolt: '<path d="M13.2 3.2 5.6 13.4h5.4l-1 7.4 7.6-10.2h-5.4z"/>',
        plus: '<path d="M12 5.6v12.8M5.6 12h12.8"/>',
        search: '<circle cx="10.8" cy="10.8" r="6.4"/><path d="M15.6 15.6 20 20"/>',
        server: '<path d="M4 4.8h16v5.1H4zM4 14.1h16v5.1H4z"/><path d="M7.2 7.35h.01M7.2 16.65h.01"/>',
        check: '<path d="M5.2 12.4 9.6 16.8 18.8 7.6"/>',
        plug: '<path d="M8.4 3.6v4.2M15.6 3.6v4.2"/><path d="M6 7.8h12v3.4a6 6 0 0 1-12 0z"/><path d="M12 17.2v3.2"/>',
        key: '<circle cx="8.4" cy="12" r="4.2"/><path d="M12.6 12H20M17.2 12v3.2M20 12v2.4"/>',
        lock: '<path d="M6.4 10.6h11.2v9.4H6.4z"/><path d="M8.8 10.6V7.8a3.2 3.2 0 0 1 6.4 0v2.8"/>',
        unlock: '<path d="M6.4 10.6h11.2v9.4H6.4z"/><path d="M8.8 10.6V7.8a3.2 3.2 0 0 1 6.1-1.3"/>',
        warning: '<path d="M12 4.2 21 19.4H3z"/><path d="M12 10v4.2M12 17h.01"/>',
        external: '<path d="M14 4.6h5.4V10"/><path d="M19.4 4.6 11 13"/><path d="M18 14.4v4.2a1.4 1.4 0 0 1-1.4 1.4H5.4A1.4 1.4 0 0 1 4 18.6V7.4A1.4 1.4 0 0 1 5.4 6h4.2"/>',
        shield: '<path d="M12 3.4 19.4 6v6.1c0 4-3 7.2-7.4 8.5-4.4-1.3-7.4-4.5-7.4-8.5V6z"/><path d="M8.9 12.1 11.2 14.4l4-4.4"/>',
        history: '<path d="M4.1 11.4a8 8 0 1 0 2.2-6.1"/><path d="M4 4.2v4.9h4.9"/><path d="M12 7.6V12l3.1 1.9"/>',
        undo: '<path d="M4 9.4h9.6a5.2 5.2 0 0 1 0 10.4H8.4"/><path d="M7.6 5.2 3.4 9.4l4.2 4.2"/>'
    };

    // Extensions the file tree tints and icons by kind.
    const KINDS = {
        config: ['yml', 'yaml', 'json', 'properties', 'conf', 'cfg', 'toml', 'ini', 'env', 'sh', 'bash', 'js', 'ts', 'py', 'php', 'rb', 'go', 'java', 'html', 'css', 'xml'],
        archive: ['jar', 'zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'rar', '7z', 'dat', 'mca', 'mcr'],
        log: ['log', 'txt', 'md'],
        image: ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico']
    };

    const KIND_ICON = {
        dir: 'folder',
        config: 'code',
        archive: 'archive',
        log: 'log',
        image: 'image',
        file: 'file'
    };

    const Icons = {
        /** Markup for one icon. `cls` is appended to the base `ic` class. */
        svg(name, cls) {
            if (!PATHS[name]) name = 'file';
            return '<svg class="ic ' + (cls || '') + '" viewBox="0 0 24 24" aria-hidden="true">' + PATHS[name] + '</svg>';
        },

        /** Which kind bucket a filename falls into — drives icon and tint. */
        kindFor(filename, isDir) {
            if (isDir) return 'dir';
            const ext = String(filename || '').split('.').pop().toLowerCase();
            for (const kind of Object.keys(KINDS)) {
                if (KINDS[kind].indexOf(ext) !== -1) return kind;
            }
            return 'file';
        },

        /** Icon markup for a file tree row, tinted by kind. */
        forFile(filename, isDir) {
            const kind = Icons.kindFor(filename, isDir);
            return Icons.svg(KIND_ICON[kind], 'kind-' + kind);
        },

        kindIcon(kind) {
            return KIND_ICON[kind] || 'file';
        },

        names() {
            return Object.keys(PATHS);
        }
    };

    window.Icons = Icons;
})();
