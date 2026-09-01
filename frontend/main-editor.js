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
        // selectedFile carries the full remote path it was rendered with, so a
        // later navigation cannot repoint a stale selection at a same-named
        // file in another directory. Deleting used to rebuild the path from
        // currentPath at click time, which did exactly that.
        selectedFile: null,
        contextFile: null,
        isConnected: false,
        consoleConnected: false,
        // Set while the panel form is editing an existing panel rather than
        // adding one; blank key fields then mean "keep what is stored".
        editingPanel: null,
        
        // Navigation history
        navigationHistory: [],
        navigationIndex: -1,
        isNavigating: false,
        
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
            this.setupMouseNavigation();
            await this.checkConfig();
        },
        
        
        setupMouseNavigation() {
            // Mouse button 4 (back) and 5 (forward) navigation
            document.addEventListener('mousedown', (e) => {
                // Check if we're in the file tree area
                const fileTree = document.getElementById('fileTree');
                const isInFileArea = fileTree && (fileTree.contains(e.target) || 
                                    e.target.closest('.file-manager') || 
                                    e.target.closest('.toolbar'));
                
                if (isInFileArea) {
                    if (e.button === 3) { // Mouse button 4 (back)
                        e.preventDefault();
                        this.navigateBack();
                    } else if (e.button === 4) { // Mouse button 5 (forward)
                        e.preventDefault();
                        this.navigateForward();
                    }
                }
            });
            
            // Prevent context menu on mouse buttons 4 and 5
            document.addEventListener('auxclick', (e) => {
                if (e.button === 1 || e.button === 2) return; // Allow middle and right click
                
                const fileTree = document.getElementById('fileTree');
                const isInFileArea = fileTree && (fileTree.contains(e.target) || 
                                    e.target.closest('.file-manager') || 
                                    e.target.closest('.toolbar'));
                
                if (isInFileArea && (e.button === 3 || e.button === 4)) {
                    e.preventDefault();
                }
            });
        },
        
        navigateBack() {
            if (this.navigationIndex > 0) {
                this.navigationIndex--;
                this.isNavigating = true;
                const targetPath = this.navigationHistory[this.navigationIndex];
                console.log('Navigating back to:', targetPath);
                this.loadFiles(targetPath);
            }
        },
        
        navigateForward() {
            if (this.navigationIndex < this.navigationHistory.length - 1) {
                this.navigationIndex++;
                this.isNavigating = true;
                const targetPath = this.navigationHistory[this.navigationIndex];
                console.log('Navigating forward to:', targetPath);
                this.loadFiles(targetPath);
            }
        },
        
        addToHistory(path) {
            // Don't add to history if we're navigating via history
            if (this.isNavigating) {
                this.isNavigating = false;
                return;
            }
            
            // If we're not at the end of history, remove everything after current index
            if (this.navigationIndex < this.navigationHistory.length - 1) {
                this.navigationHistory = this.navigationHistory.slice(0, this.navigationIndex + 1);
            }
            
            // Don't add duplicate of current path
            if (this.navigationHistory[this.navigationIndex] === path) {
                return;
            }
            
            // Add new path to history
            this.navigationHistory.push(path);
            this.navigationIndex = this.navigationHistory.length - 1;
            
            // Limit history to 50 items
            if (this.navigationHistory.length > 50) {
                this.navigationHistory.shift();
                this.navigationIndex--;
            }
            
            console.log('Navigation history:', this.navigationHistory, 'Index:', this.navigationIndex);
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
                // Alt+Left - Navigate back
                if (e.altKey && e.key === 'ArrowLeft') {
                    e.preventDefault();
                    this.navigateBack();
                }
                // Alt+Right - Navigate forward
                if (e.altKey && e.key === 'ArrowRight') {
                    e.preventDefault();
                    this.navigateForward();
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
                // Reset path and history when server changes
                this.currentPath = '/';
                this.navigationHistory = ['/'];
                this.navigationIndex = 0;
                this.appendConsole('=== Switched to server: ' + serverID + ' ===', 'info');
                // Clear console and reload files for the new server
                this.clearConsole();
                this.loadFiles('/');
                // Close all open files as they belong to the previous server
                this.closeAllFiles();
            });
            
            window.runtime.EventsOn('panel-changed', (panelName) => {
                console.log('Panel changed to:', panelName);
                // Reset path and history when panel changes
                this.currentPath = '/';
                this.navigationHistory = ['/'];
                this.navigationIndex = 0;
                this.appendConsole('=== Switched to panel: ' + panelName + ' ===', 'info');
                // Reload files for the new panel
                this.loadFiles('/');
                // Close all open files as they belong to the previous panel
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
                // Load panels first
                await this.loadPanels();
                
                const panels = await window.go.main.App.ListPanels();
                const config = await window.go.main.App.GetConfig();
                console.log('Config loaded:', config);
                
                if (config && config.panelURL && config.apiKey) {
                    // Connect even without server ID (we'll select from dropdown)
                    await this.connect();
                    // Load servers after connecting
                    await this.loadServers();
                } else if (panels.length === 0) {
                    // No panels configured, show panel manager
                    this.showPanelManager();
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
                this.showPanelManager();
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
            // The shell owns tab state: it drives the rail's active marker and
            // emits `tab:show`, which is what the data tabs load on. Falls back
            // to the old direct DOM swap if the shell has not booted yet.
            if (window.Shell && window.Shell.showTab) {
                window.Shell.showTab(tabName);
                return;
            }

            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.toggle('active', tab.dataset.tab === tabName);
            });
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
            // Ensure path is never undefined
            if (path === undefined || path === null || path === '') {
                path = '/';
            }
            this.currentPath = path;

            // Leaving a directory drops the selection with it. Keeping it is
            // how a delete aimed at /a/config.yml lands on /b/config.yml.
            this.clearSelection();

            // Add to navigation history
            this.addToHistory(path);
            
            const pathInput = document.getElementById('currentPath');
            if (pathInput) pathInput.value = path;

            // The visible path is the breadcrumb bar in the shell; the input
            // above is kept as the value other code still reads.
            document.dispatchEvent(new CustomEvent('path:changed', { detail: path }));
            
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

            // Resolved once, at render time, and carried on the row. Every
            // action on this row uses it instead of recomputing from
            // currentPath, which may have moved on by then.
            const fullPath = isParent
                ? null
                : (this.currentPath === '/' ? '/' + file.name : this.currentPath + '/' + file.name);
            const entry = isParent ? null : Object.assign({}, file, { path: fullPath });
            div.dataset.path = fullPath || '';
            
            const icon = document.createElement('span');
            const kind = window.Icons ? window.Icons.kindFor(file.name, file.isDir) : 'file';
            icon.className = 'file-icon kind-' + kind;
            icon.innerHTML = this.getFileIcon(file.name, file.isDir);
            
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
                    this.loadFiles(fullPath);
                } else {
                    this.markSelected(div, entry);
                    this.openFile(entry);
                }
            });
            
            div.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                if (!isParent) {
                    this.markSelected(div, entry);
                    this.showContextMenu(e, entry);
                }
            });
            
            return div;
        },
        
        markSelected(row, entry) {
            document.querySelectorAll('.file-item').forEach(item => {
                item.classList.remove('selected');
            });
            if (row) row.classList.add('selected');
            this.selectedFile = entry;
            this.contextFile = entry;
        },

        clearSelection() {
            this.selectedFile = null;
            this.contextFile = null;
            document.querySelectorAll('.file-item.selected').forEach(item => {
                item.classList.remove('selected');
            });
        },

        // Every path the UI hands to the backend goes through here first. The
        // backend re-validates, but a bad path caught in the editor produces a
        // better message than a rejected API call.
        resolvePath(entry) {
            if (!entry) return null;
            if (entry.path) return entry.path;
            if (!entry.name) return null;
            return this.currentPath === '/' ? '/' + entry.name : this.currentPath + '/' + entry.name;
        },

        say(title, message) {
            if (window.Shell && window.Shell.dialog) {
                return window.Shell.dialog.confirm(title, this.escapeHtml(message), { confirmLabel: 'OK' });
            }
            alert(title + '\n\n' + message);
            return Promise.resolve(true);
        },

        ask(title, message, opts) {
            if (window.Shell && window.Shell.dialog) {
                return window.Shell.dialog.confirm(title, message, opts);
            }
            return Promise.resolve(confirm(title));
        },

        // Returns SVG markup from the shared sprite (see icons.js). Kept as
        // getFileIcon so existing callers do not change; the emoji map it
        // replaced could not be recoloured or sized.
        getFileIcon(filename, isDir = false) {
            if (window.Icons) return window.Icons.forFile(filename, isDir);
            return '';
        },
        
        // Editor functions
        async openFile(file) {
            if (!file || file.isDir) return;

            // Auto-save current file if enabled
            if (this.autoSave && this.activeFile && this.isFileModified(this.activeFile)) {
                await this.saveFile();
            }

            const filePath = this.resolvePath(file);
            if (!filePath) return;

            // The split editor browses and loads its own files on each side
            // (see split-view.js), so opening from this tree always targets
            // the main editor.

            // Check if already open
            if (this.openFiles.has(filePath)) {
                this.switchToFile(filePath);
                return;
            }

            try {
                // ReadFileForEdit refuses the two cases where a later save
                // would corrupt the file: content that is not valid UTF-8, and
                // a file too big to hold in the editor intact. Opening either
                // and saving it back writes a mangled copy over the real one.
                const read = await window.go.main.App.ReadFileForEdit('', filePath);

                if (read.too_big) {
                    await this.say('Too large to edit',
                        file.name + ' is ' + this.formatSize(read.size) +
                        '. The editor caps files at 8 MB so a save cannot truncate one. ' +
                        'Use the panel or SFTP for this file.');
                    return;
                }
                if (read.binary) {
                    await this.say('Not a text file',
                        file.name + ' is not valid UTF-8, so it is a binary file. ' +
                        'Editing it here would corrupt it on save.');
                    return;
                }

                // Add to open files
                this.openFiles.set(filePath, {
                    name: file.name,
                    path: filePath,
                    content: read.content,
                    originalContent: read.content,
                    modified: false
                });

                // Add tab
                this.addEditorTab(filePath, file.name);

                // Switch to file
                this.switchToFile(filePath);

                // Update file type
                this.updateFileType(file.name);

            } catch (err) {
                await this.say('Failed to open file', String(err));
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
            document.getElementById('historyBtn').style.display = '';
            
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

            return this.writeFile(this.activeFile, file, false);
        },

        /**
         * The single write path for the main editor.
         *
         * The backend copies the remote bytes locally before replacing them,
         * and refuses the write outright when the panel's copy no longer
         * matches what this editor loaded - someone editing the same file in
         * the panel, or a plugin rewriting its own config, would otherwise be
         * silently overwritten. `force` answers that prompt; the local copy is
         * still taken, so even a forced overwrite is recoverable from Vault.
         */
        async writeFile(path, file, force) {
            try {
                const res = await window.go.main.App.SafeSaveFileContent(
                    path, file.content, file.originalContent, !!force);

                if (res && res.conflict) {
                    // Show the change rather than describing it: deciding
                    // whether to overwrite needs to know what would be lost.
                    let diff = '';
                    if (res.remote_content && window.Vault && window.Vault.diffHtml) {
                        diff = '<div style="margin-top:12px">' +
                            window.Vault.diffHtml(res.remote_content, file.content) + '</div>';
                    }

                    const ok = await this.ask('The panel copy changed',
                        '<b>' + this.escapeHtml(path) + '</b> ' + this.escapeHtml(res.reason) + '.' +
                        '<br><br>Saving now replaces what is on the panel with what is in this editor. ' +
                        'The panel copy is filed in the local history first, so it can be restored from Vault.' +
                        diff,
                        { danger: true, confirmLabel: 'Overwrite anyway' });
                    if (!ok) return false;
                    return this.writeFile(path, file, true);
                }

                file.originalContent = file.content;
                file.modified = false;
                this.updateTabModified(path, false);
                console.log('File saved:', path, res && res.version_id ? '(version ' + res.version_id + ')' : '');
                return true;
            } catch (err) {
                await this.say('Failed to save file', String(err));
                return false;
            }
        },

        async saveAllFiles() {
            for (const [path, file] of this.openFiles) {
                if (file.modified) {
                    await this.writeFile(path, file, false);
                }
            }
            console.log('Save all finished');
        },
        
        async closeFile() {
            if (!this.activeFile) return;
            
            const file = this.openFiles.get(this.activeFile);
            if (file && file.modified) {
                const save = await this.ask('Unsaved changes',
                    'Save changes to <b>' + this.escapeHtml(file.name) + '</b> before closing?',
                    { confirmLabel: 'Save and close' });
                if (save) {
                    const written = await this.saveFile();
                    // A refused or failed save must not close the tab; that
                    // would drop the edits with no way back.
                    if (written === false) return;
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
                    document.getElementById('historyBtn').style.display = 'none';

                    // Update file type to show no file
                    const typeEl = document.getElementById('fileType');
                    if (typeEl) {
                        typeEl.textContent = 'No file open';
                    }
                }
            }
        },
        
        async newFile() {
            const v = await window.Shell.dialog.form('New file', [
                { name: 'name', label: 'File name', placeholder: 'config.yml', mono: true }
            ], { confirmLabel: 'Create' });
            if (!v || !v.name || !v.name.trim()) return;

            const name = v.name.trim();
            const path = this.currentPath === '/' ? '/' + name : this.currentPath + '/' + name;

            try {
                // CreateFileStrict refuses a name that is already taken. The
                // old path wrote an empty string to it, which truncated any
                // existing file of that name to nothing.
                await window.go.main.App.CreateFileStrict(path);
                await this.refreshFiles();

                // Open the new file
                await this.openFile({ name: name, isDir: false, size: 0, path: path });
            } catch (err) {
                await this.say('Could not create the file', String(err));
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
                const path = this.currentPath === '/'
                    ? '/' + file.name
                    : this.currentPath + '/' + file.name;

                let bytes;
                try {
                    bytes = Array.from(new Uint8Array(await file.arrayBuffer()));
                } catch (err) {
                    await this.say('Could not read ' + file.name, String(err));
                    continue;
                }

                try {
                    // overwrite is false, so an upload that would land on an
                    // existing name is refused rather than performed.
                    await window.go.main.App.UploadFileSafe(path, bytes, false);
                    console.log('Uploaded:', file.name);
                } catch (err) {
                    const message = String(err);
                    if (message.indexOf('already exists') === -1) {
                        await this.say('Failed to upload ' + file.name, message);
                        continue;
                    }
                    const ok = await this.ask('Replace ' + this.escapeHtml(file.name) + '?',
                        'A file with that name is already in <span class="mono">' + this.escapeHtml(this.currentPath) + '</span>. ' +
                        'Replacing it puts the current file in the local recycle bin first.',
                        { danger: true, confirmLabel: 'Replace' });
                    if (!ok) continue;
                    try {
                        await window.go.main.App.UploadFileSafe(path, bytes, true);
                    } catch (err2) {
                        await this.say('Failed to upload ' + file.name, String(err2));
                    }
                }
            }
            await this.refreshFiles();
        },
        
        async newFolder() {
            const v = await window.Shell.dialog.form('New folder', [
                { name: 'name', label: 'Folder name', placeholder: 'plugins', mono: true }
            ], { confirmLabel: 'Create' });
            if (!v || !v.name || !v.name.trim()) return;

            const name = v.name.trim();
            try {
                const path = this.currentPath === '/'
                    ? '/' + name
                    : this.currentPath + '/' + name;
                await window.go.main.App.CreateFolder(path);
                await this.refreshFiles();
            } catch (err) {
                await this.say('Could not create the folder', String(err));
            }
        },
        
        async deleteSelected() {
            const path = this.resolvePath(this.selectedFile);
            if (!path) {
                await this.say('Nothing selected', 'Pick a file or folder in the tree first.');
                return;
            }
            await this.deletePaths([path]);
        },

        async deleteFile() {
            await this.deleteSelected();
        },

        /**
         * Deleting is a handshake, not a click.
         *
         * PlanDelete walks the selection on the panel and returns exactly what
         * would go, plus a token bound to that path set. Nothing is removed
         * until SafeDeleteFiles is called with that token, so a stale
         * selection, a double click or a re-render cannot delete anything the
         * dialog did not list. Everything the plan captures lands in the local
         * recycle bin first.
         */
        async deletePaths(paths) {
            if (!paths || !paths.length) return;

            let plan;
            try {
                plan = await window.go.main.App.PlanDelete('', paths);
            } catch (err) {
                await this.say('Cannot delete', String(err));
                return;
            }

            const intro = this.renderDeletePlan(plan);
            // Anything recursive, anything the backend flagged as critical, and
            // anything that will not fit in the recycle bin has to be typed out.
            const strict = (plan.critical && plan.critical.length > 0) ||
                !plan.recoverable || plan.dir_count > 0;

            if (strict) {
                const v = await window.Shell.dialog.form('Delete ' + plan.roots.length + ' item(s)', [
                    { name: 'confirm', label: 'Type DELETE to confirm', placeholder: 'DELETE', mono: true }
                ], { confirmLabel: 'Delete', danger: true, intro: intro });
                if (!v) return;
                if (String(v.confirm || '').trim().toUpperCase() !== 'DELETE') {
                    await this.say('Nothing deleted', 'The confirmation did not match, so nothing was removed.');
                    return;
                }
            } else {
                const ok = await window.Shell.dialog.open({
                    title: 'Delete ' + plan.roots.length + ' item(s)',
                    body: intro,
                    confirmLabel: 'Delete',
                    danger: true
                });
                if (!ok) return;
            }

            try {
                const outcome = await window.go.main.App.SafeDeleteFiles(plan.token);
                this.forgetOpenFilesUnder(plan.roots);
                await this.refreshFiles();

                if (outcome.skipped && outcome.skipped.length) {
                    await this.say('Deleted, with gaps',
                        outcome.captured + ' file(s) went to the recycle bin. ' +
                        outcome.skipped.length + ' could not be copied first and are gone for good:\n' +
                        outcome.skipped.join('\n'));
                } else if (outcome.captured > 0) {
                    // Undo offered where the mistake is noticed: right after it.
                    const undo = await this.ask('Deleted',
                        outcome.captured + ' file(s) went to the recycle bin.',
                        { confirmLabel: 'Undo' });
                    if (undo && outcome.batch) {
                        try {
                            const back = await window.go.main.App.RestoreBinBatch(outcome.batch, false);
                            await this.refreshFiles();
                            await this.say('Undone', (back.restored || []).length + ' file(s) put back.');
                        } catch (err) {
                            await this.say('Could not undo', String(err));
                        }
                    }
                }
            } catch (err) {
                await this.say('Delete failed', String(err));
            }
        },

        renderDeletePlan(plan) {
            const esc = (v) => this.escapeHtml(v);
            const size = (v) => window.Shell.fmt.bytes(v);

            let html = '<div style="font-size:12.5px;line-height:1.6;color:var(--text-secondary)">';

            html += '<p><b>' + plan.file_count + '</b> file' + (plan.file_count === 1 ? '' : 's');
            if (plan.dir_count) html += ' and <b>' + plan.dir_count + '</b> folder' + (plan.dir_count === 1 ? '' : 's');
            html += ', ' + size(plan.total_bytes) + ' in total.</p>';

            const listed = plan.items.slice(0, 40);
            html += '<div class="mono" style="max-height:190px;overflow:auto;margin:10px 0;padding:8px 10px;' +
                'background:var(--bg-secondary);border:1px solid var(--line);border-radius:6px;font-size:11.5px">';
            listed.forEach((item) => {
                const flag = item.level === 'critical' ? ' — <span style="color:var(--danger-text)">' + esc(item.reason) + '</span>'
                    : (!item.is_dir && !item.capturable ? ' — <span style="color:var(--warning)">not recoverable</span>' : '');
                html += '<div>' + (item.is_dir ? '📁 ' : '') + esc(item.path) + flag + '</div>';
            });
            if (plan.items.length > listed.length) {
                html += '<div style="opacity:.7">…and ' + (plan.items.length - listed.length) + ' more</div>';
            }
            html += '</div>';

            if (plan.critical && plan.critical.length) {
                html += '<p style="color:var(--danger-text)"><b>This includes files the server needs:</b><br>' +
                    plan.critical.slice(0, 8).map(esc).join('<br>') + '</p>';
            }

            (plan.warnings || []).forEach((w) => {
                html += '<p style="color:var(--warning)">' + esc(w) + '</p>';
            });

            if (plan.recoverable) {
                html += '<p>All of it is copied to the local recycle bin first (' +
                    size(plan.bin_free) + ' free of ' + size(plan.bin_limit) + '), so it can be restored from Vault.</p>';
            } else {
                html += '<p style="color:var(--danger-text)"><b>Not everything here can be recovered.</b> ' +
                    'Whatever the recycle bin cannot hold is gone once this runs.</p>';
            }

            html += '</div>';
            return html;
        },

        // Drops editor tabs for files that no longer exist, so a later Save
        // cannot recreate a file the user deleted.
        forgetOpenFilesUnder(roots) {
            const paths = Array.from(this.openFiles.keys());
            paths.forEach((path) => {
                const gone = roots.some((root) => path === root || path.indexOf(root + '/') === 0);
                if (gone) this.closeFileTab(path);
            });
        },

        async renameFile() {
            const path = this.resolvePath(this.selectedFile);
            if (!path) return;

            const current = this.selectedFile.name;
            const v = await window.Shell.dialog.form('Rename', [
                { name: 'name', label: 'New name', value: current, mono: true }
            ], { confirmLabel: 'Rename' });
            if (!v || !v.name || !v.name.trim() || v.name.trim() === current) return;

            try {
                // RenameFileStrict refuses a name that is already taken, which
                // the panel's rename route would otherwise overwrite.
                await window.go.main.App.RenameFileStrict(path, v.name.trim());
                await this.refreshFiles();
            } catch (err) {
                await this.say('Could not rename', String(err));
            }
        },

        async duplicateFile() {
            const path = this.resolvePath(this.selectedFile);
            if (!path || this.selectedFile.isDir) {
                await this.say('Cannot duplicate', 'Pick a file. Folders cannot be duplicated from here.');
                return;
            }

            const name = this.selectedFile.name;
            const dot = name.lastIndexOf('.');
            const suggested = dot > 0 ? name.slice(0, dot) + '-copy' + name.slice(dot) : name + '-copy';

            const v = await window.Shell.dialog.form('Duplicate ' + this.escapeHtml(name), [
                { name: 'name', label: 'Name for the copy', value: suggested, mono: true }
            ], { confirmLabel: 'Duplicate' });
            if (!v || !v.name || !v.name.trim()) return;

            const target = this.currentPath === '/' ? '/' + v.name.trim() : this.currentPath + '/' + v.name.trim();

            try {
                const read = await window.go.main.App.ReadFileForEdit('', path);
                if (read.binary || read.too_big) {
                    await this.say('Cannot duplicate',
                        name + ' is binary or too large to copy through the editor.');
                    return;
                }
                await window.go.main.App.CreateFileStrict(target);
                await window.go.main.App.SafeSaveFileContent(target, read.content, '', false);
                await this.refreshFiles();
            } catch (err) {
                await this.say('Could not duplicate', String(err));
            }
        },

        copyPath() {
            const path = this.resolvePath(this.selectedFile);
            if (!path) return;
            navigator.clipboard.writeText(path);
            console.log('Path copied:', path);
        },

        async downloadFile() {
            const path = this.resolvePath(this.selectedFile);
            if (!path || this.selectedFile.isDir) {
                await this.say('Cannot download', 'Pick a file. The panel does not serve folders as a single download.');
                return;
            }

            try {
                const url = await window.go.main.App.GetFileDownloadURL(path);
                if (window.runtime && window.runtime.BrowserOpenURL) window.runtime.BrowserOpenURL(url);
                else window.open(url, '_blank');
            } catch (err) {
                await this.say('Could not download', String(err));
            }
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
        
        // Panel Management
        async showPanelManager() {
            const modal = document.getElementById('panelManagerModal');
            if (modal) {
                modal.classList.add('show');
                this.cancelPanelEdit();
                await this.loadPanelList();
            }
        },
        
        closePanelManager() {
            const modal = document.getElementById('panelManagerModal');
            if (modal) modal.classList.remove('show');
        },
        
        async loadPanelList() {
            try {
                const panels = await window.go.main.App.ListPanels();
                const activePanel = await window.go.main.App.GetActivePanel();
                const listEl = document.getElementById('panelList');
                
                if (!listEl) return;
                
                if (panels.length === 0) {
                    listEl.innerHTML = '<div style="padding: 20px; text-align: center; color: var(--text-secondary);">No panels configured. Add one below.</div>';
                    return;
                }
                
                // Details carry the URL and whether an admin key is set, so the
                // row can say what it is instead of "Panel configuration".
                const details = await window.go.main.App.GetPanels().catch(() => []);
                const detailFor = {};
                (details || []).forEach(d => { detailFor[d.name] = d; });

                listEl.innerHTML = panels.map(panel => {
                    const d = detailFor[panel] || {};
                    const esc = this.escapeHtml(panel);
                    return `
                    <div class="panel-item ${panel === activePanel ? 'active' : ''}" data-panel="${esc}">
                        <div class="panel-info">
                            <div class="panel-name">${esc}${d.hasAdminKey ? '<span class="panel-tag">admin key</span>' : ''}</div>
                            <div class="panel-url mono">${this.escapeHtml(d.panelURL || 'no URL recorded')}</div>
                        </div>
                        <div class="panel-actions">
                            ${panel !== activePanel ? `<button onclick="window.app.switchPanel('${esc}')">Switch</button>` : ''}
                            <button onclick="window.app.editPanel('${esc}')">Edit</button>
                            ${d.hasAdminKey ? `<button onclick="window.app.clearPanelAdminKey('${esc}')">Clear admin key</button>` : ''}
                            <button onclick="window.app.removePanel('${esc}')" class="danger">Remove</button>
                        </div>
                    </div>`;
                }).join('');
            } catch (err) {
                console.error('Failed to load panel list:', err);
            }
        },
        
        async switchPanel(panelName) {
            if (!panelName) return;
            
            try {
                console.log('Switching to panel:', panelName);
                await window.go.main.App.SetActivePanel(panelName);
                
                // Update panel dropdown
                const dropdown = document.getElementById('panelDropdown');
                if (dropdown) {
                    dropdown.value = panelName;
                }
                
                // Reconnect with new panel
                await this.connect();
                
                // Reload panel list if modal is open
                const modal = document.getElementById('panelManagerModal');
                if (modal && modal.classList.contains('show')) {
                    await this.loadPanelList();
                }
                
                // Load servers for the new panel
                await this.loadServers();
                
                // Clear and reload files
                await this.loadFiles();
            } catch (err) {
                console.error('Failed to switch panel:', err);
                alert('Failed to switch panel: ' + err);
            }
        },
        
        async addNewPanel() {
            const name = document.getElementById('newPanelName')?.value?.trim();
            const url = document.getElementById('newPanelUrl')?.value?.trim();
            const apiKey = document.getElementById('newPanelApiKey')?.value?.trim();
            const adminKey = document.getElementById('newPanelAdminKey')?.value?.trim();

            if (!name || !url) {
                await this.say('Missing details', 'A display name and the panel URL are both required.');
                return;
            }
            // Editing an existing panel may leave the keys blank to keep them.
            if (!apiKey && !this.editingPanel) {
                await this.say('Missing details', 'A client API key is required for a new panel.');
                return;
            }

            // AddPanel connects when the new panel becomes the active one, and
            // reports that separately from failing to save. A panel that saved
            // but could not connect is still worth keeping — the URL or the key
            // can be corrected from the list instead of retyped.
            let offlineReason = null;
            try {
                await window.go.main.App.AddPanel(name, url, apiKey, adminKey);
            } catch (err) {
                const message = String(err);
                if (message.indexOf('panel saved, but connecting failed') === -1) {
                    console.error('Failed to add panel:', err);
                    await this.say('Could not add the panel', message);
                    return;
                }
                offlineReason = message;
            }

            this.cancelPanelEdit();

            // Reload panel list
            await this.loadPanelList();
            await this.loadPanels();

            if (offlineReason) {
                this.updateStatus(false);
                await this.say('Saved, but not connected',
                    offlineReason + '\n\nCheck the panel URL and the API key, then use Switch on it in this list to try again.');
                return;
            }

            // Connected. Pull the server list in; loadServers selects the first
            // one when nothing is configured yet, which is what populates the
            // rest of the app.
            await this.loadServers();
            document.dispatchEvent(new CustomEvent('shell:refresh'));

            const dropdown = document.getElementById('serverDropdown');
            const count = dropdown ? Math.max(0, dropdown.options.length - 1) : 0;

            if (!count) {
                await this.say('Connected, but no servers',
                    'Connected to ' + name + ', but this API key can see no servers on it. ' +
                    'A client key only lists servers your account owns or has been added to.');
                return;
            }

            this.closePanelManager();
        },
        
        // Loads a panel back into the form. The key fields stay blank on
        // purpose: the app never sends a stored key back to the UI, and blank
        // means "keep it" on the way in.
        async editPanel(panelName) {
            const details = await window.go.main.App.GetPanels().catch(() => []);
            const panel = (details || []).find(d => d.name === panelName);
            if (!panel) return;

            this.editingPanel = panelName;
            document.getElementById('newPanelName').value = panel.name;
            document.getElementById('newPanelUrl').value = panel.panelURL || '';
            document.getElementById('newPanelApiKey').value = '';
            document.getElementById('newPanelAdminKey').value = '';

            document.getElementById('panelFormTitle').textContent = 'Edit ' + panel.name;
            document.getElementById('panelFormSubmit').textContent = 'Save panel';
            document.getElementById('panelFormCancel').hidden = false;

            const note = document.getElementById('panelFormEditing');
            note.hidden = false;
            note.textContent = panel.hasAdminKey
                ? 'Leave either key blank to keep the one already saved. This panel has an admin key set.'
                : 'Leave the client key blank to keep the one already saved.';

            document.getElementById('newPanelUrl').focus();
        },

        cancelPanelEdit() {
            this.editingPanel = null;
            document.getElementById('newPanelName').value = '';
            document.getElementById('newPanelUrl').value = '';
            document.getElementById('newPanelApiKey').value = '';
            document.getElementById('newPanelAdminKey').value = '';

            document.getElementById('panelFormTitle').textContent = 'Add a panel';
            document.getElementById('panelFormSubmit').textContent = 'Add panel';
            document.getElementById('panelFormCancel').hidden = true;
            document.getElementById('panelFormEditing').hidden = true;
        },

        async clearPanelAdminKey(panelName) {
            const ok = await this.ask('Remove the admin key',
                'The server list for <b>' + this.escapeHtml(panelName) + '</b> falls back to what the client key can see — ' +
                'the servers your account owns or was added to.',
                { confirmLabel: 'Remove it' });
            if (!ok) return;

            try {
                await window.go.main.App.ClearPanelAdminKey(panelName);
            } catch (err) {
                const message = String(err);
                if (message.indexOf('panel saved, but connecting failed') === -1) {
                    await this.say('Could not remove the admin key', message);
                    return;
                }
                await this.say('Removed, but not connected', message);
            }

            await this.loadPanelList();
            await this.loadServers();
            document.dispatchEvent(new CustomEvent('shell:refresh'));
        },

        async removePanel(panelName) {
            const ok = await this.ask('Remove panel',
                'Remove <b>' + this.escapeHtml(panelName) + '</b> and its saved keys from this app? ' +
                'Nothing on the panel itself changes.',
                { danger: true, confirmLabel: 'Remove' });
            if (!ok) return;

            try {
                await window.go.main.App.RemovePanel(panelName);
                if (this.editingPanel === panelName) this.cancelPanelEdit();
                await this.loadPanelList();
                await this.loadPanels();
            } catch (err) {
                console.error('Failed to remove panel:', err);
                await this.say('Could not remove the panel', String(err));
            }
        },
        
        async loadPanels() {
            try {
                const panels = await window.go.main.App.ListPanels();
                const activePanel = await window.go.main.App.GetActivePanel();
                const dropdown = document.getElementById('panelDropdown');
                
                if (!dropdown) return;
                
                // Clear existing options
                dropdown.innerHTML = '<option value="" disabled>Select Panel</option>';
                
                // Add panel options
                const panelDetails = await window.go.main.App.GetPanels().catch(() => []);
                const urlFor = {};
                (panelDetails || []).forEach(p => { urlFor[p.name] = p.url || p.panelURL || ''; });

                panels.forEach(panel => {
                    const option = document.createElement('option');
                    option.value = panel;
                    option.textContent = panel;
                    option.selected = panel === activePanel;
                    // Read by the chip menu in ui-shell.js for the sub-label.
                    if (urlFor[panel]) option.dataset.sub = String(urlFor[panel]).replace(/^https?:\/\//, '');
                    dropdown.appendChild(option);
                });
                
                // If no panel is selected and we have panels, prompt to select
                if (!activePanel && panels.length > 0) {
                    dropdown.value = '';
                }
            } catch (err) {
                console.error('Failed to load panels:', err);
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
            document.getElementById('historyBtn').style.display = 'none';

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
