// Monaco Editor Integration with Advanced Features
console.log('Monaco Editor initializing...');

// Language configurations
const LANGUAGE_MAP = {
    // Programming languages
    'js': 'javascript',
    'jsx': 'javascript',
    'ts': 'typescript',
    'tsx': 'typescript',
    'py': 'python',
    'rb': 'ruby',
    'go': 'go',
    'rs': 'rust',
    'java': 'java',
    'kt': 'kotlin',
    'cpp': 'cpp',
    'c': 'c',
    'cs': 'csharp',
    'php': 'php',
    'swift': 'swift',
    'scala': 'scala',
    'r': 'r',
    'lua': 'lua',
    'perl': 'perl',
    'sh': 'shell',
    'bash': 'shell',
    'zsh': 'shell',
    'ps1': 'powershell',
    
    // Web technologies
    'html': 'html',
    'htm': 'html',
    'css': 'css',
    'scss': 'scss',
    'sass': 'scss',
    'less': 'less',
    
    // Data formats
    'json': 'json',
    'xml': 'xml',
    'yaml': 'yaml',
    'yml': 'yaml',
    'toml': 'toml',
    'ini': 'ini',
    'conf': 'ini',
    'cfg': 'ini',
    
    // Documentation
    'md': 'markdown',
    'markdown': 'markdown',
    'rst': 'restructuredtext',
    'tex': 'latex',
    
    // Database
    'sql': 'sql',
    'mysql': 'mysql',
    'pgsql': 'pgsql',
    
    // Other
    'dockerfile': 'dockerfile',
    'makefile': 'makefile',
    'cmake': 'cmake',
    'graphql': 'graphql',
    'proto': 'protobuf',
    'bat': 'bat',
    'cmd': 'bat',
    'txt': 'plaintext',
    'log': 'log'
};

// File icons mapping
const FILE_ICONS = {
    'javascript': '📜',
    'typescript': '🔷',
    'python': '🐍',
    'go': '🐹',
    'rust': '🦀',
    'java': '☕',
    'php': '🐘',
    'html': '🌐',
    'css': '🎨',
    'json': '📋',
    'yaml': '📄',
    'markdown': '📝',
    'dockerfile': '🐳',
    'sql': '🗃️',
    'shell': '💻',
    'xml': '📰',
    'git': '📦'
};

// Initialize Monaco Loader
require.config({
    paths: {
        'vs': 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs'
    }
});

// Wait for runtime and Monaco
function waitForDependencies() {
    if (typeof window.go !== 'undefined' && 
        typeof window.runtime !== 'undefined' && 
        typeof require !== 'undefined') {
        console.log('Dependencies ready, loading Monaco Editor...');
        loadMonacoEditor();
    } else {
        console.log('Waiting for dependencies...');
        setTimeout(waitForDependencies, 50);
    }
}

function loadMonacoEditor() {
    require(['vs/editor/editor.main'], function() {
        console.log('Monaco Editor loaded, initializing app...');
        // Define custom themes before initializing app
        defineCustomThemes();
        initAdvancedApp();
    });
}

