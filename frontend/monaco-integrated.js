// Monaco Editor Integration for Pterodactyl Manager
console.log('Monaco Editor Integration loading...');

// Languages and the filename -> language rules live in monaco-kit.js, so the
// split view's editors resolve them the same way this one does.
const languageFor = (name) => (window.MonacoKit ? window.MonacoKit.languageFor(name) : 'plaintext');

// Initialize Monaco
// The Monaco loader is fetched from a CDN. When that fetch fails — no
// network, a blocked host — `require` never appears and the app falls back
// to the built-in simple editor instead of throwing on every load.
if (typeof require === 'undefined') {
    console.warn('Monaco loader unavailable; using the simple editor.');
} else {
    require.config({
        paths: {
            'vs': 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs'
        }
    });
}

// Wait for dependencies. Bounded, because when the Monaco CDN is unreachable
// `require` never turns up and this used to poll for the life of the process.
let dependencyAttempts = 0;
const MAX_DEPENDENCY_ATTEMPTS = 200; // ~10s at 50ms

function waitForDependencies() {
    if (typeof window.go !== 'undefined' &&
        typeof window.runtime !== 'undefined' &&
        typeof require !== 'undefined') {
        console.log('Dependencies ready, loading Monaco...');
        loadMonacoEditor();
        return;
    }

    if (++dependencyAttempts > MAX_DEPENDENCY_ATTEMPTS) {
        console.warn('Monaco did not become available; staying on the simple editor.');
        return;
    }
    setTimeout(waitForDependencies, 50);
}

function loadMonacoEditor() {
    require(['vs/editor/editor.main'], function() {
        console.log('Monaco loaded, defining themes...');
        defineCustomThemes();
        // Before anything creates a model: Monaco ships no TOML and no
        // .properties definition, and a model made with an unregistered
        // language id stays plain text even after the language turns up.
        if (window.MonacoKit) window.MonacoKit.registerLanguages();
        enhanceApp();
    });
}

