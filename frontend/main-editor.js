// Enhanced Pterodactyl Manager with Integrated Editor
console.log('Pterodactyl Manager Pro starting...');

// Simple syntax highlighter
class SimpleEditor {
    constructor(container) {
        this.container = container;
        this.textarea = null;
        this.lineNumbers = null;
        this.content = '';
        this.showLineNumbers = true;
        this.wordWrap = false;
        this.onChange = null;
    }
    
    create() {
        this.container.innerHTML = `
            <div style="display: flex; height: 100%; position: relative;">
                <div id="lineNumbers" style="
                    background: #1a1a2e;
                    color: #666;
                    padding: 12px 8px;
                    text-align: right;
                    font-family: 'Consolas', monospace;
                    font-size: 14px;
                    line-height: 21px;
                    user-select: none;
                    overflow: hidden;
                    ${this.showLineNumbers ? '' : 'display: none;'}
                "></div>
                <textarea id="codeEditor" style="
                    flex: 1;
                    background: #0b1020;
                    color: #e5e7eb;
                    border: none;
                    outline: none;
                    padding: 12px;
                    font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
                    font-size: 14px;
                    line-height: 21px;
                    resize: none;
                    tab-size: 4;
                    white-space: ${this.wordWrap ? 'pre-wrap' : 'pre'};
                    overflow: auto;
                " spellcheck="false"></textarea>
            </div>
        `;
        
        this.textarea = document.getElementById('codeEditor');
        this.lineNumbers = document.getElementById('lineNumbers');
        
        // Update line numbers on input
        this.textarea.addEventListener('input', () => {
            this.updateLineNumbers();
            if (this.onChange) this.onChange(this.textarea.value);
        });
        
        // Sync scroll
        this.textarea.addEventListener('scroll', () => {
            this.lineNumbers.scrollTop = this.textarea.scrollTop;
        });
        
        // Handle tab key
        this.textarea.addEventListener('keydown', (e) => {
            if (e.key === 'Tab') {
                e.preventDefault();
                const start = this.textarea.selectionStart;
                const end = this.textarea.selectionEnd;
                const value = this.textarea.value;
                this.textarea.value = value.substring(0, start) + '    ' + value.substring(end);
                this.textarea.selectionStart = this.textarea.selectionEnd = start + 4;
            }
        });
    }
    
    setValue(content) {
        this.content = content;
        if (this.textarea) {
            this.textarea.value = content;
            this.updateLineNumbers();
        }
    }
    
    getValue() {
        return this.textarea ? this.textarea.value : this.content;
    }
    
    updateLineNumbers() {
        if (!this.lineNumbers || !this.showLineNumbers) return;
        const lines = this.textarea.value.split('\n').length;
        let html = '';
        for (let i = 1; i <= lines; i++) {
            html += i + '<br>';
        }
        this.lineNumbers.innerHTML = html;
    }
    
    setShowLineNumbers(show) {
        this.showLineNumbers = show;
        if (this.lineNumbers) {
            this.lineNumbers.style.display = show ? '' : 'none';
        }
    }
    
    setWordWrap(wrap) {
        this.wordWrap = wrap;
        if (this.textarea) {
            this.textarea.style.whiteSpace = wrap ? 'pre-wrap' : 'pre';
        }
    }
    
    focus() {
        if (this.textarea) this.textarea.focus();
    }
}

// Wait for runtime
function waitForRuntime() {
    if (typeof window.go !== 'undefined' && typeof window.runtime !== 'undefined') {
        console.log('Runtime ready, initializing enhanced app...');
        initApp();
    } else {
        console.log('Waiting for runtime...');
        setTimeout(waitForRuntime, 50);
    }
}