function initAdvancedApp() {
    // Configure Monaco languages
    configureLinters();
    
    // Main app object with Monaco integration
    const app = {
        // Editor instance
        editor: null,
        currentModel: null,
        decorations: [],
        
        // File management
        openFiles: new Map(),
        activeFile: null,
        currentPath: '/',
        selectedFile: null,
        
        // Connection state
        isConnected: false,
        
        // Settings
        settings: {
            theme: 'pterodactyl-dark',
            fontSize: 14,
            tabSize: 4,
            insertSpaces: true,
            wordWrap: false,
            minimap: true,
            lineNumbers: true,
            autoSave: false,
            autoSaveDelay: 1000,
            formatOnSave: false,
            lintingEnabled: true,
            lintOnType: true,
            inlineHints: true
        },
        
        // Auto-save timer
        autoSaveTimer: null,
        
        async init() {
            console.log('Initializing advanced editor app...');
            this.createEditor();
            this.setupEventListeners();
            this.loadSettings();
            await this.checkConfig();
        },
        
        createEditor() {
            const container = document.getElementById('monaco-editor');
            if (!container) {
                console.error('Editor container not found');
                return;
            }
            
            // Create Monaco Editor instance
            this.editor = monaco.editor.create(container, {
                value: '// Welcome to Pterodactyl Manager Pro\n// Select a file to begin editing\n',
                language: 'javascript',
                theme: this.settings.theme,
                fontSize: this.settings.fontSize,
                minimap: { enabled: this.settings.minimap },
                wordWrap: this.settings.wordWrap ? 'on' : 'off',
                lineNumbers: this.settings.lineNumbers ? 'on' : 'off',
                automaticLayout: true,
                scrollBeyondLastLine: false,
                renderWhitespace: 'selection',
                suggestOnTriggerCharacters: true,
                quickSuggestions: true,
                folding: true,
                foldingStrategy: 'indentation',
                showFoldingControls: 'always',
                matchBrackets: 'always',
                bracketPairColorization: { enabled: true },
                guides: {
                    indentation: true,
                    bracketPairs: true
                },
                inlineSuggest: { enabled: true },
                formatOnPaste: true,
                formatOnType: false,
                tabSize: this.settings.tabSize,
                insertSpaces: this.settings.insertSpaces
            });
            
            // Setup editor event handlers
            this.editor.onDidChangeModelContent((e) => {
                this.onEditorChange();
            });
            
            this.editor.onDidChangeCursorPosition((e) => {
                this.updateCursorPosition(e.position);
            });
            
            // Setup keyboard shortcuts
            this.setupKeyboardShortcuts();
            
            console.log('Monaco Editor created successfully');
        },
        
        setupKeyboardShortcuts() {
            // Save file - Ctrl+S
            this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
                this.saveFile();
            });
            
            // Save all - Ctrl+Shift+S
            this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyS, () => {
                this.saveAllFiles();
            });
            
            // Format document - Shift+Alt+F
            this.editor.addCommand(monaco.KeyMod.Shift | monaco.KeyMod.Alt | monaco.KeyCode.KeyF, () => {
                this.formatDocument();
            });
            
            // Toggle comment - Ctrl+/
            this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Slash, () => {
                this.editor.trigger('keyboard', 'editor.action.commentLine', {});
            });
            
            // Find - Ctrl+F
            this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyF, () => {
                this.editor.trigger('keyboard', 'actions.find', {});
            });
            
            // Replace - Ctrl+H
            this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyH, () => {
                this.editor.trigger('keyboard', 'editor.action.startFindReplaceAction', {});
            });
        },
        
        setupEventListeners() {
            // Backend events
            window.runtime.EventsOn('connected', (connected) => {
                console.log('Connection event:', connected);
                this.isConnected = connected;
                this.updateStatus(connected);
                if (connected) {
                    this.loadFiles('/');
                }
            });
            
            window.runtime.EventsOn('server-changed', (serverID) => {
                console.log('Server changed to:', serverID);
                this.closeAllFiles();
                this.loadFiles('/');
            });
            
            // File upload
            const fileInput = document.getElementById('fileInput');
            if (fileInput) {
                fileInput.addEventListener('change', (e) => {
                    this.handleFileUpload(e.target.files);
                });
            }
        },
        
        async checkConfig() {
            try {
                await this.loadPanels();
                const config = await window.go.main.App.GetConfig();
                
                if (config && config.panelURL && config.apiKey) {
                    await this.connect();
                    await this.loadServers();
                }
            } catch (err) {
                console.error('Config check failed:', err);
            }
        },
        
        async connect() {
            try {
                await window.go.main.App.Connect();
                this.updateStatus(true);
                await this.loadFiles('/');
            } catch (err) {
                console.error('Connection failed:', err);
                this.updateStatus(false);
            }
        },
        
        updateStatus(connected) {
            const dot = document.getElementById('statusDot');
            const text = document.getElementById('statusText');
            
            if (dot) {
                dot.classList.toggle('connected', connected);
            }
            
            if (text) {
                text.textContent = connected ? 'Connected' : 'Disconnected';
            }
        },
        
        // File management
        async loadFiles(path) {
            const config = await window.go.main.App.GetConfig();
            if (!config.serverID) {
                const tree = document.getElementById('fileTree');
                if (tree) {
                    tree.innerHTML = '<div class="error">Please select a server</div>';
                }
                return;
            }
            
            this.currentPath = path;
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            
            tree.innerHTML = '<div class="loading">Loading files...</div>';
            
            try {
                const files = await window.go.main.App.ListFiles(path);
                this.renderFiles(files || []);
            } catch (err) {
                console.error('Failed to load files:', err);
                tree.innerHTML = '<div class="error">Failed to load files</div>';
            }
        },
        
        renderFiles(files) {
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            
            tree.innerHTML = '';
            
            // Sort files
            files.sort((a, b) => {
                if (a.isDir !== b.isDir) return b.isDir ? 1 : -1;
                return a.name.localeCompare(b.name);
            });
            
            // Add parent directory
            if (this.currentPath !== '/') {
                const parentItem = this.createFileItem({
                    name: '..',
                    isDir: true,
                    size: 0
                }, true);
                tree.appendChild(parentItem);
            }
            
            // Render files
            files.forEach(file => {
                const item = this.createFileItem(file);
                tree.appendChild(item);
            });
        },
        
        createFileItem(file, isParent = false) {
            const div = document.createElement('div');
            div.className = 'file-item';
            
            const icon = document.createElement('span');
            icon.className = 'file-icon';
            icon.textContent = file.isDir ? '📁' : this.getFileIcon(file.name);
            
            const name = document.createElement('span');
            name.className = 'file-name';
            name.textContent = file.name;
            
            const size = document.createElement('span');
            size.className = 'file-size';
            size.textContent = file.isDir ? '' : this.formatSize(file.size);
            
            div.appendChild(icon);
            div.appendChild(name);
            div.appendChild(size);
            
            div.addEventListener('click', () => {
                if (isParent) {
                    const parts = this.currentPath.split('/').filter(p => p);
                    parts.pop();
                    const parentPath = '/' + parts.join('/');
                    this.loadFiles(parentPath || '/');
                } else if (file.isDir) {
                    const newPath = this.currentPath === '/' 
                        ? '/' + file.name 
                        : this.currentPath + '/' + file.name;
                    this.loadFiles(newPath);
                } else {
                    document.querySelectorAll('.file-item').forEach(item => {
                        item.classList.remove('selected');
                    });
                    div.classList.add('selected');
                    this.selectedFile = file;
                    this.openFile(file);
                }
            });
            
            return div;
        },
        
        getFileIcon(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            const language = LANGUAGE_MAP[ext];
            if (language && FILE_ICONS[language]) {
                return FILE_ICONS[language];
            }
            return '📄';
        },
        
        async openFile(file) {
            if (!file || file.isDir) return;
            
            const filePath = this.currentPath === '/' 
                ? '/' + file.name 
                : this.currentPath + '/' + file.name;
            
            // Check if already open
            if (this.openFiles.has(filePath)) {
                this.switchToFile(filePath);
                return;
            }
            
            try {
                const content = await window.go.main.App.GetFileContent(filePath);
                
                // Detect language
                const language = this.detectLanguage(file.name);
                
                // Create model
                const model = monaco.editor.createModel(content, language);
                
                // Add to open files
                this.openFiles.set(filePath, {
                    name: file.name,
                    model: model,
                    originalContent: content,
                    modified: false,
                    language: language
                });
                
                // Add tab
                this.addEditorTab(filePath, file.name);
                
                // Switch to file
                this.switchToFile(filePath);
                
                // Run linting if enabled
                if (this.settings.lintingEnabled) {
                    this.lintFile(filePath);
                }
                
            } catch (err) {
                alert('Failed to open file: ' + err);
            }
        },
        
        detectLanguage(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            return LANGUAGE_MAP[ext] || 'plaintext';
        },
        
        addEditorTab(path, name) {
            const tabsContainer = document.getElementById('editorTabs');
            if (!tabsContainer) return;
            
            const tab = document.createElement('div');
            tab.className = 'editor-tab';
            tab.dataset.path = path;
            
            const nameSpan = document.createElement('span');
            nameSpan.textContent = name;
            tab.appendChild(nameSpan);
            
            const closeBtn = document.createElement('span');
            closeBtn.className = 'close';
            closeBtn.textContent = '×';
            closeBtn.onclick = (e) => {
                e.stopPropagation();
                this.closeFileTab(path);
            };
            tab.appendChild(closeBtn);
            
            tab.onclick = () => this.switchToFile(path);
            
            tabsContainer.appendChild(tab);
        },
        
        switchToFile(path) {
            const file = this.openFiles.get(path);
            if (!file) return;
            
            this.activeFile = path;
            
            // Update tabs
            document.querySelectorAll('.editor-tab').forEach(tab => {
                tab.classList.toggle('active', tab.dataset.path === path);
            });
            
            // Set model
            this.editor.setModel(file.model);
            this.currentModel = file.model;
            
            // Update UI
            this.updateFileType(file.language);
            
            // Show controls
            document.getElementById('saveBtn').style.display = '';
            document.getElementById('saveAllBtn').style.display = '';
            document.getElementById('formatBtn').style.display = '';
            
            // Focus editor
            this.editor.focus();
        },
        
        onEditorChange() {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (file) {
                const currentContent = file.model.getValue();
                file.modified = currentContent !== file.originalContent;
                this.updateTabModified(this.activeFile, file.modified);
                
                // Auto-save
                if (this.settings.autoSave && file.modified) {
                    this.scheduleAutoSave();
                }
                
                // Lint on type
                if (this.settings.lintingEnabled && this.settings.lintOnType) {
                    this.scheduleLinting();
                }
            }
        },
        
        updateTabModified(path, modified) {
            const tab = document.querySelector(`.editor-tab[data-path="${path}"]`);
            if (tab) {
                tab.classList.toggle('modified', modified);
            }
        },
        
        scheduleAutoSave() {
            if (this.autoSaveTimer) {
                clearTimeout(this.autoSaveTimer);
            }
            this.autoSaveTimer = setTimeout(() => {
                this.saveFile();
            }, this.settings.autoSaveDelay);
        },
        
        scheduleLinting() {
            // Debounce linting
            if (this.lintTimer) {
                clearTimeout(this.lintTimer);
            }
            this.lintTimer = setTimeout(() => {
                this.lintFile(this.activeFile);
            }, 500);
        },
        
        async saveFile() {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (!file || !file.modified) return;
            
            // Format on save if enabled
            if (this.settings.formatOnSave) {
                await this.formatDocument();
            }
            
            try {
                const content = file.model.getValue();
                await window.go.main.App.SaveFileContent(this.activeFile, content);
                file.originalContent = content;
                file.modified = false;
                this.updateTabModified(this.activeFile, false);
                console.log('File saved:', this.activeFile);
            } catch (err) {
                alert('Failed to save file: ' + err);
            }
        },
        
        async saveAllFiles() {
            for (const [path, file] of this.openFiles) {
                if (file.modified) {
                    const currentFile = this.activeFile;
                    this.activeFile = path;
                    await this.saveFile();
                    this.activeFile = currentFile;
                }
            }
        },
        
        async formatDocument() {
            if (!this.editor || !this.currentModel) return;
            
            // Trigger format action
            await this.editor.getAction('editor.action.formatDocument').run();
        },
        
        closeFileTab(path) {
            const file = this.openFiles.get(path);
            if (file) {
                file.model.dispose();
                this.openFiles.delete(path);
            }
            
            // Remove tab
            const tab = document.querySelector(`.editor-tab[data-path="${path}"]`);
            if (tab) tab.remove();
            
            // Switch to another file or clear
            if (this.activeFile === path) {
                this.activeFile = null;
                if (this.openFiles.size > 0) {
                    const nextFile = this.openFiles.keys().next().value;
                    this.switchToFile(nextFile);
                } else {
                    // Clear editor
                    const emptyModel = monaco.editor.createModel('// Select a file to edit\n', 'javascript');
                    this.editor.setModel(emptyModel);
                    this.currentModel = emptyModel;
                    
                    // Hide controls
                    document.getElementById('saveBtn').style.display = 'none';
                    document.getElementById('saveAllBtn').style.display = 'none';
                    document.getElementById('formatBtn').style.display = 'none';
                }
            }
        },
        
        closeAllFiles() {
            for (const [path, file] of this.openFiles) {
                file.model.dispose();
            }
            this.openFiles.clear();
            this.activeFile = null;
            
            // Clear tabs
            const tabsContainer = document.getElementById('editorTabs');
            if (tabsContainer) tabsContainer.innerHTML = '';
            
            // Clear editor
            const emptyModel = monaco.editor.createModel('// Select a file to edit\n', 'javascript');
            this.editor.setModel(emptyModel);
            this.currentModel = emptyModel;
        },
        
        // Linting
        async lintFile(path) {
            if (!this.settings.lintingEnabled) return;
            
            const file = this.openFiles.get(path);
            if (!file) return;
            
            // Get markers based on language
            const markers = await this.getLanguageMarkers(file.language, file.model);
            
            // Set markers
            monaco.editor.setModelMarkers(file.model, 'owner', markers);
            
            // Update problem count
            this.updateProblemCount(markers.length);
        },
        
        async getLanguageMarkers(language, model) {
            const markers = [];
            const content = model.getValue();
            const lines = content.split('\n');
            
            // Language-specific linting rules
            switch (language) {
                case 'javascript':
                case 'typescript':
                    markers.push(...this.lintJavaScript(lines, model));
                    break;
                case 'python':
                    markers.push(...this.lintPython(lines, model));
                    break;
                case 'json':
                    markers.push(...this.lintJSON(content, model));
                    break;
                case 'yaml':
                    markers.push(...this.lintYAML(lines, model));
                    break;
                case 'markdown':
                    markers.push(...this.lintMarkdown(lines, model));
                    break;
                case 'html':
                    markers.push(...this.lintHTML(content, model));
                    break;
                case 'css':
                case 'scss':
                    markers.push(...this.lintCSS(lines, model));
                    break;
            }
            
            return markers;
        },
        
        lintJavaScript(lines, model) {
            const markers = [];
            
            lines.forEach((line, i) => {
                // Check for console.log statements
                if (line.includes('console.log') && !line.trim().startsWith('//')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Warning,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('console.log') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('console.log') + 12,
                        message: 'console.log should be removed in production'
                    });
                }
                
                // Check for debugger statements
                if (line.includes('debugger') && !line.trim().startsWith('//')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Error,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('debugger') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('debugger') + 9,
                        message: 'debugger statement should be removed'
                    });
                }
                
                // Check for TODO comments
                if (line.includes('TODO')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Info,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('TODO') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('TODO') + 5,
                        message: 'TODO comment found'
                    });
                }
            });
            
            return markers;
        },
        
        lintPython(lines, model) {
            const markers = [];
            
            lines.forEach((line, i) => {
                // Check for print statements
                if (line.includes('print(') && !line.trim().startsWith('#')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Warning,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('print(') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('print(') + 6,
                        message: 'Consider using logging instead of print'
                    });
                }
                
                // Check for missing colons
                if ((line.includes('if ') || line.includes('for ') || line.includes('while ') || 
                     line.includes('def ') || line.includes('class ')) && 
                    !line.trim().startsWith('#') && !line.endsWith(':')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Error,
                        startLineNumber: i + 1,
                        startColumn: line.length,
                        endLineNumber: i + 1,
                        endColumn: line.length + 1,
                        message: 'Missing colon'
                    });
                }
            });
            
            return markers;
        },
        
        lintJSON(content, model) {
            const markers = [];
            
            try {
                JSON.parse(content);
            } catch (err) {
                const match = err.message.match(/position (\d+)/);
                if (match) {
                    const position = parseInt(match[1]);
                    const lines = content.substring(0, position).split('\n');
                    const lineNumber = lines.length;
                    const column = lines[lines.length - 1].length + 1;
                    
                    markers.push({
                        severity: monaco.MarkerSeverity.Error,
                        startLineNumber: lineNumber,
                        startColumn: column,
                        endLineNumber: lineNumber,
                        endColumn: column + 1,
                        message: err.message
                    });
                }
            }
            
            return markers;
        },
        
        lintYAML(lines, model) {
            const markers = [];
            let indentStack = [0];
            
            lines.forEach((line, i) => {
                if (line.trim() === '') return;
                
                const indent = line.search(/\S/);
                if (indent === -1) return;
                
                // Check for tabs
                if (line.includes('\t')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Error,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('\t') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('\t') + 2,
                        message: 'YAML files should not contain tabs'
                    });
                }
                
                // Check indentation
                if (indent % 2 !== 0) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Warning,
                        startLineNumber: i + 1,
                        startColumn: 1,
                        endLineNumber: i + 1,
                        endColumn: indent + 1,
                        message: 'Inconsistent indentation (should be multiple of 2)'
                    });
                }
            });
            
            return markers;
        },
        
        lintMarkdown(lines, model) {
            const markers = [];
            
            lines.forEach((line, i) => {
                // Check for multiple consecutive blank lines
                if (i > 0 && line.trim() === '' && lines[i - 1].trim() === '') {
                    markers.push({
                        severity: monaco.MarkerSeverity.Info,
                        startLineNumber: i + 1,
                        startColumn: 1,
                        endLineNumber: i + 1,
                        endColumn: 1,
                        message: 'Multiple consecutive blank lines'
                    });
                }
                
                // Check for trailing spaces
                if (line.endsWith(' ') && !line.endsWith('  ')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Warning,
                        startLineNumber: i + 1,
                        startColumn: line.length,
                        endLineNumber: i + 1,
                        endColumn: line.length + 1,
                        message: 'Trailing space (use two spaces for line break)'
                    });
                }
            });
            
            return markers;
        },
        
        lintHTML(content, model) {
            const markers = [];
            
            // Check for unclosed tags
            const openTags = content.match(/<(\w+)[^>]*>/g) || [];
            const closeTags = content.match(/<\/(\w+)>/g) || [];
            
            const selfClosing = ['img', 'br', 'hr', 'input', 'meta', 'link'];
            const openCount = {};
            const closeCount = {};
            
            openTags.forEach(tag => {
                const tagName = tag.match(/<(\w+)/)[1];
                if (!selfClosing.includes(tagName)) {
                    openCount[tagName] = (openCount[tagName] || 0) + 1;
                }
            });
            
            closeTags.forEach(tag => {
                const tagName = tag.match(/<\/(\w+)/)[1];
                closeCount[tagName] = (closeCount[tagName] || 0) + 1;
            });
            
            for (const tag in openCount) {
                if (openCount[tag] !== (closeCount[tag] || 0)) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Error,
                        startLineNumber: 1,
                        startColumn: 1,
                        endLineNumber: 1,
                        endColumn: 1,
                        message: `Unclosed <${tag}> tag`
                    });
                }
            }
            
            return markers;
        },
        
        lintCSS(lines, model) {
            const markers = [];
            
            lines.forEach((line, i) => {
                // Check for !important
                if (line.includes('!important')) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Warning,
                        startLineNumber: i + 1,
                        startColumn: line.indexOf('!important') + 1,
                        endLineNumber: i + 1,
                        endColumn: line.indexOf('!important') + 11,
                        message: 'Avoid using !important'
                    });
                }
                
                // Check for vendor prefixes
                if (line.match(/-webkit-|-moz-|-ms-|-o-/)) {
                    markers.push({
                        severity: monaco.MarkerSeverity.Info,
                        startLineNumber: i + 1,
                        startColumn: 1,
                        endLineNumber: i + 1,
                        endColumn: line.length,
                        message: 'Consider using autoprefixer for vendor prefixes'
                    });
                }
            });
            
            return markers;
        },
        
        updateProblemCount(count) {
            const countEl = document.getElementById('problemCount');
            const statusEl = document.getElementById('problemStatus');
            
            if (countEl) countEl.textContent = count;
            if (statusEl) {
                if (count === 0) {
                    statusEl.textContent = 'No Problems';
                } else if (count === 1) {
                    statusEl.textContent = '1 Problem';
                } else {
                    statusEl.textContent = `${count} Problems`;
                }
            }
        },
        
        updateCursorPosition(position) {
            const lineInfo = document.getElementById('lineInfo');
            if (lineInfo) {
                lineInfo.textContent = `Ln ${position.lineNumber}, Col ${position.column}`;
            }
        },
        
        updateFileType(language) {
            const typeEl = document.getElementById('fileType');
            if (typeEl) {
                const languageNames = {
                    'javascript': 'JavaScript',
                    'typescript': 'TypeScript',
                    'python': 'Python',
                    'html': 'HTML',
                    'css': 'CSS',
                    'json': 'JSON',
                    'yaml': 'YAML',
                    'markdown': 'Markdown',
                    'plaintext': 'Plain Text'
                };
                typeEl.textContent = languageNames[language] || language;
            }
        },
        
        // Settings management
        loadSettings() {
            const saved = localStorage.getItem('editorSettings');
            if (saved) {
                this.settings = { ...this.settings, ...JSON.parse(saved) };
            }
            this.applySettings();
        },
        
        saveSettings() {
            localStorage.setItem('editorSettings', JSON.stringify(this.settings));
        },
        
        applySettings() {
            if (!this.editor) return;
            
            this.editor.updateOptions({
                theme: this.settings.theme,
                fontSize: this.settings.fontSize,
                wordWrap: this.settings.wordWrap ? 'on' : 'off',
                minimap: { enabled: this.settings.minimap },
                lineNumbers: this.settings.lineNumbers ? 'on' : 'off',
                tabSize: this.settings.tabSize,
                insertSpaces: this.settings.insertSpaces
            });
            
            // Update UI
            document.getElementById('autoSaveStatus').textContent = 
                `Auto-save: ${this.settings.autoSave ? 'On' : 'Off'}`;
        },
        
        // UI Controls
        toggleSettings() {
            const panel = document.getElementById('settingsPanel');
            if (panel) {
                panel.classList.toggle('show');
            }
        },
        
        toggleSidebar() {
            const sidebar = document.getElementById('sidebar');
            if (sidebar) {
                sidebar.classList.toggle('collapsed');
            }
        },
        
        toggleLintPanel() {
            const panel = document.getElementById('lintPanel');
            if (panel) {
                panel.classList.toggle('show');
            }
        },
        
        closeLintPanel() {
            const panel = document.getElementById('lintPanel');
            if (panel) {
                panel.classList.remove('show');
            }
        },
        
        toggleTerminal() {
            // Terminal implementation would go here
            console.log('Terminal toggle not implemented');
        },
        
        // Settings handlers
        changeTheme(theme) {
            this.settings.theme = theme;
            this.applySettings();
            this.saveSettings();
        },
        
        changeFontSize(size) {
            this.settings.fontSize = parseInt(size);
            this.applySettings();
            this.saveSettings();
        },
        
        toggleLineNumbers(enabled) {
            this.settings.lineNumbers = enabled;
            this.applySettings();
            this.saveSettings();
        },
        
        toggleWordWrap(enabled) {
            this.settings.wordWrap = enabled;
            this.applySettings();
            this.saveSettings();
        },
        
        toggleMinimap(enabled) {
            this.settings.minimap = enabled;
            this.applySettings();
            this.saveSettings();
        },
        
        toggleAutoSave(enabled) {
            this.settings.autoSave = enabled;
            this.applySettings();
            this.saveSettings();
        },
        
        changeAutoSaveDelay(delay) {
            this.settings.autoSaveDelay = parseInt(delay);
            this.saveSettings();
        },
        
        toggleFormatOnSave(enabled) {
            this.settings.formatOnSave = enabled;
            this.saveSettings();
        },
        
        changeTabSize(size) {
            this.settings.tabSize = parseInt(size);
            this.applySettings();
            this.saveSettings();
        },
        
        toggleInsertSpaces(enabled) {
            this.settings.insertSpaces = enabled;
            this.applySettings();
            this.saveSettings();
        },
        
        toggleLinting(enabled) {
            this.settings.lintingEnabled = enabled;
            this.saveSettings();
            if (enabled && this.activeFile) {
                this.lintFile(this.activeFile);
            } else if (!enabled && this.currentModel) {
                monaco.editor.setModelMarkers(this.currentModel, 'owner', []);
            }
        },
        
        toggleLintOnType(enabled) {
            this.settings.lintOnType = enabled;
            this.saveSettings();
        },
        
        toggleInlineHints(enabled) {
            this.settings.inlineHints = enabled;
            if (this.editor) {
                this.editor.updateOptions({
                    inlineSuggest: { enabled: enabled }
                });
            }
            this.saveSettings();
        },
        
        // Panel management
        async loadPanels() {
            try {
                const panels = await window.go.main.App.ListPanels();
                const activePanel = await window.go.main.App.GetActivePanel();
                const dropdown = document.getElementById('panelDropdown');
                
                if (!dropdown) return;
                
                dropdown.innerHTML = '<option value="" disabled>Select Panel</option>';
                
                panels.forEach(panel => {
                    const option = document.createElement('option');
                    option.value = panel;
                    option.textContent = panel;
                    option.selected = panel === activePanel;
                    dropdown.appendChild(option);
                });
            } catch (err) {
                console.error('Failed to load panels:', err);
            }
        },
        
        async switchPanel(panelName) {
            if (!panelName) return;
            
            try {
                await window.go.main.App.SetActivePanel(panelName);
                await this.connect();
                await this.loadServers();
                await this.loadFiles('/');
            } catch (err) {
                console.error('Failed to switch panel:', err);
            }
        },
        
        async loadServers() {
            try {
                const servers = await window.go.main.App.ListServers();
                const config = await window.go.main.App.GetConfig();
                const dropdown = document.getElementById('serverDropdown');
                
                if (!dropdown) return;
                
                dropdown.innerHTML = '<option value="" disabled>Select Server</option>';
                
                servers.forEach(server => {
                    const option = document.createElement('option');
                    option.value = server.id;
                    option.textContent = server.name;
                    option.selected = server.id === config.serverID;
                    dropdown.appendChild(option);
                });
                
                if (!config.serverID && servers.length > 0) {
                    dropdown.value = servers[0].id;
                    await this.switchServer(servers[0].id);
                }
            } catch (err) {
                console.error('Failed to load servers:', err);
            }
        },
        
        async switchServer(serverID) {
            if (!serverID) return;
            
            try {
                await window.go.main.App.SwitchServer(serverID);
                const dropdown = document.getElementById('serverDropdown');
                if (dropdown) {
                    dropdown.value = serverID;
                }
            } catch (err) {
                console.error('Failed to switch server:', err);
            }
        },
        
        showSettings() {
            // Show main settings modal
            alert('Main settings not implemented');
        },
        
        formatSize(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
        }
    };
    
    // Set global app reference
    window.app = app;
    
    // Initialize app
    app.init();
    
    console.log('Advanced Monaco Editor app initialized');
}