// Define vibrant custom themes
function defineCustomThemes() {
    // Dracula-inspired theme with vibrant colors
    monaco.editor.defineTheme('pterodactyl-dark', {
        base: 'vs-dark',
        inherit: false,
        rules: [
            // Default text
            { token: '', foreground: 'F8F8F2' },
            
            // Comments - Green
            { token: 'comment', foreground: '6272A4', fontStyle: 'italic' },
            { token: 'comment.js', foreground: '6272A4', fontStyle: 'italic' },
            { token: 'comment.line', foreground: '6272A4', fontStyle: 'italic' },
            { token: 'comment.block', foreground: '6272A4', fontStyle: 'italic' },
            
            // Strings - Yellow/Green
            { token: 'string', foreground: 'F1FA8C' },
            { token: 'string.js', foreground: 'F1FA8C' },
            { token: 'string.sql', foreground: 'F1FA8C' },
            { token: 'string.yaml', foreground: 'F1FA8C' },
            { token: 'string.value.json', foreground: 'F1FA8C' },
            { token: 'string.quoted', foreground: 'F1FA8C' },
            { token: 'string.template', foreground: 'F1FA8C' },
            { token: 'string.regexp', foreground: 'FF79C6' },
            
            // Numbers - Purple
            { token: 'number', foreground: 'BD93F9' },
            { token: 'number.js', foreground: 'BD93F9' },
            { token: 'number.float', foreground: 'BD93F9' },
            { token: 'number.hex', foreground: 'BD93F9' },
            { token: 'constant.numeric', foreground: 'BD93F9' },
            
            // Keywords - Pink
            { token: 'keyword', foreground: 'FF79C6' },
            { token: 'keyword.js', foreground: 'FF79C6' },
            { token: 'keyword.flow', foreground: 'FF79C6' },
            { token: 'keyword.json', foreground: 'FF79C6' },
            { token: 'storage', foreground: 'FF79C6' },
            { token: 'storage.type', foreground: 'FF79C6' },
            { token: 'keyword.control', foreground: 'FF79C6' },
            { token: 'constant.language', foreground: 'BD93F9' },
            { token: 'constant.language.boolean', foreground: 'BD93F9' },
            { token: 'constant.language.null', foreground: 'BD93F9' },
            { token: 'constant.language.undefined', foreground: 'BD93F9' },
            
            // Functions & Methods - Green
            { token: 'entity.name.function', foreground: '50FA7B' },
            { token: 'support.function', foreground: '50FA7B' },
            { token: 'entity.name.method', foreground: '50FA7B' },
            
            // Variables & Properties - White/Cyan
            { token: 'variable', foreground: 'F8F8F2' },
            { token: 'variable.js', foreground: 'F8F8F2' },
            { token: 'variable.other', foreground: 'F8F8F2' },
            { token: 'variable.parameter', foreground: 'FFB86C' },
            { token: 'property', foreground: '8BE9FD' },
            { token: 'meta.property-name', foreground: '8BE9FD' },
            { token: 'support.property-value', foreground: 'F8F8F2' },
            
            // Types & Classes - Cyan/Purple
            { token: 'entity.name.type', foreground: '8BE9FD' },
            { token: 'entity.name.class', foreground: '8BE9FD' },
            { token: 'support.type', foreground: '8BE9FD' },
            { token: 'support.class', foreground: '8BE9FD' },
            { token: 'type', foreground: '8BE9FD' },
            
            // Tags (HTML/XML) - Pink
            { token: 'entity.name.tag', foreground: 'FF79C6' },
            { token: 'tag', foreground: 'FF79C6' },
            { token: 'meta.tag', foreground: 'FF79C6' },
            
            // Attributes - Green
            { token: 'entity.other.attribute-name', foreground: '50FA7B' },
            { token: 'attribute.name', foreground: '50FA7B' },
            { token: 'attribute.value', foreground: 'F1FA8C' },
            
            // Operators - Pink
            { token: 'keyword.operator', foreground: 'FF79C6' },
            { token: 'operator', foreground: 'FF79C6' },
            { token: 'punctuation.definition', foreground: 'F8F8F2' },
            { token: 'meta.brace', foreground: 'F8F8F2' },
            
            // JSON specific
            { token: 'string.key.json', foreground: '8BE9FD' },
            { token: 'support.type.property-name.json', foreground: '8BE9FD' },
            
            // YAML specific
            { token: 'entity.name.tag.yaml', foreground: 'FF79C6' },
            
            // CSS specific
            { token: 'entity.name.selector', foreground: '50FA7B' },
            { token: 'support.property-name.css', foreground: '8BE9FD' },
            { token: 'constant.numeric.css', foreground: 'BD93F9' },
            { token: 'keyword.other.unit', foreground: 'FF79C6' },
            
            // Markdown
            { token: 'markup.heading', foreground: 'BD93F9', fontStyle: 'bold' },
            { token: 'markup.bold', fontStyle: 'bold' },
            { token: 'markup.italic', fontStyle: 'italic' },
            { token: 'markup.underline', fontStyle: 'underline' },
            { token: 'markup.strikethrough', fontStyle: 'strikethrough' },
            { token: 'markup.list', foreground: 'FF79C6' },
            { token: 'markup.quote', foreground: '6272A4', fontStyle: 'italic' },
            { token: 'markup.raw', foreground: 'F1FA8C' },
            { token: 'markup.link', foreground: '8BE9FD', fontStyle: 'underline' },
            
            // Regex
            { token: 'constant.character.escape', foreground: 'FF79C6' },
            
            // TOML and .properties: table headers, keys, escapes
            { token: 'type.toml', foreground: '8BE9FD', fontStyle: 'bold' },
            { token: 'property.toml', foreground: '50FA7B' },
            { token: 'property.properties', foreground: '50FA7B' },
            { token: 'string.escape', foreground: 'FF79C6' },
            { token: 'string.escape.invalid', foreground: 'FF5555' },
            { token: 'invalid', foreground: 'FF5555', fontStyle: 'bold' },
            { token: 'operator.toml', foreground: 'FF79C6' },
            { token: 'operator.properties', foreground: 'FF79C6' },

            // Delimiters and Brackets
            { token: 'delimiter', foreground: 'F8F8F2' },
            { token: 'delimiter.bracket', foreground: 'F8F8F2' },
            { token: 'delimiter.array', foreground: 'F8F8F2' },
            { token: 'delimiter.parenthesis', foreground: 'F8F8F2' },
            { token: 'delimiter.square', foreground: 'F8F8F2' },
            { token: 'bracket', foreground: 'F8F8F2' }
        ],
        colors: {
            'editor.background': '#282A36',
            'editor.foreground': '#F8F8F2',
            'editorLineNumber.foreground': '#6272A4',
            'editorLineNumber.activeForeground': '#F8F8F2',
            'editor.selectionBackground': '#44475A',
            'editor.inactiveSelectionBackground': '#44475A',
            'editor.lineHighlightBackground': '#44475A',
            'editor.lineHighlightBorder': '#44475A',
            'editorCursor.foreground': '#F8F8F0',
            'editorWhitespace.foreground': '#3B3A32',
            'editorIndentGuide.background': '#3B3A32',
            'editorIndentGuide.activeBackground': '#6272A4',
            'editor.findMatchBackground': '#FFB86C',
            'editor.findMatchHighlightBackground': '#FFB86C55',
            'editor.selectionHighlightBackground': '#FFB86C55',
            'editor.wordHighlightBackground': '#8BE9FD50',
            'editor.wordHighlightStrongBackground': '#50FA7B50',
            'editorBracketMatch.background': '#44475A',
            'editorBracketMatch.border': '#F8F8F2',
            'editorGutter.background': '#282A36',
            'editorGutter.modifiedBackground': '#FFB86C',
            'editorGutter.addedBackground': '#50FA7B',
            'editorGutter.deletedBackground': '#FF5555',
            'diffEditor.insertedTextBackground': '#50FA7B33',
            'diffEditor.removedTextBackground': '#FF555533',
            'scrollbar.shadow': '#191A21',
            'scrollbarSlider.background': '#44475Aaa',
            'scrollbarSlider.activeBackground': '#44475Acc',
            'scrollbarSlider.hoverBackground': '#44475Aee'
        }
    });
    
    
    // Also try Monokai theme for even more vibrant colors
    monaco.editor.defineTheme('monokai-vibrant', {
        base: 'vs-dark',
        inherit: false,
        rules: [
            { token: '', foreground: 'F8F8F2' },
            { token: 'comment', foreground: '88846F' },
            { token: 'string', foreground: 'E6DB74' },
            { token: 'constant.numeric', foreground: 'AE81FF' },
            { token: 'constant.language', foreground: 'AE81FF' },
            { token: 'keyword', foreground: 'F92672' },
            { token: 'storage', foreground: 'F92672' },
            { token: 'storage.type', foreground: '66D9EF', fontStyle: 'italic' },
            { token: 'entity.name.class', foreground: 'A6E22E' },
            { token: 'entity.other.inherited-class', foreground: 'A6E22E', fontStyle: 'italic' },
            { token: 'entity.name.function', foreground: 'A6E22E' },
            { token: 'entity.name.tag', foreground: 'F92672' },
            { token: 'entity.other.attribute-name', foreground: 'A6E22E' },
            { token: 'variable', foreground: 'F8F8F2' },
            { token: 'variable.parameter', foreground: 'FD971F', fontStyle: 'italic' },
            { token: 'support.function', foreground: '66D9EF' },
            { token: 'support.constant', foreground: '66D9EF' },
            { token: 'support.type', foreground: '66D9EF' },
            { token: 'support.class', foreground: '66D9EF' }
        ],
        colors: {
            'editor.background': '#272822',
            'editor.foreground': '#F8F8F2',
            'editor.selectionBackground': '#49483E',
            'editor.lineHighlightBackground': '#3E3D32',
            'editorCursor.foreground': '#F8F8F0',
            'editorWhitespace.foreground': '#3B3A32'
        }
    });
}

