// Full Pterodactyl Manager Application
console.log('Pterodactyl Manager starting...');

// Wait for Wails runtime to be available
function waitForRuntime() {
    if (typeof window.go !== 'undefined' && typeof window.runtime !== 'undefined') {
        console.log('Runtime ready, initializing app...');
        initApp();
    } else {
        console.log('Waiting for runtime...');
        setTimeout(waitForRuntime, 50);
    }
}

function initApp() {
    console.log('Initializing application...');
    
    // Main app object
    const app = {
        currentPath: '/',
        selectedFile: null,
        contextFile: null,
        isConnected: false,
        consoleConnected: false,
        
        // Initialize the app
        async init() {
            console.log('App init started');
            this.setupEventListeners();
            await this.checkConfig();
        },
        
        // Setup event listeners
        setupEventListeners() {
            // Listen for backend events
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
            
            // File upload listener
            const fileInput = document.getElementById('fileInput');
            if (fileInput) {
                fileInput.addEventListener('change', (e) => {
                    this.handleFileUpload(e.target.files);
                });
            }
            
            // Console input enter key
            const commandInput = document.getElementById('commandInput');
            if (commandInput) {
                commandInput.addEventListener('keydown', (e) => {
                    if (e.key === 'Enter') {
                        this.sendCommand();
                    }
                });
            }
            
            // Hide context menu on click
            document.addEventListener('click', () => {
                const menu = document.getElementById('contextMenu');
                if (menu) menu.classList.remove('show');
            });
        },
        
        // Check configuration
        async checkConfig() {
            try {
                const config = await window.go.main.App.GetConfig();
                console.log('Config loaded:', config);
                
                if (config && config.panelURL && config.apiKey && config.serverID) {
                    await this.connect();
                } else {
                    this.showSettings();
                }
            } catch (err) {
                console.error('Config check failed:', err);
                this.showSettings();
            }
        },
        
        // Connect to server
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
        
        // Update connection status
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
        
        // Tab switching
        switchTab(tabName) {
            console.log('Switching to tab:', tabName);
            
            // Update tab buttons
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            if (event && event.target) {
                event.target.classList.add('active');
            }
            
            // Update tab content
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            const tabContent = document.getElementById(tabName + 'Tab');
            if (tabContent) {
                tabContent.classList.add('active');
            }
        },
        
        // Load files from server
        async loadFiles(path) {
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
        
        // Render file list
        renderFiles(files) {
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            
            tree.innerHTML = '';
            
            // Sort files: folders first, then by name
            files.sort((a, b) => {
                if (a.isDir !== b.isDir) return b.isDir ? 1 : -1;
                return a.name.localeCompare(b.name);
            });
            
            // Add parent directory if not root
            if (this.currentPath !== '/') {
                const parentItem = this.createFileItem({
                    name: '..',
                    isDir: true,
                    size: 0
                }, true);
                tree.appendChild(parentItem);
            }
            
            // Render each file
            files.forEach(file => {
                const item = this.createFileItem(file);
                tree.appendChild(item);
            });
            
            if (files.length === 0 && this.currentPath === '/') {
                tree.innerHTML = '<div class="preview-empty">No files found</div>';
            }
        },
        
        // Create file item element
        createFileItem(file, isParent = false) {
            const div = document.createElement('div');
            div.className = 'file-item';
            
            const icon = document.createElement('span');
            icon.className = 'file-icon';
            icon.textContent = file.isDir ? '[D]' : '[F]';
            
            const name = document.createElement('span');
            name.className = 'file-name';
            name.textContent = file.name;
            
            const size = document.createElement('span');
            size.className = 'file-size';
            size.textContent = file.isDir ? '' : this.formatSize(file.size);
            
            div.appendChild(icon);
            div.appendChild(name);
            div.appendChild(size);
            
            // Click handler
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
                    this.previewFile(file);
                }
            });
            
            // Right-click handler
            div.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                if (!isParent) {
                    this.showContextMenu(e, file);
                }
            });
            
            return div;
        },
        
        // Preview file content
        async previewFile(file) {
            const preview = document.getElementById('filePreview');
            if (!preview) return;
            
            if (file.size > 1024 * 1024) {
                preview.innerHTML = '<div class="preview-empty">File too large to preview</div>';
                return;
            }
            
            try {
                const filePath = this.currentPath === '/' 
                    ? '/' + file.name 
                    : this.currentPath + '/' + file.name;
                
                const content = await window.go.main.App.GetFileContent(filePath);
                
                preview.innerHTML = `
                    <div class="preview-header">
                        <span>${file.name}</span>
                        <button onclick="window.app.editFile()">Edit</button>
                    </div>
                    <pre class="preview-content">${this.escapeHtml(content)}</pre>
                `;
            } catch (err) {
                preview.innerHTML = '<div class="error">Failed to load file: ' + err + '</div>';
            }
        },
        
        // Show context menu
        showContextMenu(event, file) {
            const menu = document.getElementById('contextMenu');
            if (!menu) return;
            
            menu.style.left = event.clientX + 'px';
            menu.style.top = event.clientY + 'px';
            menu.classList.add('show');
            this.contextFile = file;
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
        
        async editFile() {
            if (!this.selectedFile || this.selectedFile.isDir) {
                alert('Please select a file to edit');
                return;
            }
            
            const filePath = this.currentPath === '/' 
                ? '/' + this.selectedFile.name 
                : this.currentPath + '/' + this.selectedFile.name;
            
            try {
                const content = await window.go.main.App.GetFileContent(filePath);
                const newContent = prompt('Edit file content:', content);
                
                if (newContent !== null && newContent !== content) {
                    await window.go.main.App.SaveFileContent(filePath, newContent);
                    alert('File saved successfully');
                    await this.previewFile(this.selectedFile);
                }
            } catch (err) {
                alert('Failed to edit file: ' + err);
            }
        },
        
        // Context menu actions
        openFile() { this.editFile(); },
        renameFile() { alert('Rename not implemented yet'); },
        downloadFile() { alert('Download not implemented yet'); },
        deleteFile() { this.deleteSelected(); },
        
        // Console operations
        appendConsole(message, type = '') {
            const consoleEl = document.getElementById('console');
            if (!consoleEl) return;
            
            const line = document.createElement('div');
            line.className = 'console-line';
            if (type) line.classList.add(type);
            line.textContent = message;
            
            consoleEl.appendChild(line);
            consoleEl.scrollTop = consoleEl.scrollHeight;
            
            // Limit lines
            while (consoleEl.children.length > 1000) {
                consoleEl.removeChild(consoleEl.firstChild);
            }
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
                    console.log('Console connect initiated');
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
        
        // Settings modal
        showSettings() {
            const modal = document.getElementById('settingsModal');
            if (modal) {
                modal.classList.add('show');
                
                // Load existing config
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
            
            if (!panelURL || !apiKey || !serverID) {
                alert('All fields are required');
                return;
            }
            
            try {
                console.log('Saving settings:', { panelURL, serverID });
                await window.go.main.App.SaveConfig(panelURL, apiKey, serverID);
                this.closeSettings();
                console.log('Settings saved, connecting...');
                await this.connect();
            } catch (err) {
                console.error('Save settings failed:', err);
                alert('Failed to save settings: ' + err);
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
    
    console.log('App initialized successfully');
}

// Start initialization when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForRuntime);
} else {
    waitForRuntime();
}