// Define custom themes with vibrant colors
function defineCustomThemes() {
    // Pterodactyl Dark Theme - Vibrant colors for better visibility
    monaco.editor.defineTheme('pterodactyl-dark', {
        base: 'vs-dark',
        inherit: true,
        rules: [
            // Comments - Bright green
            { token: 'comment', foreground: '7EC768', fontStyle: 'italic' },
            { token: 'comment.doc', foreground: '7EC768', fontStyle: 'italic' },
            
            // Strings - Bright orange
            { token: 'string', foreground: 'FFA657' },
            { token: 'string.escape', foreground: 'FFD580' },
            { token: 'string.sql', foreground: 'FFA657' },
            
            // Numbers - Bright purple
            { token: 'number', foreground: 'D4BFFF' },
            { token: 'number.hex', foreground: 'D4BFFF' },
            { token: 'number.float', foreground: 'D4BFFF' },
            
            // Keywords - Bright blue
            { token: 'keyword', foreground: '569CD6', fontStyle: 'bold' },
            { token: 'keyword.control', foreground: 'FF79C6' },
            { token: 'keyword.operator', foreground: 'FF79C6' },
            
            // Functions - Bright yellow
            { token: 'function', foreground: 'FFFF00' },
            { token: 'function.call', foreground: 'F1FA8C' },
            
            // Variables - Bright cyan
            { token: 'variable', foreground: '8BE9FD' },
            { token: 'variable.parameter', foreground: 'FFB86C' },
            { token: 'variable.other', foreground: '8BE9FD' },
            
            // Types - Bright magenta
            { token: 'type', foreground: 'FF79C6' },
            { token: 'class', foreground: 'BD93F9' },
            { token: 'interface', foreground: 'BD93F9' },
            
            // YAML specific
            { token: 'key.yaml', foreground: 'FF79C6', fontStyle: 'bold' },
            { token: 'string.yaml', foreground: 'F1FA8C' },
            { token: 'number.yaml', foreground: 'BD93F9' },
            { token: 'boolean.yaml', foreground: 'FFB86C' },
            
            // JSON specific
            { token: 'string.key.json', foreground: 'FF79C6', fontStyle: 'bold' },
            { token: 'string.value.json', foreground: 'F1FA8C' },
            { token: 'number.json', foreground: 'BD93F9' },
            { token: 'keyword.json', foreground: 'FFB86C' },
            
            // TOML specific
            { token: 'keyword.toml', foreground: 'FF79C6', fontStyle: 'bold' },
            { token: 'string.toml', foreground: 'F1FA8C' },
            { token: 'number.toml', foreground: 'BD93F9' },
            { token: 'variable.toml', foreground: '8BE9FD' },
            
            // XML/HTML
            { token: 'tag', foreground: 'FF79C6' },
            { token: 'attribute.name', foreground: '8BE9FD' },
            { token: 'attribute.value', foreground: 'F1FA8C' },
            
            // CSS
            { token: 'selector', foreground: 'FF79C6' },
            { token: 'attribute.name.css', foreground: '8BE9FD' },
            { token: 'attribute.value.css', foreground: 'F1FA8C' },
            { token: 'property.css', foreground: '8BE9FD' },
            
            // Markdown
            { token: 'markup.heading', foreground: 'FF79C6', fontStyle: 'bold' },
            { token: 'markup.bold', fontStyle: 'bold' },
            { token: 'markup.italic', fontStyle: 'italic' },
            { token: 'markup.inserted', foreground: '50FA7B' },
            { token: 'markup.deleted', foreground: 'FF5555' },
            { token: 'markup.underline', fontStyle: 'underline' },
            { token: 'markup.link', foreground: '8BE9FD' },
            
            // Diff
            { token: 'inserted', foreground: '50FA7B', background: '50FA7B22' },
            { token: 'deleted', foreground: 'FF5555', background: 'FF555522' }
        ],
        colors: {
            'editor.background': '#0B1020',
            'editor.foreground': '#F8F8F2',
            'editorLineNumber.foreground': '#6272A4',
            'editorLineNumber.activeForeground': '#FFB86C',
            'editor.selectionBackground': '#44475A',
            'editor.lineHighlightBackground': '#1A1F35',
            'editorCursor.foreground': '#FFB86C',
            'editorWhitespace.foreground': '#424450',
            'editorIndentGuide.background': '#424450',
            'editorIndentGuide.activeBackground': '#6272A4',
            'editor.findMatchBackground': '#FFB86C44',
            'editor.findMatchHighlightBackground': '#8BE9FD22',
            'editorBracketMatch.background': '#6272A444',
            'editorBracketMatch.border': '#FFB86C',
            'editor.wordHighlightBackground': '#8BE9FD22',
            'editor.wordHighlightStrongBackground': '#50FA7B22',
            'editorError.foreground': '#FF5555',
            'editorWarning.foreground': '#FFB86C',
            'editorInfo.foreground': '#8BE9FD'
        }
    });
    
    // Pterodactyl Light Theme - Vibrant light colors
    monaco.editor.defineTheme('pterodactyl-light', {
        base: 'vs',
        inherit: true,
        rules: [
            // Comments - Forest green
            { token: 'comment', foreground: '008000', fontStyle: 'italic' },
            
            // Strings - Deep orange
            { token: 'string', foreground: 'D14800' },
            
            // Numbers - Purple
            { token: 'number', foreground: '9C27B0' },
            
            // Keywords - Deep blue
            { token: 'keyword', foreground: '0000FF', fontStyle: 'bold' },
            { token: 'keyword.control', foreground: 'AF00DB' },
            
            // Functions - Dark golden
            { token: 'function', foreground: 'B8860B', fontStyle: 'bold' },
            
            // Variables - Teal
            { token: 'variable', foreground: '008B8B' },
            
            // Types - Magenta
            { token: 'type', foreground: 'AF00DB' },
            { token: 'class', foreground: 'AF00DB' },
            
            // YAML specific
            { token: 'key.yaml', foreground: '0000FF', fontStyle: 'bold' },
            { token: 'string.yaml', foreground: 'D14800' },
            
            // JSON specific
            { token: 'string.key.json', foreground: '0000FF', fontStyle: 'bold' },
            { token: 'string.value.json', foreground: 'D14800' }
        ],
        colors: {
            'editor.background': '#FFFFFF',
            'editor.foreground': '#000000',
            'editor.selectionBackground': '#ADD6FF',
            'editor.lineHighlightBackground': '#F5F5F5',
            'editorCursor.foreground': '#000000',
            'editorLineNumber.foreground': '#008000',
            'editorLineNumber.activeForeground': '#0000FF'
        }
    });
    
    // Neon Theme - Ultra vibrant for dark environments
    monaco.editor.defineTheme('neon', {
        base: 'vs-dark',
        inherit: true,
        rules: [
            { token: 'comment', foreground: '00FF00', fontStyle: 'italic' },
            { token: 'string', foreground: 'FFFF00' },
            { token: 'number', foreground: 'FF00FF' },
            { token: 'keyword', foreground: '00FFFF', fontStyle: 'bold' },
            { token: 'function', foreground: 'FF00FF', fontStyle: 'bold' },
            { token: 'variable', foreground: '00FF00' },
            { token: 'type', foreground: 'FF00FF' },
            { token: 'class', foreground: 'FFFF00' }
        ],
        colors: {
            'editor.background': '#000000',
            'editor.foreground': '#00FF00',
            'editorCursor.foreground': '#FFFF00',
            'editor.lineHighlightBackground': '#001100',
            'editor.selectionBackground': '#00FF0044'
        }
    });
}