// Enhance the existing app with Monaco
function enhanceApp() {
    console.log('Enhancing app with Monaco Editor...');
    
    // Wait for app to be initialized
    const checkApp = setInterval(() => {
        if (window.app && window.app.editor) {
            clearInterval(checkApp);
            replaceEditor();
        }
    }, 100);
}

function replaceEditor() {
    const app = window.app;
    
    // Store reference to old editor
    const oldEditor = app.editor;
    
    // Create Monaco editor instance
    let monacoEditor = null;
    let monacoModels = new Map();
    let welcomeModel = null;
    
    // Replace the editor with Monaco
    app.editor = {
        textarea: true, // Fake property to pass checks
        
        create() {
            const container = document.getElementById('editor');
            if (!container || monacoEditor) return;
            
            // Clear the container
            container.innerHTML = '';
            
            // Create Monaco Editor
            monacoEditor = monaco.editor.create(container, {
                value: '// Welcome to Pterodactyl Manager\n// Select a file from the file tree to edit\n',
                language: 'javascript',
                theme: 'pterodactyl-dark',
                fontSize: 14,
                minimap: { enabled: true },
                wordWrap: 'off',
                lineNumbers: 'on',
                automaticLayout: true,
                scrollBeyondLastLine: false,
                renderWhitespace: 'selection',
                bracketPairColorization: { enabled: true },
                guides: {
                    indentation: true,
                    bracketPairs: true
                },
                formatOnPaste: true,
                tabSize: 4,
                insertSpaces: true
            });
            
            // Handle changes
            monacoEditor.onDidChangeModelContent(() => {
                if (this.onChange) {
                    this.onChange(monacoEditor.getValue());
                }
            });
            
            // Update cursor position
            monacoEditor.onDidChangeCursorPosition((e) => {
                const posEl = document.getElementById('cursorPosition');
                if (posEl) {
                    posEl.textContent = `Line ${e.position.lineNumber}, Col ${e.position.column}`;
                }
            });
            
            // No save bindings here on purpose. Monaco's addCommand fires on
            // its own keydown and the event still bubbles to document, so
            // registering Ctrl+S here as well as in the hotkey manager saved
            // the file twice — two writes and two entries in the history.
            // commands.js owns it; the manager lets it through inside Monaco
            // because that command is marked allowInField.
            
            console.log('Monaco Editor created successfully');
        },
        
        setValue(content) {
            if (monacoEditor) {
                const currentModel = monacoEditor.getModel();
                if (currentModel) {
                    currentModel.setValue(content);
                } else {
                    monacoEditor.setValue(content);
                }
            }
        },
        
        getValue() {
            return monacoEditor ? monacoEditor.getValue() : '';
        },
        
        focus() {
            if (monacoEditor) monacoEditor.focus();
        },
        
        setShowLineNumbers(show) {
            if (monacoEditor) {
                monacoEditor.updateOptions({
                    lineNumbers: show ? 'on' : 'off'
                });
            }
        },
        
        setWordWrap(wrap) {
            if (monacoEditor) {
                monacoEditor.updateOptions({
                    wordWrap: wrap ? 'on' : 'off'
                });
            }
        },
        
        onChange: null
    };
    
    // Keep the onChange handler
    if (oldEditor && oldEditor.onChange) {
        app.editor.onChange = oldEditor.onChange;
    }
    
    // Override openFile to detect language.
    //
    // The path comes from the entry the file tree rendered, not from
    // currentPath: by the time a tab is reopened the tree may be showing a
    // different directory, and recomputing here pointed the model at a file
    // with the same name somewhere else.
    const originalOpenFile = app.openFile;
    app.openFile = async function(file, opts) {
        // opts has to be forwarded: dropping it here is what made a middle
        // click — which asks for the file in a background tab — switch to it
        // anyway, and then set the model and the language for it.
        const result = await originalOpenFile.call(this, file, opts);
        const background = !!(opts && opts.background);

        if (monacoEditor && !background && file && !file.isDir) {
            const filePath = this.resolvePath(file);
            // originalOpenFile bails out for binary and oversized files, so
            // there is nothing open to point a model at.
            if (!filePath || !this.openFiles.has(filePath)) return result;

            const language = languageFor(file.name);

            let model = monacoModels.get(filePath);
            if (!model) {
                const content = this.openFiles.get(filePath).content || '';
                model = monaco.editor.createModel(content, language);
                monacoModels.set(filePath, model);
            }

            if (monacoEditor.getModel() !== model) monacoEditor.setModel(model);


            // Update file type display
            const typeEl = document.getElementById('fileType');
            if (typeEl) {
                const types = {
                    javascript: 'JavaScript', typescript: 'TypeScript', python: 'Python',
                    yaml: 'YAML', json: 'JSON', html: 'HTML', css: 'CSS',
                    markdown: 'Markdown', toml: 'TOML', properties: 'Properties',
                    ini: 'INI', shell: 'Shell', plaintext: 'Plain Text', log: 'Log'
                };
                typeEl.textContent = types[language] || language;
            }
        }

        if (monacoEditor && background && file && !file.isDir) {
            const filePath = this.resolvePath(file);
            if (filePath && this.openFiles.has(filePath) && !monacoModels.get(filePath)) {
                // Build the model but leave it off screen, so switching to the
                // tab later is instant and does not re-read the file.
                const content = this.openFiles.get(filePath).content || '';
                monacoModels.set(filePath, monaco.editor.createModel(content, languageFor(file.name)));
            }
        }

        return result;
    };
    
    // Override switchToFile to use models.
    //
    // The model is attached BEFORE the base implementation runs. The base ends
    // with editor.setValue(file.content), and setValue writes into whichever
    // model is currently attached — doing it the other way round wrote the
    // incoming file's text into the outgoing file's model.
    const originalSwitchToFile = app.switchToFile;
    app.switchToFile = function(path) {
        if (monacoEditor) {
            let model = monacoModels.get(path);
            const file = this.openFiles.get(path);

            if (file && !model) {
                model = monaco.editor.createModel(file.content, languageFor(file.name));
                monacoModels.set(path, model);
            }

            if (model) {
                monacoEditor.setModel(model);
            }
        }

        originalSwitchToFile.call(this, path);
    };
    
    // Override closeFileTab to dispose models
    const originalCloseFileTab = app.closeFileTab;
    app.closeFileTab = function(path) {
        // Dispose of the model
        const model = monacoModels.get(path);
        if (model) {
            model.dispose();
            monacoModels.delete(path);
        }
        
        originalCloseFileTab.call(this, path);

        // If no files are open, show welcome message. The placeholder model is
        // reused rather than recreated; the old code leaked one per close.
        if (this.openFiles.size === 0 && monacoEditor) {
            if (!welcomeModel) {
                welcomeModel = monaco.editor.createModel(
                    '// Welcome to Pterodactyl Manager\n// Select a file from the file tree to edit\n',
                    'javascript'
                );
            }
            monacoEditor.setModel(welcomeModel);
        }
    };
    
    // Add format document function
    app.formatDocument = function() {
        if (monacoEditor) {
            monacoEditor.getAction('editor.action.formatDocument').run();
        }
    };
    
    // Update onEditorChange to sync with models
    const originalOnEditorChange = app.onEditorChange;
    app.onEditorChange = function(content) {
        if (this.activeFile) {
            const model = monacoModels.get(this.activeFile);
            if (model && model.getValue() !== content) {
                model.setValue(content);
            }
        }
        if (originalOnEditorChange) {
            originalOnEditorChange.call(this, content);
        }
    };
    
    // Initialize the Monaco editor
    if (!monacoEditor) {
        app.editor.create();
    }
    
    // Show format button
    const formatBtn = document.getElementById('formatBtn');
    if (formatBtn) {
        formatBtn.style.display = '';
    }
    
    console.log('App enhanced with Monaco Editor successfully');
}

// Start initialization
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForDependencies);
} else {
    waitForDependencies();
}
