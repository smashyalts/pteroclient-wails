// Split View Editor Implementation
console.log('Split View Editor initializing...');

// Language and icon mappings (shared with main editor)
const LANGUAGE_MAP = {
    'js': 'javascript', 'jsx': 'javascript', 'ts': 'typescript', 'tsx': 'typescript',
    'py': 'python', 'rb': 'ruby', 'go': 'go', 'rs': 'rust', 'java': 'java',
    'kt': 'kotlin', 'cpp': 'cpp', 'c': 'c', 'cs': 'csharp', 'php': 'php',
    'swift': 'swift', 'scala': 'scala', 'r': 'r', 'lua': 'lua', 'perl': 'perl',
    'sh': 'shell', 'bash': 'shell', 'zsh': 'shell', 'ps1': 'powershell',
    'html': 'html', 'htm': 'html', 'css': 'css', 'scss': 'scss', 'sass': 'scss',
    'less': 'less', 'json': 'json', 'xml': 'xml', 'yaml': 'yaml', 'yml': 'yaml',
    'toml': 'toml', 'ini': 'ini', 'conf': 'ini', 'cfg': 'ini',
    'md': 'markdown', 'markdown': 'markdown', 'rst': 'restructuredtext',
    'tex': 'latex', 'sql': 'sql', 'mysql': 'mysql', 'pgsql': 'pgsql',
    'dockerfile': 'dockerfile', 'makefile': 'makefile', 'cmake': 'cmake',
    'graphql': 'graphql', 'proto': 'protobuf', 'bat': 'bat', 'cmd': 'bat',
    'txt': 'plaintext', 'log': 'log'
};

const FILE_ICONS = {
    'javascript': '📜', 'typescript': '🔷', 'python': '🐍', 'go': '🐹',
    'rust': '🦀', 'java': '☕', 'php': '🐘', 'html': '🌐', 'css': '🎨',
    'json': '📋', 'yaml': '📄', 'markdown': '📝', 'dockerfile': '🐳',
    'sql': '🗃️', 'shell': '💻', 'xml': '📰', 'git': '📦'
};

// Initialize Monaco Loader
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
        console.log('Dependencies ready, loading Monaco Editor...');
        loadMonacoEditor();
    } else {
        console.log('Waiting for dependencies...');
        setTimeout(waitForDependencies, 50);
    }
}

function loadMonacoEditor() {
    require(['vs/editor/editor.main'], function() {
        console.log('Monaco Editor loaded, initializing split view...');
        initSplitView();
    });
}