// Configure language-specific linters and highlighting
function configureLinters() {
    // Configure TypeScript/JavaScript compiler options
    monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
        noSemanticValidation: false,
        noSyntaxValidation: false,
        noSuggestionDiagnostics: false,
        diagnosticCodesToIgnore: []
    });
    
    monaco.languages.typescript.javascriptDefaults.setCompilerOptions({
        target: monaco.languages.typescript.ScriptTarget.ES2020,
        allowNonTsExtensions: true,
        moduleResolution: monaco.languages.typescript.ModuleResolutionKind.NodeJs,
        module: monaco.languages.typescript.ModuleKind.CommonJS,
        noEmit: true,
        allowJs: true,
        checkJs: true
    });
    
    // Configure JSON validation with enhanced schema support
    monaco.languages.json.jsonDefaults.setDiagnosticsOptions({
        validate: true,
        schemas: [
            {
                uri: 'http://json-schema.org/draft-07/schema#',
                fileMatch: ['*.json'],
                schema: {
                    type: 'object'
                }
            },
            {
                fileMatch: ['package.json'],
                uri: 'https://json.schemastore.org/package',
            },
            {
                fileMatch: ['tsconfig.json', 'tsconfig.*.json'],
                uri: 'https://json.schemastore.org/tsconfig',
            },
            {
                fileMatch: ['.eslintrc.json', '.eslintrc'],
                uri: 'https://json.schemastore.org/eslintrc',
            }
        ],
        allowComments: true,
        trailingCommas: 'warning'
    });
    
    // Configure HTML options
    monaco.languages.html.htmlDefaults.setOptions({
        format: {
            tabSize: 4,
            insertSpaces: true,
            endWithNewline: true
        },
        suggest: {
            html5: true
        }
    });
    
    // Configure CSS options  
    monaco.languages.css.cssDefaults.setOptions({
        validate: true,
        lint: {
            compatibleVendorPrefixes: 'warning',
            vendorPrefix: 'warning',
            duplicateProperties: 'warning',
            emptyRules: 'warning',
            importStatement: 'ignore',
            boxModel: 'warning',
            universalSelector: 'ignore',
            zeroUnits: 'ignore',
            fontFaceProperties: 'warning',
            hexColorLength: 'error',
            argumentsInColorFunction: 'error',
            unknownProperties: 'warning',
            ieHack: 'ignore',
            unknownVendorSpecificProperties: 'ignore',
            propertyIgnoredDueToDisplay: 'warning',
            important: 'warning',
            float: 'ignore',
            idSelector: 'ignore'
        }
    });
    
    // Register additional language configurations for better highlighting
    registerLanguageConfigurations();
}