function initApp() {
    console.log('Initializing enhanced application...');
    
    // Main app object
    const app = {
        currentPath: '/',
        selectedFile: null,
        contextFile: null,
        isConnected: false,
        consoleConnected: false,
        
        // Editor state
        editor: null,
        openFiles: new Map(), // path -> {content, modified, originalContent}
        activeFile: null,
        autoSave: false,
        
        async init() {
            console.log('App init started');
            this.setupEditor();
            this.setupEventListeners();
            this.setupKeyboardShortcuts();
            await this.checkConfig();
        },
        
        setupEditor() {
            const editorContainer = document.getElementById('editor');
            if (editorContainer) {
                this.editor = new SimpleEditor(editorContainer);
                this.editor.onChange = (content) => {
                    this.onEditorChange(content);
                };
                // Create the editor immediately
                this.editor.create();
                this.editor.setValue('// Welcome to Pterodactyl Manager\n// Select a file from the file tree to edit\n');
            }
        },
        
        setupKeyboardShortcuts() {
            document.addEventListener('keydown', (e) => {
                // Ctrl+S - Save
                if (e.ctrlKey && e.key === 's') {
                    e.preventDefault();
                    if (e.shiftKey) {
                        this.saveAllFiles();
                    } else {
                        this.saveFile();
                    }
                }
                // Ctrl+W - Close file
                if (e.ctrlKey && e.key === 'w') {
                    e.preventDefault();
                    this.closeFile();
                }
                // Ctrl+N - New file
                if (e.ctrlKey && e.key === 'n') {
                    e.preventDefault();
                    this.newFile();
                }
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
            
            window.runtime.EventsOn('console-output', (message) => {
                this.appendConsole(message);
            });
            
            window.runtime.EventsOn('console-error', (error) => {
                this.appendConsole('[ERROR] ' + error, 'error');
            });
            
            window.runtime.EventsOn('console-connected', (connected) => {
                console.log('Console connected:', connected);
                this.consoleConnected = connected;
                const btn = document.getElementById('connectBtn');
                if (btn) {
                    btn.textContent = connected ? 'Disconnect' : 'Connect';
                }
                if (connected) {
                    this.appendConsole('=== Console connected ===', 'info');
                }
            });
            
            window.runtime.EventsOn('server-changed', (serverID) => {
                console.log('Server changed to:', serverID);
                this.appendConsole('=== Switched to server: ' + serverID + ' ===', 'info');
                // Clear console and reload files for the new server
                this.clearConsole();
                this.loadFiles('/');
                // Close all open files as they belong to the previous server
                this.closeAllFiles();
            });
            
            // File upload
            const fileInput = document.getElementById('fileInput');
            if (fileInput) {
                fileInput.addEventListener('change', (e) => {
                    this.handleFileUpload(e.target.files);
                });
            }
            
            // Console input
            const commandInput = document.getElementById('commandInput');
            if (commandInput) {
                commandInput.addEventListener('keydown', (e) => {
                    if (e.key === 'Enter') {
                        this.sendCommand();
                    }
                });
            }
            
            // Hide context menu
            document.addEventListener('click', () => {
                const menu = document.getElementById('contextMenu');
                if (menu) menu.classList.remove('show');
            });
        },
        
        async checkConfig() {
            try {
                const config = await window.go.main.App.GetConfig();
                console.log('Config loaded:', config);
                
                if (config && config.panelURL && config.apiKey) {
                    // Connect even without server ID (we'll select from dropdown)
                    await this.connect();
                    // Load servers after connecting
                    await this.loadServers();
                } else {
                    this.showSettings();
                }
                
                // Load editor preferences
                this.autoSave = localStorage.getItem('autoSave') === 'true';
                const wordWrap = localStorage.getItem('wordWrap') === 'true';
                const showLineNumbers = localStorage.getItem('showLineNumbers') !== 'false';
                
                if (this.editor) {
                    this.editor.setWordWrap(wordWrap);
                    this.editor.setShowLineNumbers(showLineNumbers);
                }
                
                // Update UI
                const autoSaveCheckbox = document.getElementById('autoSave');
                if (autoSaveCheckbox) autoSaveCheckbox.checked = this.autoSave;
                const wordWrapCheckbox = document.getElementById('wordWrap');
                if (wordWrapCheckbox) wordWrapCheckbox.checked = wordWrap;
                const lineNumbersCheckbox = document.getElementById('showLineNumbers');
                if (lineNumbersCheckbox) lineNumbersCheckbox.checked = showLineNumbers;
                
                this.updateAutoSaveStatus();
            } catch (err) {
                console.error('Config check failed:', err);
                this.showSettings();
            }
        },
        
        async connect() {
            try {
                console.log('Connecting to server...');
                await window.go.main.App.Connect();
                console.log('Connected successfully');
                this.updateStatus(true);
                await this.loadFiles('/');
            } catch (err) {
                console.error('Connection failed:', err);
                this.updateStatus(false);
                alert('Connection failed: ' + err);
            }
        },
        
        updateStatus(connected) {
            const dot = document.getElementById('statusDot');
            const text = document.getElementById('statusText');
            
            if (dot) {
                if (connected) {
                    dot.classList.add('connected');
                } else {
                    dot.classList.remove('connected');
                }
            }
            
            if (text) {
                text.textContent = connected ? 'Connected' : 'Disconnected';
            }
        },
        
        switchTab(tabName) {
            console.log('Switching to tab:', tabName);
            
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            if (event && event.target) {
                event.target.classList.add('active');
            }
            
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            const tabContent = document.getElementById(tabName + 'Tab');
            if (tabContent) {
                tabContent.classList.add('active');
            }
        },
        
        // File Manager
        async loadFiles(path) {
            // Check if a server is selected first
            const config = await window.go.main.App.GetConfig();
            if (!config.serverID) {
                const tree = document.getElementById('fileTree');
                if (tree) {
                    tree.innerHTML = '<div class="preview-empty">Please select a server from the dropdown above</div>';
                }
                return;
            }
            
            console.log('Loading files from:', path);
            this.currentPath = path;
            
            const pathInput = document.getElementById('currentPath');
            if (pathInput) pathInput.value = path;
            
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            
            tree.innerHTML = '<div class="loading">Loading files...</div>';
            
            try {
                const files = await window.go.main.App.ListFiles(path);
                console.log('Files loaded:', files);
                this.renderFiles(files || []);
            } catch (err) {
                console.error('Failed to load files:', err);
                tree.innerHTML = '<div class="error">Failed to load files: ' + err + '</div>';
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
            
            if (files.length === 0 && this.currentPath === '/') {
                tree.innerHTML = '<div class="preview-empty">No files found</div>';
            }
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
            
            div.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                if (!isParent) {
                    this.selectedFile = file;
                    this.showContextMenu(e, file);
                }
            });
            
            return div;
        },
        
        getFileIcon(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            const icons = {
                js: '📜', py: '🐍', php: '🐘', html: '🌐', css: '🎨',
                json: '📋', xml: '📄', md: '📝', txt: '📄', log: '📊',
                jpg: '🖼️', png: '🖼️', gif: '🖼️', svg: '🖼️',
                zip: '📦', tar: '📦', gz: '📦',
            };
            return icons[ext] || '📄';
        },
        
        // Editor functions
        async openFile(file) {
            if (!file || file.isDir) return;
            
            // Auto-save current file if enabled
            if (this.autoSave && this.activeFile && this.isFileModified(this.activeFile)) {
                await this.saveFile();
            }
            
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
                
                // Add to open files
                this.openFiles.set(filePath, {
                    name: file.name,
                    content: content,
                    originalContent: content,
                    modified: false
                });
                
                // Add tab
                this.addEditorTab(filePath, file.name);
                
                // Switch to file
                this.switchToFile(filePath);
                
                // Update file type
                this.updateFileType(file.name);
                
            } catch (err) {
                alert('Failed to open file: ' + err);
            }
        },
        
        openInNewTab() {
            if (this.selectedFile && !this.selectedFile.isDir) {
                this.openFile(this.selectedFile);
            }
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
            
            // Show editor
            if (!this.editor.textarea) {
                this.editor.create();
            }
            this.editor.setValue(file.content);
            
            // Show editor buttons
            document.getElementById('saveBtn').style.display = '';
            document.getElementById('saveAllBtn').style.display = '';
            document.getElementById('closeBtn').style.display = '';
            
            // Focus editor
            this.editor.focus();
        },
        
        onEditorChange(content) {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (file) {
                file.content = content;
                file.modified = content !== file.originalContent;
                this.updateTabModified(this.activeFile, file.modified);
            }
        },
        
        updateTabModified(path, modified) {
            const tab = document.querySelector(`.editor-tab[data-path="${path}"]`);
            if (tab) {
                if (modified) {
                    tab.classList.add('modified');
                } else {
                    tab.classList.remove('modified');
                }
            }
        },
        
        isFileModified(path) {
            const file = this.openFiles.get(path);
            return file && file.modified;
        },
        
        async saveFile() {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (!file || !file.modified) return;
            
            try {
                await window.go.main.App.SaveFileContent(this.activeFile, file.content);
                file.originalContent = file.content;
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
                    try {
                        await window.go.main.App.SaveFileContent(path, file.content);
                        file.originalContent = file.content;
                        file.modified = false;
                        this.updateTabModified(path, false);
                    } catch (err) {
                        console.error('Failed to save file:', path, err);
                    }
                }
            }
            console.log('All files saved');
        },
        
        async closeFile() {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (file && file.modified) {
                if (confirm('Save changes to ' + file.name + '?')) {
                    await this.saveFile();
                }
            }
            
            this.closeFileTab(this.activeFile);
        },
        
        closeFileTab(path) {
            // Remove from open files
            this.openFiles.delete(path);
            
            // Remove tab
            const tab = document.querySelector(`.editor-tab[data-path="${path}"]`);
            if (tab) tab.remove();
            
            // If this was the active file
            if (this.activeFile === path) {
                this.activeFile = null;
                
                // Check if we're closing the currently selected file in the tree
                // Extract just the filename from the path for comparison
                const fileName = path.split('/').pop();
                if (this.selectedFile && this.selectedFile.name === fileName) {
                    // Deselect the file in the tree
                    document.querySelectorAll('.file-item').forEach(item => {
                        item.classList.remove('selected');
                    });
                    this.selectedFile = null;
                }
                
                // Switch to another open file or clear editor
                if (this.openFiles.size > 0) {
                    const nextFile = this.openFiles.keys().next().value;
                    this.switchToFile(nextFile);
                } else {
                    // Clear editor content but preserve the editor instance
                    if (this.editor) {
                        if (!this.editor.textarea) {
                            // If editor wasn't created yet, create it
                            this.editor.create();
                        }
                        // Clear the editor content and show placeholder
                        this.editor.setValue('// Select a file from the file tree to edit\n');
                    }
                    
                    // Hide editor buttons
                    document.getElementById('saveBtn').style.display = 'none';
                    document.getElementById('saveAllBtn').style.display = 'none';
                    document.getElementById('closeBtn').style.display = 'none';
                    
                    // Update file type to show no file
                    const typeEl = document.getElementById('fileType');
                    if (typeEl) {
                        typeEl.textContent = 'No file open';
                    }
                }
            }
        },
        
        async newFile() {
            const name = prompt('File name:');
            if (!name) return;
            
            const path = this.currentPath === '/' ? '/' + name : this.currentPath + '/' + name;
            
            try {
                await window.go.main.App.SaveFileContent(path, '');
                await this.refreshFiles();
                
                // Open the new file
                this.openFile({ name: name, isDir: false, size: 0 });
            } catch (err) {
                alert('Failed to create file: ' + err);
            }
        },
        
        updateFileType(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            const types = {
                js: 'JavaScript', py: 'Python', php: 'PHP',
                html: 'HTML', css: 'CSS', json: 'JSON',
                xml: 'XML', md: 'Markdown', txt: 'Plain Text',
                log: 'Log File', yml: 'YAML', yaml: 'YAML'
            };
            
            const typeEl = document.getElementById('fileType');
            if (typeEl) {
                typeEl.textContent = types[ext] || 'Plain Text';
            }
        },
        
        // Editor settings
        toggleAutoSave() {
            this.autoSave = document.getElementById('autoSave').checked;
            localStorage.setItem('autoSave', this.autoSave);
            this.updateAutoSaveStatus();
        },
        
        toggleWordWrap() {
            const enabled = document.getElementById('wordWrap').checked;
            localStorage.setItem('wordWrap', enabled);
            if (this.editor) {
                this.editor.setWordWrap(enabled);
            }
        },
        
        toggleLineNumbers() {
            const enabled = document.getElementById('showLineNumbers').checked;
            localStorage.setItem('showLineNumbers', enabled);
            if (this.editor) {
                this.editor.setShowLineNumbers(enabled);
            }
        },
        
        updateAutoSaveStatus() {
            const statusEl = document.getElementById('autoSaveStatus');
            if (statusEl) {
                statusEl.textContent = 'Auto-save: ' + (this.autoSave ? 'On' : 'Off');
            }
        },
        
        // File operations
        async refreshFiles() {
            await this.loadFiles(this.currentPath);
        },
        
        uploadFile() {
            const input = document.getElementById('fileInput');
            if (input) input.click();
        },
        
        async handleFileUpload(files) {
            for (const file of files) {
                try {
                    const buffer = await file.arrayBuffer();
                    const path = this.currentPath === '/' 
                        ? '/' + file.name 
                        : this.currentPath + '/' + file.name;
                    
                    await window.go.main.App.UploadFile(path, Array.from(new Uint8Array(buffer)));
                    console.log('Uploaded:', file.name);
                } catch (err) {
                    alert('Failed to upload ' + file.name + ': ' + err);
                }
            }
            await this.refreshFiles();
        },
        
        async newFolder() {
            const name = prompt('Folder name:');
            if (!name) return;
            
            try {
                const path = this.currentPath === '/' 
                    ? '/' + name 
                    : this.currentPath + '/' + name;
                await window.go.main.App.CreateFolder(path);
                await this.refreshFiles();
            } catch (err) {
                alert('Failed to create folder: ' + err);
            }
        },
        
        async deleteSelected() {
            if (!this.selectedFile) {
                alert('No file selected');
                return;
            }
            
            if (!confirm('Delete ' + this.selectedFile.name + '?')) return;
            
            try {
                const path = this.currentPath === '/' 
                    ? '/' + this.selectedFile.name 
                    : this.currentPath + '/' + this.selectedFile.name;
                await window.go.main.App.DeleteFiles([path]);
                await this.refreshFiles();
            } catch (err) {
                alert('Failed to delete: ' + err);
            }
        },
        
        async deleteFile() {
            await this.deleteSelected();
        },
        
        renameFile() {
            if (!this.selectedFile) return;
            
            const newName = prompt('New name:', this.selectedFile.name);
            if (!newName || newName === this.selectedFile.name) return;
            
            // TODO: Implement rename
            alert('Rename not implemented yet');
        },
        
        duplicateFile() {
            alert('Duplicate not implemented yet');
        },
        
        copyPath() {
            if (!this.selectedFile) return;
            
            const path = this.currentPath === '/' 
                ? '/' + this.selectedFile.name 
                : this.currentPath + '/' + this.selectedFile.name;
            
            navigator.clipboard.writeText(path);
            console.log('Path copied:', path);
        },
        
        downloadFile() {
            alert('Download not implemented yet');
        },
        
        showContextMenu(event, file) {
            const menu = document.getElementById('contextMenu');
            if (!menu) return;
            
            menu.style.left = event.clientX + 'px';
            menu.style.top = event.clientY + 'px';
            menu.classList.add('show');
            this.contextFile = file;
        },
        
        // Console operations
        appendConsole(message, type = '') {
            const consoleEl = document.getElementById('console');
            if (!consoleEl) return;
            
            const line = document.createElement('div');
            line.className = 'console-line';
            if (type) line.classList.add(type);
            
            // Convert ANSI codes to HTML for colored output
            line.innerHTML = this.ansiToHtml(message);
            
            consoleEl.appendChild(line);
            consoleEl.scrollTop = consoleEl.scrollHeight;
            
            while (consoleEl.children.length > 1000) {
                consoleEl.removeChild(consoleEl.firstChild);
            }
        },
        
        ansiToHtml(text) {
            // Handle various ANSI escape sequences
            let html = this.escapeHtmlForConsole(text);
            
            // Process ANSI color codes
            const ansiPattern = /\x1b\[([0-9;]*)m/g;
            let result = '';
            let lastIndex = 0;
            let currentStyle = { fg: null, bg: null, bold: false };
            let openSpan = false;
            
            let match;
            while ((match = ansiPattern.exec(html)) !== null) {
                // Add text before the ANSI code
                if (match.index > lastIndex) {
                    result += html.substring(lastIndex, match.index);
                }
                
                // Parse ANSI codes
                const codes = match[1].split(';').filter(Boolean).map(Number);
                
                // Close existing span if open
                if (openSpan) {
                    result += '</span>';
                    openSpan = false;
                }
                
                // Process each code
                for (const code of codes) {
                    if (code === 0) { // Reset
                        currentStyle = { fg: null, bg: null, bold: false };
                    } else if (code === 1) { // Bold
                        currentStyle.bold = true;
                    } else if (code === 22) { // Not bold
                        currentStyle.bold = false;
                    } else if (code >= 30 && code <= 37) { // Foreground color
                        currentStyle.fg = code;
                    } else if (code === 39) { // Default foreground
                        currentStyle.fg = null;
                    } else if (code >= 40 && code <= 47) { // Background color
                        currentStyle.bg = code;
                    } else if (code === 49) { // Default background
                        currentStyle.bg = null;
                    } else if (code >= 90 && code <= 97) { // Bright foreground
                        currentStyle.fg = code;
                    } else if (code >= 100 && code <= 107) { // Bright background
                        currentStyle.bg = code;
                    }
                }
                
                // Build style string
                if (currentStyle.fg || currentStyle.bg || currentStyle.bold) {
                    let styleStr = 'style="';
                    if (currentStyle.bold) styleStr += 'font-weight:bold;';
                    
                    // Map ANSI colors to CSS colors
                    const colorMap = {
                        30: '#1d1d1d', 31: '#f44747', 32: '#608b4e', 33: '#dcdcaa',
                        34: '#569cd6', 35: '#c678dd', 36: '#56b6c2', 37: '#d4d4d4',
                        90: '#666666', 91: '#ff7b72', 92: '#7ec16e', 93: '#f9c513',
                        94: '#79b8ff', 95: '#e2b4f4', 96: '#8cc4d6', 97: '#ffffff'
                    };
                    
                    if (currentStyle.fg && colorMap[currentStyle.fg]) {
                        styleStr += `color:${colorMap[currentStyle.fg]};`;
                    }
                    
                    const bgColorMap = {
                        40: '#1d1d1d', 41: '#f44747', 42: '#608b4e', 43: '#dcdcaa',
                        44: '#569cd6', 45: '#c678dd', 46: '#56b6c2', 47: '#d4d4d4',
                        100: '#666666', 101: '#ff7b72', 102: '#7ec16e', 103: '#f9c513',
                        104: '#79b8ff', 105: '#e2b4f4', 106: '#8cc4d6', 107: '#ffffff'
                    };
                    
                    if (currentStyle.bg && bgColorMap[currentStyle.bg]) {
                        styleStr += `background-color:${bgColorMap[currentStyle.bg]};`;
                    }
                    
                    styleStr += '"';
                    result += `<span ${styleStr}>`;
                    openSpan = true;
                }
                
                lastIndex = match.index + match[0].length;
            }
            
            // Add remaining text
            if (lastIndex < html.length) {
                result += html.substring(lastIndex);
            }
            
            // Close any open span
            if (openSpan) {
                result += '</span>';
            }
            
            // Also handle some common terminal control sequences
            result = result
                .replace(/\x1b\[2K/g, '') // Clear line
                .replace(/\x1b\[K/g, '')  // Clear to end of line
                .replace(/\x1b\[H/g, '')  // Cursor home
                .replace(/\x1b\[\d+;\d+H/g, '') // Cursor position
                .replace(/\x1b\[\d+[ABCD]/g, '') // Cursor movement
                .replace(/\x1b\[[\d;]*[HfJ]/g, '') // Various other codes
                .replace(/\r\n/g, '\n')   // Normalize line endings
                .replace(/\r/g, '\n');
            
            return result || this.escapeHtmlForConsole(text);
        },
        
        escapeHtmlForConsole(text) {
            // Special HTML escape for console output
            return text
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#039;');
        },
        
        clearConsole() {
            const consoleEl = document.getElementById('console');
            if (consoleEl) {
                consoleEl.innerHTML = '';
                this.appendConsole('Console cleared', 'info');
            }
        },
        
        async connectConsole() {
            try {
                if (this.consoleConnected) {
                    await window.go.main.App.DisconnectConsole();
                    this.consoleConnected = false;
                    document.getElementById('connectBtn').textContent = 'Connect';
                } else {
                    console.log('Connecting to console...');
                    await window.go.main.App.ConnectConsole();
                }
            } catch (err) {
                console.error('Console connection failed:', err);
                alert('Failed to connect console: ' + err);
            }
        },
        
        async sendCommand() {
            const input = document.getElementById('commandInput');
            if (!input) return;
            
            const command = input.value.trim();
            if (!command) return;
            
            this.appendConsole('> ' + command, 'command');
            input.value = '';
            
            try {
                await window.go.main.App.SendCommand(command);
            } catch (err) {
                this.appendConsole('Failed to send command: ' + err, 'error');
            }
        },
        
        async sendPower(signal) {
            try {
                console.log('Sending power signal:', signal);
                await window.go.main.App.SetPowerState(signal);
                this.appendConsole('Power signal sent: ' + signal, 'info');
            } catch (err) {
                console.error('Power signal failed:', err);
                alert('Failed to send power signal: ' + err);
            }
        },
        
        // Settings
        showSettings() {
            const modal = document.getElementById('settingsModal');
            if (modal) {
                modal.classList.add('show');
                
                window.go.main.App.GetConfig().then(config => {
                    const urlInput = document.getElementById('panelUrl');
                    const keyInput = document.getElementById('apiKey');
                    const idInput = document.getElementById('serverId');
                    
                    if (urlInput) urlInput.value = config.panelURL || '';
                    if (keyInput) keyInput.value = config.apiKey || '';
                    if (idInput) idInput.value = config.serverID || '';
                });
            }
        },
        
        closeSettings() {
            const modal = document.getElementById('settingsModal');
            if (modal) modal.classList.remove('show');
        },
        
        async saveSettings() {
            const panelURL = document.getElementById('panelUrl')?.value;
            const apiKey = document.getElementById('apiKey')?.value;
            const serverID = document.getElementById('serverId')?.value;
            
            if (!panelURL || !apiKey) {
                alert('Panel URL and API Key are required');
                return;
            }
            
            try {
                console.log('Saving settings...');
                await window.go.main.App.SaveConfig(panelURL, apiKey, serverID || '');
                this.closeSettings();
                await this.connect();
                // Load servers after connecting
                await this.loadServers();
            } catch (err) {
                console.error('Save settings failed:', err);
                alert('Failed to save settings: ' + err);
            }
        },
        
        // Server management
        async loadServers() {
            try {
                const servers = await window.go.main.App.ListServers();
                console.log('Servers loaded:', servers);
                
                const dropdown = document.getElementById('serverDropdown');
                if (!dropdown) return;
                
                // Get current server ID from config
                const config = await window.go.main.App.GetConfig();
                const currentServerID = config.serverID;
                
                // Clear existing options except the placeholder
                dropdown.innerHTML = '<option value="" disabled>Select Server</option>';
                
                // Add server options
                servers.forEach(server => {
                    const option = document.createElement('option');
                    option.value = server.id;
                    option.textContent = server.name + (server.description ? ' - ' + server.description : '');
                    option.selected = server.id === currentServerID;
                    dropdown.appendChild(option);
                });
                
                // If no server is selected and we have servers, select the first one
                if (!currentServerID && servers.length > 0) {
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
                console.log('Switching to server:', serverID);
                await window.go.main.App.SwitchServer(serverID);
                
                // Update dropdown selection
                const dropdown = document.getElementById('serverDropdown');
                if (dropdown) {
                    dropdown.value = serverID;
                }
            } catch (err) {
                console.error('Failed to switch server:', err);
                alert('Failed to switch server: ' + err);
            }
        },
        
        closeAllFiles() {
            // Close all open files
            const paths = Array.from(this.openFiles.keys());
            paths.forEach(path => {
                this.openFiles.delete(path);
                const tab = document.querySelector(`.editor-tab[data-path="${path}"]`);
                if (tab) tab.remove();
            });
            
            this.activeFile = null;
            this.selectedFile = null;
            
            // Clear selection in file tree
            document.querySelectorAll('.file-item').forEach(item => {
                item.classList.remove('selected');
            });
            
            // Clear editor
            if (this.editor) {
                if (!this.editor.textarea) {
                    this.editor.create();
                }
                this.editor.setValue('// Select a file from the file tree to edit\n');
            }
            
            // Hide editor buttons
            document.getElementById('saveBtn').style.display = 'none';
            document.getElementById('saveAllBtn').style.display = 'none';
            document.getElementById('closeBtn').style.display = 'none';
            
            // Update file type
            const typeEl = document.getElementById('fileType');
            if (typeEl) {
                typeEl.textContent = 'No file open';
            }
        },
        
        // Utilities
        formatSize(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
        },
        
        escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
    };
    
    // Set global app reference
    window.app = app;
    
    // Start the app
    app.init();
    
    console.log('Enhanced app initialized successfully');
}

// Start initialization when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForRuntime);
} else {
    waitForRuntime();
}
