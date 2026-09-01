/**
 * Shared editor plumbing: which language a filename is, the languages Monaco
 * does not ship, and one factory that hands back an editor with the same
 * interface whether Monaco loaded or not.
 *
 * Monaco's bundled language set is large but not complete. TOML is missing
 * entirely — Velocity's velocity.toml opened as plain text — and there is no
 * definition for Java property files, which on a Minecraft host is the single
 * most-edited file there is. Both are defined here in Monarch.
 *
 * The factory exists because the split view's panes were textareas: the main
 * editor had highlighting and the panes did not, for no reason other than that
 * they were built at different times.
 */
(function () {
    'use strict';

    const LANGUAGE_MAP = {
        js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
        ts: 'typescript', tsx: 'typescript',
        py: 'python', rb: 'ruby', go: 'go', rs: 'rust',
        java: 'java', kt: 'kotlin', kts: 'kotlin', scala: 'scala', groovy: 'java',
        cpp: 'cpp', cc: 'cpp', cxx: 'cpp', h: 'cpp', hpp: 'cpp', c: 'c', cs: 'csharp',
        php: 'php', swift: 'swift', lua: 'lua', pl: 'perl', r: 'r', dart: 'dart',
        html: 'html', htm: 'html', xhtml: 'html',
        css: 'css', scss: 'scss', less: 'less',
        json: 'json', json5: 'json', jsonc: 'json', map: 'json',
        xml: 'xml', xsd: 'xml', xsl: 'xml', svg: 'xml', plist: 'xml',
        yaml: 'yaml', yml: 'yaml',
        toml: 'toml',
        ini: 'ini', cfg: 'ini', conf: 'ini',
        properties: 'properties', props: 'properties', env: 'properties',
        md: 'markdown', markdown: 'markdown', mdx: 'markdown',
        sh: 'shell', bash: 'shell', zsh: 'shell', command: 'shell',
        bat: 'bat', cmd: 'bat', ps1: 'powershell',
        sql: 'sql',
        dockerfile: 'dockerfile',
        graphql: 'graphql', gql: 'graphql',
        hcl: 'hcl', tf: 'hcl',
        log: 'log', txt: 'plaintext'
    };

    // Filenames with no useful extension.
    const BY_NAME = {
        dockerfile: 'dockerfile',
        makefile: 'plaintext',
        'server.properties': 'properties',
        'gradle.properties': 'properties',
        '.env': 'properties',
        'eula.txt': 'properties'
    };

    function languageFor(filename) {
        const name = String(filename || '').toLowerCase();
        if (BY_NAME[name]) return BY_NAME[name];

        // A trailing ".log.1" or ".yml.bak" is still that kind of file.
        const parts = name.split('.');
        for (let i = parts.length - 1; i > 0; i--) {
            const hit = LANGUAGE_MAP[parts[i]];
            if (hit) return hit;
        }
        return 'plaintext';
    }

    /* --------------------------------------------------------------- TOML */

    // https://toml.io/en/v1.0.0 — enough of it to colour a real config: tables
    // and array-of-tables headers, dotted and quoted keys, every string form
    // including the triple-quoted ones, numbers with underscores, the date and
    // time literals, and the bare keywords.
    const TOML_LANGUAGE = {
        defaultToken: '',
        tokenPostfix: '.toml',

        keywords: ['true', 'false', 'inf', 'nan'],

        // Ordered: the multi-line delimiters have to be tried before the
        // single-line ones, or """ opens as an empty "" followed by a string.
        tokenizer: {
            root: [
                [/^\s*#.*$/, 'comment'],

                // [[array of tables]] and [table.sub."quoted"]
                [/^\s*(\[\[)([^\]]*)(\]\])/, ['delimiter.bracket', 'type', 'delimiter.bracket']],
                [/^\s*(\[)([^\]]*)(\])/, ['delimiter.bracket', 'type', 'delimiter.bracket']],

                // key = , "quoted key" = , dotted.key =
                [/([A-Za-z0-9_\-.]+)(\s*)(=)/, ['property', '', 'operator']],
                [/("(?:[^"\\]|\\.)*")(\s*)(=)/, ['property', '', 'operator']],
                [/('[^']*')(\s*)(=)/, ['property', '', 'operator']],

                { include: '@value' }
            ],

            value: [
                [/#.*$/, 'comment'],

                [/"""/, 'string', '@mlbasic'],
                [/'''/, 'string', '@mlliteral'],
                [/"/, 'string', '@basic'],
                [/'/, 'string', '@literal'],

                // Offset date-time, local date-time, local date, local time.
                [/\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})?/, 'number'],
                [/\d{4}-\d{2}-\d{2}/, 'number'],
                [/\d{2}:\d{2}:\d{2}(\.\d+)?/, 'number'],

                [/[+-]?0x[0-9A-Fa-f_]+/, 'number'],
                [/[+-]?0o[0-7_]+/, 'number'],
                [/[+-]?0b[01_]+/, 'number'],
                [/[+-]?\d[\d_]*\.[\d_]+([eE][+-]?\d[\d_]*)?/, 'number'],
                [/[+-]?\d[\d_]*[eE][+-]?\d[\d_]*/, 'number'],
                [/[+-]?\d[\d_]*/, 'number'],

                [/\b(?:true|false|inf|nan)\b/, 'keyword'],

                [/[{}[\]]/, 'delimiter.bracket'],
                [/[,=]/, 'operator'],
                [/\./, 'delimiter']
            ],

            basic: [
                [/[^\\"]+/, 'string'],
                [/\\(?:[btnfr"\\]|u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{8})/, 'string.escape'],
                [/\\./, 'string.escape.invalid'],
                [/"/, 'string', '@pop']
            ],

            mlbasic: [
                [/[^\\"]+/, 'string'],
                [/\\(?:[btnfr"\\]|u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{8}|\s*$)/, 'string.escape'],
                [/"""/, 'string', '@pop'],
                [/"/, 'string']
            ],

            literal: [
                [/[^']+/, 'string'],
                [/'/, 'string', '@pop']
            ],

            mlliteral: [
                [/[^']+/, 'string'],
                [/'''/, 'string', '@pop'],
                [/'/, 'string']
            ]
        }
    };

    const TOML_CONFIG = {
        comments: { lineComment: '#' },
        brackets: [['[', ']'], ['{', '}']],
        autoClosingPairs: [
            { open: '[', close: ']' },
            { open: '{', close: '}' },
            { open: '"', close: '"', notIn: ['string'] },
            { open: "'", close: "'", notIn: ['string'] }
        ],
        surroundingPairs: [
            { open: '[', close: ']' },
            { open: '{', close: '}' },
            { open: '"', close: '"' },
            { open: "'", close: "'" }
        ]
    };

    /* ---------------------------------------------------- .properties ---- */

    // Java property files: no sections, '#' and '!' comments, and '=' or ':'
    // separating a key from a value that runs to end of line. Monaco's ini
    // definition is close but treats ';' as a comment and expects [sections],
    // which server.properties does not have.
    const PROPERTIES_LANGUAGE = {
        defaultToken: '',
        tokenPostfix: '.properties',

        tokenizer: {
            root: [
                [/^\s*[#!].*$/, 'comment'],
                [/^\s*([^=:#!\s][^=:]*?)(\s*)([=:])/, ['property', '', 'operator']],
                [/\\$/, 'string.escape'],
                [/\\./, 'string.escape'],
                [/\btrue\b|\bfalse\b/, 'keyword'],
                [/\b\d+(\.\d+)?\b/, 'number'],
                [/.+$/, 'string']
            ]
        }
    };

    const PROPERTIES_CONFIG = {
        comments: { lineComment: '#' }
    };

    /* ------------------------------------------------------- registration */

    let ready = false;

    function has(id) {
        return monaco.languages.getLanguages().some((l) => l.id === id);
    }

    /**
     * Called once Monaco has loaded. Registers only what is missing, so a later
     * Monaco that does ship TOML wins over this definition rather than fighting
     * it.
     */
    function registerLanguages() {
        if (typeof monaco === 'undefined') return;

        if (!has('toml')) {
            monaco.languages.register({ id: 'toml', extensions: ['.toml'], aliases: ['TOML', 'toml'] });
            monaco.languages.setMonarchTokensProvider('toml', TOML_LANGUAGE);
            monaco.languages.setLanguageConfiguration('toml', TOML_CONFIG);
        }

        if (!has('properties')) {
            monaco.languages.register({
                id: 'properties',
                extensions: ['.properties', '.env'],
                filenames: ['server.properties', '.env'],
                aliases: ['Properties', 'properties']
            });
            monaco.languages.setMonarchTokensProvider('properties', PROPERTIES_LANGUAGE);
            monaco.languages.setLanguageConfiguration('properties', PROPERTIES_CONFIG);
        }

        if (!has('log')) {
            monaco.languages.register({ id: 'log', extensions: ['.log'] });
            monaco.languages.setMonarchTokensProvider('log', {
                defaultToken: '',
                tokenizer: {
                    root: [
                        [/\[\d{2}:\d{2}:\d{2}\]/, 'number'],
                        [/\b(?:ERROR|SEVERE|FATAL)\b/, 'invalid'],
                        [/\b(?:WARN|WARNING)\b/, 'keyword'],
                        [/\b(?:INFO|DEBUG|TRACE)\b/, 'type'],
                        [/\[[^\]]*\]/, 'comment']
                    ]
                }
            });
        }

        ready = true;
        document.dispatchEvent(new CustomEvent('monaco:ready'));
    }

    /* ---------------------------------------------------- editor factory */

    const BASE_OPTIONS = {
        theme: 'pterodactyl-dark',
        fontSize: 13,
        minimap: { enabled: false },
        wordWrap: 'off',
        lineNumbers: 'on',
        automaticLayout: true,
        scrollBeyondLastLine: false,
        renderWhitespace: 'selection',
        bracketPairColorization: { enabled: true },
        guides: { indentation: true, bracketPairs: true },
        tabSize: 4,
        insertSpaces: true,
        scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 }
    };

    /**
     * Builds an editor inside `host` and returns a handle with the same shape
     * whether it is Monaco or the textarea fallback, so callers do not branch.
     */
    function createEditor(host, opts) {
        const o = opts || {};
        host.innerHTML = '';

        if (!ready || typeof monaco === 'undefined') {
            const area = document.createElement('textarea');
            area.className = 'editor-textarea mono';
            area.spellcheck = false;
            if (o.placeholder) area.placeholder = o.placeholder;
            area.value = o.value || '';
            host.appendChild(area);

            if (o.onChange) area.addEventListener('input', () => o.onChange(area.value));

            return {
                isMonaco: false,
                el: area,
                getValue: () => area.value,
                setValue: (v) => { area.value = v; },
                setLanguage: () => {},
                setModel: (_key, value) => { area.value = value; },
                dropModel: () => {},
                focus: () => area.focus(),
                layout: () => {},
                dispose: () => { area.remove(); }
            };
        }

        const editor = monaco.editor.create(host, Object.assign({}, BASE_OPTIONS, {
            value: o.value || '',
            language: o.language || 'plaintext'
        }, o.options || {}));

        // One model per file, so switching tabs keeps each file's undo history
        // and cursor rather than resetting them.
        const models = new Map();
        let suppress = false;

        if (o.onChange) {
            editor.onDidChangeModelContent(() => {
                if (suppress) return;
                o.onChange(editor.getValue());
            });
        }

        return {
            isMonaco: true,
            editor: editor,
            getValue: () => editor.getValue(),
            setValue: (v) => {
                suppress = true;
                editor.setValue(v);
                suppress = false;
            },
            setLanguage: (lang) => {
                const model = editor.getModel();
                if (model) monaco.editor.setModelLanguage(model, lang || 'plaintext');
            },
            /** Attach the model for `key`, creating it from `value` if new. */
            setModel: (key, value, language) => {
                let model = models.get(key);
                if (!model) {
                    model = monaco.editor.createModel(value, language || 'plaintext');
                    models.set(key, model);
                } else if (value !== undefined && model.getValue() !== value) {
                    suppress = true;
                    model.setValue(value);
                    suppress = false;
                }
                editor.setModel(model);
            },
            dropModel: (key) => {
                const model = models.get(key);
                if (!model) return;
                models.delete(key);
                model.dispose();
            },
            focus: () => editor.focus(),
            layout: () => editor.layout(),
            dispose: () => {
                models.forEach((m) => m.dispose());
                models.clear();
                editor.dispose();
            }
        };
    }

    window.MonacoKit = {
        LANGUAGE_MAP: LANGUAGE_MAP,
        languageFor: languageFor,
        registerLanguages: registerLanguages,
        createEditor: createEditor,
        isReady: () => ready
    };
})();