// Register enhanced language configurations
function registerLanguageConfigurations() {
    // Enhanced YAML configuration
    monaco.languages.setLanguageConfiguration('yaml', {
        wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
        comments: {
            lineComment: '#',
        },
        brackets: [
            ['{', '}'],
            ['[', ']'],
            ['(', ')']
        ],
        autoClosingPairs: [
            { open: '{', close: '}' },
            { open: '[', close: ']' },
            { open: '(', close: ')' },
            { open: '"', close: '"' },
            { open: "'", close: "'" }
        ],
        surroundingPairs: [
            { open: '{', close: '}' },
            { open: '[', close: ']' },
            { open: '(', close: ')' },
            { open: '"', close: '"' },
            { open: "'", close: "'" }
        ],
        folding: {
            offSide: true
        }
    });
    
    // TOML configuration
    monaco.languages.setLanguageConfiguration('toml', {
        wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
        comments: {
            lineComment: '#',
        },
        brackets: [
            ['[', ']'],
            ['{', '}'],
            ['(', ')']
        ],
        autoClosingPairs: [
            { open: '[', close: ']' },
            { open: '{', close: '}' },
            { open: '(', close: ')' },
            { open: '"', close: '"' },
            { open: "'", close: "'" },
            { open: '"""', close: '"""' },
            { open: "'''", close: "'''" }
        ]
    });
    
    // INI configuration
    monaco.languages.setLanguageConfiguration('ini', {
        wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
        comments: {
            lineComment: ';',
        },
        brackets: [
            ['[', ']']
        ],
        autoClosingPairs: [
            { open: '[', close: ']' },
            { open: '"', close: '"' },
            { open: "'", close: "'" }
        ]
    });
    
    // Register custom tokenizers for better syntax highlighting
    registerCustomTokenizers();
}