function initSplitView() {
    // Main split view application
    const splitView = {
        // Editor instances for each pane
        editors: {
            1: null,
            2: null
        },
        
        // Pane state
        panes: {
            1: {
                serverID: null,
                currentPath: '/',
                openFiles: new Map(),
                activeFile: null
            },
            2: {
                serverID: null,
                currentPath: '/',
                openFiles: new Map(),
                activeFile: null
            }
        },
        
        // Layout state
        layout: 'horizontal',
        syncScroll: false,
        isConnected: false,
        servers: [],
        
        // Initialize the split view
        async init() {
            console.log('Initializing split view application...');
            
            // Create editor instances
            this.createEditors();
            
            // Setup event listeners
            this.setupEventListeners();
            
            // Setup splitter
            this.setupSplitter();
            
            // Load panels and servers
            await this.loadPanels();
            await this.checkConnection();
        },
        
        // Create Monaco editor instances
        createEditors() {
            // Create editor for pane 1
            const container1 = document.getElementById('editor1');
            if (container1) {
                this.editors[1] = monaco.editor.create(container1, {
                    value: '// Select a file from Server 1\n',
                    language: 'javascript',
                    theme: 'vs-dark',
                    fontSize: 14,
                    automaticLayout: true,
                    minimap: { enabled: true },
                    wordWrap: 'off',
                    lineNumbers: 'on',
                    scrollBeyondLastLine: false,
                    renderWhitespace: 'selection',
                    folding: true,
                    bracketPairColorization: { enabled: true }
                });
                
                this.editors[1].onDidChangeModelContent(() => {
                    this.onEditorChange(1);
                });
            }
            
            // Create editor for pane 2
            const container2 = document.getElementById('editor2');
            if (container2) {
                this.editors[2] = monaco.editor.create(container2, {
                    value: '// Select a file from Server 2\n',
                    language: 'javascript',
                    theme: 'vs-dark',
                    fontSize: 14,
                    automaticLayout: true,
                    minimap: { enabled: true },
                    wordWrap: 'off',
                    lineNumbers: 'on',
                    scrollBeyondLastLine: false,
                    renderWhitespace: 'selection',
                    folding: true,
                    bracketPairColorization: { enabled: true }
                });
                
                this.editors[2].onDidChangeModelContent(() => {
                    this.onEditorChange(2);
                });
            }
            
            // Setup sync scrolling if enabled
            if (this.syncScroll) {
                this.setupSyncScroll();
            }
        },
        
        // Setup event listeners
        setupEventListeners() {
            // Backend events
            window.runtime.EventsOn('connected', (connected) => {
                console.log('Connection event:', connected);
                this.isConnected = connected;
                this.updateStatus(connected);
                if (connected) {
                    this.loadServers();
                }
            });
            
            window.runtime.EventsOn('panel-changed', () => {
                this.loadServers();
            });
        },
        
        // Setup splitter for resizing panes
        setupSplitter() {
            const splitter = document.getElementById('splitter');
            const container = document.getElementById('splitContainer');
            const pane1 = document.getElementById('pane1');
            const pane2 = document.getElementById('pane2');
            
            let isDragging = false;
            let startPos = 0;
            let startSize1 = 0;
            
            splitter.addEventListener('mousedown', (e) => {
                isDragging = true;
                splitter.classList.add('dragging');
                
                if (this.layout === 'horizontal') {
                    startPos = e.clientX;
                    startSize1 = pane1.offsetWidth;
                } else {
                    startPos = e.clientY;
                    startSize1 = pane1.offsetHeight;
                }
                
                e.preventDefault();
            });
            
            document.addEventListener('mousemove', (e) => {
                if (!isDragging) return;
                
                let delta, newSize1;
                
                if (this.layout === 'horizontal') {
                    delta = e.clientX - startPos;
                    newSize1 = startSize1 + delta;
                    const totalWidth = container.offsetWidth - 4; // Minus splitter width
                    
                    if (newSize1 >= 200 && newSize1 <= totalWidth - 200) {
                        pane1.style.flex = `0 0 ${newSize1}px`;
                        pane2.style.flex = '1';
                    }
                } else {
                    delta = e.clientY - startPos;
                    newSize1 = startSize1 + delta;
                    const totalHeight = container.offsetHeight - 4; // Minus splitter height
                    
                    if (newSize1 >= 100 && newSize1 <= totalHeight - 100) {
                        pane1.style.flex = `0 0 ${newSize1}px`;
                        pane2.style.flex = '1';
                    }
                }
            });
            
            document.addEventListener('mouseup', () => {
                if (isDragging) {
                    isDragging = false;
                    splitter.classList.remove('dragging');
                    
                    // Trigger layout update for Monaco editors
                    if (this.editors[1]) this.editors[1].layout();
                    if (this.editors[2]) this.editors[2].layout();
                }
            });
        },
        
        // Setup synchronized scrolling
        setupSyncScroll() {
            if (!this.editors[1] || !this.editors[2]) return;
            
            let syncing = false;
            
            this.editors[1].onDidScrollChange((e) => {
                if (syncing || !this.syncScroll) return;
                syncing = true;
                this.editors[2].setScrollPosition({
                    scrollTop: e.scrollTop,
                    scrollLeft: e.scrollLeft
                });
                syncing = false;
            });
            
            this.editors[2].onDidScrollChange((e) => {
                if (syncing || !this.syncScroll) return;
                syncing = true;
                this.editors[1].setScrollPosition({
                    scrollTop: e.scrollTop,
                    scrollLeft: e.scrollLeft
                });
                syncing = false;
            });
        },
        
        // Toggle synchronized scrolling
        toggleSyncScroll(enabled) {
            this.syncScroll = enabled;
            if (enabled) {
                this.setupSyncScroll();
            }
        },
        
        // Set layout (horizontal or vertical)
        setLayout(layout) {
            this.layout = layout;
            const container = document.getElementById('splitContainer');
            const splitter = document.getElementById('splitter');
            const pane1 = document.getElementById('pane1');
            const pane2 = document.getElementById('pane2');
            
            // Reset flex properties
            pane1.style.flex = '1';
            pane2.style.flex = '1';
            
            if (layout === 'horizontal') {
                container.classList.remove('vertical');
                container.classList.add('horizontal');
                splitter.classList.remove('vertical');
                splitter.classList.add('horizontal');
            } else {
                container.classList.remove('horizontal');
                container.classList.add('vertical');
                splitter.classList.remove('horizontal');
                splitter.classList.add('vertical');
            }
            
            // Trigger layout update for Monaco editors
            setTimeout(() => {
                if (this.editors[1]) this.editors[1].layout();
                if (this.editors[2]) this.editors[2].layout();
            }, 100);
        },
        
        // Swap panes
        swapPanes() {
            // Swap server selections
            const server1 = document.getElementById('server1Dropdown').value;
            const server2 = document.getElementById('server2Dropdown').value;
            
            document.getElementById('server1Dropdown').value = server2;
            document.getElementById('server2Dropdown').value = server1;
            
            // Swap pane data
            const tempPane = this.panes[1];
            this.panes[1] = this.panes[2];
            this.panes[2] = tempPane;
            
            // Swap editor models
            const model1 = this.editors[1].getModel();
            const model2 = this.editors[2].getModel();
            this.editors[1].setModel(model2);
            this.editors[2].setModel(model1);
            
            // Reload file trees
            if (this.panes[1].serverID) {
                this.loadFilesForPane(1, this.panes[1].currentPath);
            }
            if (this.panes[2].serverID) {
                this.loadFilesForPane(2, this.panes[2].currentPath);
            }
            
            // Update tabs
            this.updateTabs(1);
            this.updateTabs(2);
        },
        
        // Toggle sidebar for a pane
        toggleSidebar(paneNum) {
            const sidebar = document.getElementById(`sidebar${paneNum}`);
            if (sidebar) {
                sidebar.classList.toggle('collapsed');
                setTimeout(() => {
                    if (this.editors[paneNum]) {
                        this.editors[paneNum].layout();
                    }
                }, 300);
            }
        },
        
        // Check connection and load configuration
        async checkConnection() {
            try {
                const config = await window.go.main.App.GetConfig();
                if (config && config.panelURL && config.apiKey) {
                    await window.go.main.App.Connect();
                    this.updateStatus(true);
                    await this.loadServers();
                }
            } catch (err) {
                console.error('Connection check failed:', err);
                this.updateStatus(false);
            }
        },
        
        // Load panels
        async loadPanels() {
            try {
                const panels = await window.go.main.App.ListPanels();
                const activePanel = await window.go.main.App.GetActivePanel();
                const dropdown = document.getElementById('panelDropdown');
                
                if (dropdown) {
                    dropdown.innerHTML = '<option value="" disabled>Select Panel</option>';
                    panels.forEach(panel => {
                        const option = document.createElement('option');
                        option.value = panel;
                        option.textContent = panel;
                        option.selected = panel === activePanel;
                        dropdown.appendChild(option);
                    });
                }
            } catch (err) {
                console.error('Failed to load panels:', err);
            }
        },
        
        // Switch panel
        async switchPanel(panelName) {
            if (!panelName) return;
            
            try {
                await window.go.main.App.SetActivePanel(panelName);
                await window.go.main.App.Connect();
                await this.loadServers();
                
                // Clear pane servers
                this.panes[1].serverID = null;
                this.panes[2].serverID = null;
                document.getElementById('server1Dropdown').value = '';
                document.getElementById('server2Dropdown').value = '';
            } catch (err) {
                console.error('Failed to switch panel:', err);
            }
        },
        
        // Load servers
        async loadServers() {
            try {
                const servers = await window.go.main.App.ListServers();
                this.servers = servers;
                
                // Update both server dropdowns
                ['server1Dropdown', 'server2Dropdown'].forEach(id => {
                    const dropdown = document.getElementById(id);
                    if (dropdown) {
                        const currentValue = dropdown.value;
                        dropdown.innerHTML = '<option value="" disabled selected>Select Server</option>';
                        
                        servers.forEach(server => {
                            const option = document.createElement('option');
                            option.value = server.id;
                            option.textContent = server.name;
                            dropdown.appendChild(option);
                        });
                        
                        // Restore previous selection if still valid
                        if (currentValue && servers.some(s => s.id === currentValue)) {
                            dropdown.value = currentValue;
                        }
                    }
                });
            } catch (err) {
                console.error('Failed to load servers:', err);
            }
        },
        
        // Switch server for a specific pane
        async switchPaneServer(paneNum, serverID) {
            if (!serverID) return;
            
            this.panes[paneNum].serverID = serverID;
            this.panes[paneNum].currentPath = '/';
            
            // Clear open files for this pane
            this.panes[paneNum].openFiles.clear();
            this.panes[paneNum].activeFile = null;
            
            // Clear tabs
            const tabsContainer = document.getElementById(`editorTabs${paneNum}`);
            if (tabsContainer) tabsContainer.innerHTML = '';
            
            // Load files for the new server
            await this.loadFilesForPane(paneNum, '/');
            
            // Update pane info
            const server = this.servers.find(s => s.id === serverID);
            if (server) {
                document.getElementById(`pane${paneNum}Info`).textContent = server.name;
            }
        },
        
        // Load files for a specific pane
        async loadFilesForPane(paneNum, path) {
            const serverID = this.panes[paneNum].serverID;
            if (!serverID) {
                const tree = document.getElementById(`fileTree${paneNum}`);
                if (tree) {
                    tree.innerHTML = '<div class="error">Please select a server</div>';
                }
                return;
            }
            
            this.panes[paneNum].currentPath = path;
            const tree = document.getElementById(`fileTree${paneNum}`);
            if (!tree) return;
            
            tree.innerHTML = '<div class="loading">Loading files...</div>';
            
            try {
                const files = await window.go.main.App.ListFilesFromServer(serverID, path);
                this.renderFilesForPane(paneNum, files || []);
            } catch (err) {
                console.error(`Failed to load files for pane ${paneNum}:`, err);
                tree.innerHTML = '<div class="error">Failed to load files</div>';
            }
        },
        
        // Render files for a specific pane
        renderFilesForPane(paneNum, files) {
            const tree = document.getElementById(`fileTree${paneNum}`);
            if (!tree) return;
            
            tree.innerHTML = '';
            
            // Sort files
            files.sort((a, b) => {
                if (a.isDir !== b.isDir) return b.isDir ? 1 : -1;
                return a.name.localeCompare(b.name);
            });
            
            // Add parent directory
            if (this.panes[paneNum].currentPath !== '/') {
                const parentItem = this.createFileItem(paneNum, {
                    name: '..',
                    isDir: true,
                    size: 0
                }, true);
                tree.appendChild(parentItem);
            }
            
            // Render files
            files.forEach(file => {
                const item = this.createFileItem(paneNum, file);
                tree.appendChild(item);
            });
        },
        
        // Create file item element
        createFileItem(paneNum, file, isParent = false) {
            const div = document.createElement('div');
            div.className = 'file-item';
            
            // Store file path as data attribute for tracking
            if (!isParent && !file.isDir) {
                const filePath = this.panes[paneNum].currentPath === '/' 
                    ? '/' + file.name 
                    : this.panes[paneNum].currentPath + '/' + file.name;
                div.dataset.filepath = filePath;
                div.dataset.pane = paneNum;
                
                // Check if this file is currently open and mark as selected
                if (this.panes[paneNum].openFiles.has(filePath)) {
                    div.classList.add('selected');
                }
            }
            
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
                    const parts = this.panes[paneNum].currentPath.split('/').filter(p => p);
                    parts.pop();
                    const parentPath = '/' + parts.join('/');
                    this.loadFilesForPane(paneNum, parentPath || '/');
                } else if (file.isDir) {
                    const newPath = this.panes[paneNum].currentPath === '/' 
                        ? '/' + file.name 
                        : this.panes[paneNum].currentPath + '/' + file.name;
                    this.loadFilesForPane(paneNum, newPath);
                } else {
                    // Update selection state
                    document.querySelectorAll(`#fileTree${paneNum} .file-item`).forEach(item => {
                        item.classList.remove('selected');
                    });
                    div.classList.add('selected');
                    this.openFileInPane(paneNum, file);
                }
            });
            
            return div;
        },
        
        // Open file in a specific pane
        async openFileInPane(paneNum, file) {
            if (!file || file.isDir) return;
            
            const serverID = this.panes[paneNum].serverID;
            if (!serverID) return;
            
            const filePath = this.panes[paneNum].currentPath === '/' 
                ? '/' + file.name 
                : this.panes[paneNum].currentPath + '/' + file.name;
            
            // Check if already open
            if (this.panes[paneNum].openFiles.has(filePath)) {
                this.switchToFileInPane(paneNum, filePath);
                return;
            }
            
            try {
                const content = await window.go.main.App.GetFileContentFromServer(serverID, filePath);
                
                // Detect language
                const language = this.detectLanguage(file.name);
                
                // Create model
                const model = monaco.editor.createModel(content, language);
                
                // Add to open files
                this.panes[paneNum].openFiles.set(filePath, {
                    name: file.name,
                    model: model,
                    originalContent: content,
                    modified: false,
                    language: language,
                    serverID: serverID
                });
                
                // Add tab
                this.addEditorTab(paneNum, filePath, file.name);
                
                // Switch to file
                this.switchToFileInPane(paneNum, filePath);
                
            } catch (err) {
                alert(`Failed to open file: ${err}`);
            }
        },
        
        // Switch to file in pane
        switchToFileInPane(paneNum, filePath) {
            const file = this.panes[paneNum].openFiles.get(filePath);
            if (!file) return;
            
            this.panes[paneNum].activeFile = filePath;
            
            // Update tabs
            document.querySelectorAll(`#editorTabs${paneNum} .editor-tab`).forEach(tab => {
                tab.classList.toggle('active', tab.dataset.path === filePath);
            });
            
            // Update file selection in sidebar
            document.querySelectorAll(`#fileTree${paneNum} .file-item`).forEach(item => {
                if (item.dataset.filepath) {
                    item.classList.toggle('selected', item.dataset.filepath === filePath);
                }
            });
            
            // Set model
            this.editors[paneNum].setModel(file.model);
            
            // Update pane info
            const server = this.servers.find(s => s.id === file.serverID);
            document.getElementById(`pane${paneNum}Info`).textContent = 
                `${server ? server.name : 'Unknown'}: ${file.name}`;
            
            // Focus editor
            this.editors[paneNum].focus();
        },
        
        // Add editor tab
        addEditorTab(paneNum, path, name) {
            const tabsContainer = document.getElementById(`editorTabs${paneNum}`);
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
                this.closeFileInPane(paneNum, path);
            };
            tab.appendChild(closeBtn);
            
            tab.onclick = () => this.switchToFileInPane(paneNum, path);
            
            tabsContainer.appendChild(tab);
        },
        
        // Close file in pane
        closeFileInPane(paneNum, path) {
            const file = this.panes[paneNum].openFiles.get(path);
            if (file) {
                file.model.dispose();
                this.panes[paneNum].openFiles.delete(path);
            }
            
            // Remove tab
            const tab = document.querySelector(`#editorTabs${paneNum} .editor-tab[data-path="${path}"]`);
            if (tab) tab.remove();
            
            // Update file item selection state in sidebar
            const fileItem = document.querySelector(`#fileTree${paneNum} .file-item[data-filepath="${path}"]`);
            if (fileItem) {
                fileItem.classList.remove('selected');
            }
            
            // Switch to another file or clear
            if (this.panes[paneNum].activeFile === path) {
                this.panes[paneNum].activeFile = null;
                if (this.panes[paneNum].openFiles.size > 0) {
                    const nextFile = this.panes[paneNum].openFiles.keys().next().value;
                    this.switchToFileInPane(paneNum, nextFile);
                } else {
                    // Clear editor
                    const emptyModel = monaco.editor.createModel(`// Select a file from Server ${paneNum}\n`, 'javascript');
                    this.editors[paneNum].setModel(emptyModel);
                    document.getElementById(`pane${paneNum}Info`).textContent = 'No file';
                }
            }
        },
        
        // Update tabs
        updateTabs(paneNum) {
            const tabsContainer = document.getElementById(`editorTabs${paneNum}`);
            if (!tabsContainer) return;
            
            tabsContainer.innerHTML = '';
            this.panes[paneNum].openFiles.forEach((file, path) => {
                this.addEditorTab(paneNum, path, file.name);
            });
            
            // Mark active tab
            if (this.panes[paneNum].activeFile) {
                const activeTab = tabsContainer.querySelector(`[data-path="${this.panes[paneNum].activeFile}"]`);
                if (activeTab) activeTab.classList.add('active');
            }
        },
        
        // Handle editor changes
        onEditorChange(paneNum) {
            if (!this.panes[paneNum].activeFile) return;
            
            const file = this.panes[paneNum].openFiles.get(this.panes[paneNum].activeFile);
            if (file) {
                const currentContent = file.model.getValue();
                file.modified = currentContent !== file.originalContent;
                this.updateTabModified(paneNum, this.panes[paneNum].activeFile, file.modified);
            }
        },
        
        // Update tab modified state
        updateTabModified(paneNum, path, modified) {
            const tab = document.querySelector(`#editorTabs${paneNum} .editor-tab[data-path="${path}"]`);
            if (tab) {
                tab.classList.toggle('modified', modified);
            }
        },
        
        // Save all modified files
        async saveAll() {
            const promises = [];
            
            for (let paneNum = 1; paneNum <= 2; paneNum++) {
                for (const [path, file] of this.panes[paneNum].openFiles) {
                    if (file.modified) {
                        promises.push(this.saveFileInPane(paneNum, path));
                    }
                }
            }
            
            await Promise.all(promises);
            console.log('All files saved');
        },
        
        // Save file in pane
        async saveFileInPane(paneNum, path) {
            const file = this.panes[paneNum].openFiles.get(path);
            if (!file || !file.modified) return;
            
            try {
                const content = file.model.getValue();
                await window.go.main.App.SaveFileContentToServer(file.serverID, path, content);
                file.originalContent = content;
                file.modified = false;
                this.updateTabModified(paneNum, path, false);
                console.log(`File saved: ${path}`);
            } catch (err) {
                alert(`Failed to save file: ${err}`);
            }
        },
        
        // Reload all files
        async reloadAll() {
            for (let paneNum = 1; paneNum <= 2; paneNum++) {
                if (this.panes[paneNum].serverID) {
                    await this.loadFilesForPane(paneNum, this.panes[paneNum].currentPath);
                }
            }
        },
        
        // Update connection status
        updateStatus(connected) {
            const dot = document.getElementById('statusDot');
            const text = document.getElementById('statusText');
            
            if (dot) {
                dot.classList.toggle('connected', connected);
            }
            
            if (text) {
                text.textContent = connected ? 'Connected' : 'Disconnected';
            }
            
            this.isConnected = connected;
        },
        
        // Helper functions
        detectLanguage(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            return LANGUAGE_MAP[ext] || 'plaintext';
        },
        
        getFileIcon(filename) {
            const ext = filename.split('.').pop().toLowerCase();
            const language = LANGUAGE_MAP[ext];
            if (language && FILE_ICONS[language]) {
                return FILE_ICONS[language];
            }
            return '📄';
        },
        
        formatSize(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
        }
    };
    
    // Set global reference
    window.splitView = splitView;
    
    // Initialize application
    splitView.init();
    
    console.log('Split View Editor initialized');
}

// Start initialization
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForDependencies);
} else {
    waitForDependencies();
}
