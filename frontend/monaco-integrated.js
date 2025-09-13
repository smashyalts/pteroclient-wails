// Monaco Editor Integration for Pterodactyl Manager
console.log('Monaco Editor Integration loading...');

// Language configurations
const LANGUAGE_MAP = {
    'js': 'javascript', 'jsx': 'javascript',
    'ts': 'typescript', 'tsx': 'typescript',
    'py': 'python', 'rb': 'ruby', 'go': 'go',
    'rs': 'rust', 'java': 'java', 'kt': 'kotlin',
    'cpp': 'cpp', 'c': 'c', 'cs': 'csharp',
    'php': 'php', 'swift': 'swift',
    'html': 'html', 'htm': 'html',
    'css': 'css', 'scss': 'scss', 'less': 'less',
    'json': 'json', 'xml': 'xml',
    'yaml': 'yaml', 'yml': 'yaml',
    'toml': 'toml', 'ini': 'ini',
    'conf': 'ini', 'cfg': 'ini',
    'md': 'markdown', 'txt': 'plaintext',
    'sh': 'shell', 'bash': 'shell',
    'dockerfile': 'dockerfile',
    'sql': 'sql', 'log': 'log'
};

// Initialize Monaco
require.config({
    paths: {
        'vs': 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs'
    }
});

// Wait for dependencies
function waitForDependencies() {
    if (typeof window.go !== 'undefined' && 
        typeof window.runtime !== 'undefined' && 
        typeof require !== 'undefined') {
        console.log('Dependencies ready, loading Monaco...');
        loadMonacoEditor();
    } else {
        console.log('Waiting for dependencies...');
        setTimeout(waitForDependencies, 50);
    }
}

function loadMonacoEditor() {
    require(['vs/editor/editor.main'], function() {
        console.log('Monaco loaded, defining themes...');
        defineCustomThemes();
        enhanceApp();
        
        // Initialize split view after Monaco is loaded
        if (window.IntegratedSplitView) {
            window.splitView = new window.IntegratedSplitView();
            if (window.app) {
                window.splitView.init(window.app);
            }
        }
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
            
            // Add keyboard shortcuts
            monacoEditor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
                if (app.saveFile) app.saveFile();
            });
            
            monacoEditor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyS, () => {
                if (app.saveAllFiles) app.saveAllFiles();
            });
            
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
    
    // Override openFile to detect language
    const originalOpenFile = app.openFile;
    app.openFile = async function(file) {
        const result = await originalOpenFile.call(this, file);
        
        if (monacoEditor && file && !file.isDir) {
            // Detect language from file extension
            const ext = file.name.split('.').pop().toLowerCase();
            const language = LANGUAGE_MAP[ext] || 'plaintext';
            
            // Get or create model for this file
            const filePath = this.currentPath === '/' 
                ? '/' + file.name 
                : this.currentPath + '/' + file.name;
            
            let model = monacoModels.get(filePath);
            if (!model) {
                const content = this.openFiles.get(filePath)?.content || '';
                model = monaco.editor.createModel(content, language);
                monacoModels.set(filePath, model);
            }
            
            monacoEditor.setModel(model);
            
            // Update file type display
            const typeEl = document.getElementById('fileType');
            if (typeEl) {
                const types = {
                    javascript: 'JavaScript', python: 'Python', 
                    yaml: 'YAML', json: 'JSON', html: 'HTML',
                    css: 'CSS', markdown: 'Markdown',
                    plaintext: 'Plain Text'
                };
                typeEl.textContent = types[language] || language;
            }
        }
        
        return result;
    };
    
    // Override switchToFile to use models
    const originalSwitchToFile = app.switchToFile;
    app.switchToFile = function(path) {
        originalSwitchToFile.call(this, path);
        
        if (monacoEditor) {
            let model = monacoModels.get(path);
            const file = this.openFiles.get(path);
            
            if (file && !model) {
                // Detect language
                const ext = file.name.split('.').pop().toLowerCase();
                const language = LANGUAGE_MAP[ext] || 'plaintext';
                model = monaco.editor.createModel(file.content, language);
                monacoModels.set(path, model);
            }
            
            if (model) {
                monacoEditor.setModel(model);
            }
        }
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
        
        // If no files are open, show welcome message
        if (this.openFiles.size === 0 && monacoEditor) {
            const welcomeModel = monaco.editor.createModel(
                '// Welcome to Pterodactyl Manager\n// Select a file from the file tree to edit\n',
                'javascript'
            );
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