// Register custom tokenizers for enhanced highlighting
function registerCustomTokenizers() {
    // Enhanced YAML tokenizer
    monaco.languages.setMonarchTokensProvider('yaml', {
        tokenizer: {
            root: [
                [/^(\s*)([\w\-]+)(\s*):/, ['white', 'key.yaml', 'white']],
                [/#.*$/, 'comment'],
                [/"[^"]*"/, 'string.yaml'],
                [/'[^']*'/, 'string.yaml'],
                [/\b(true|false|null|yes|no|on|off)\b/, 'boolean.yaml'],
                [/\b\d+\.\d+\b/, 'number.float.yaml'],
                [/\b\d+\b/, 'number.yaml'],
                [/[\[\]{}]/, 'delimiter.bracket'],
                [/[-]/, 'delimiter']
            ]
        }
    });
    
    // Enhanced TOML tokenizer
    monaco.languages.setMonarchTokensProvider('toml', {
        tokenizer: {
            root: [
                [/^\[[^\]]+\]/, 'keyword.toml'],
                [/^([\w\-]+)\s*=/, 'variable.toml'],
                [/#.*$/, 'comment'],
                [/"""[\s\S]*?"""/, 'string.toml'],
                [/'''[\s\S]*?'''/, 'string.toml'],
                [/"[^"]*"/, 'string.toml'],
                [/'[^']*'/, 'string.toml'],
                [/\b(true|false)\b/, 'keyword.toml'],
                [/\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}([\+\-]\d{2}:\d{2}|Z)?\b/, 'number.toml'],
                [/\b\d+\.\d+\b/, 'number.float.toml'],
                [/\b\d+\b/, 'number.toml']
            ]
        }
    });
    
    // Enhanced INI tokenizer
    monaco.languages.setMonarchTokensProvider('ini', {
        tokenizer: {
            root: [
                [/^\[[^\]]+\]/, 'keyword'],
                [/^([\w\-]+)\s*=/, 'variable'],
                [/[;#].*$/, 'comment'],
                [/"[^"]*"/, 'string'],
                [/'[^']*'/, 'string'],
                [/\b(true|false|yes|no|on|off)\b/i, 'keyword'],
                [/\b\d+\.\d+\b/, 'number.float'],
                [/\b\d+\b/, 'number']
            ]
        }
    });
}

// Start initialization
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForDependencies);
} else {
    waitForDependencies();
}
