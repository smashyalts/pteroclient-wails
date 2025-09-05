// Complete Pterodactyl Manager Application
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime.js';
import { 
    Connect, GetConfig, SaveConfig, GetServerState, SetPowerState,
    ConnectConsole, DisconnectConsole, SendCommand,
    ListFiles, GetFileContent, SaveFileContent, CreateFolder, 
    DeleteFiles, RenameFile, UploadFile
} from '../wailsjs/go/main/App.js';

// Console functionality will be integrated here

class PterodactylManager {
    constructor() {
        this.currentPath = '/';
        this.selectedFile = null;
        this.isConnected = false;
        this.consoleConnected = false;
        
        this.init();
    }
    
    async init() {
        // Check for existing config
        try {
            const config = await GetConfig();
            if (config.panelURL && config.apiKey && config.serverID) {
                await this.connect();
            } else {
                this.showSettings();
            }
        } catch (err) {
            console.error('Init error:', err);
            this.showSettings();
        }
        
        // Setup event listeners
        this.setupEventListeners();
        
        // Setup file upload
        document.getElementById('fileInput').addEventListener('change', (e) => {
            this.handleFileUpload(e.target.files);
        });
    }
    
    setupEventListeners() {
        EventsOn('connected', (connected) => {
            this.isConnected = connected;
            this.updateStatus(connected);
            if (connected) {
                this.loadFiles('/');
            }
        });
        
        EventsOn('console-connected', (connected) => {
            this.consoleConnected = connected;
            document.getElementById('connectBtn').textContent = connected ? '🔌 Disconnect' : '🔌 Connect';
        });
        
        // Hide context menu on click
        document.addEventListener('click', () => {
            document.getElementById('contextMenu').classList.remove('show');
        });
    }
    
    async connect() {
        try {
            await Connect();
            this.updateStatus(true);
            await this.loadFiles('/');
        } catch (err) {
            console.error('Connection failed:', err);
            this.updateStatus(false);
            alert('Connection failed: ' + err);
        }
    }
    
    updateStatus(connected) {
        const dot = document.getElementById('statusDot');
        const text = document.getElementById('statusText');
        
        if (connected) {
            dot.classList.add('connected');
            text.textContent = 'Connected';
        } else {
            dot.classList.remove('connected');
            text.textContent = 'Disconnected';
        }
    }
    
    // Tab Management
    switchTab(tabName) {
        // Update tab buttons
        document.querySelectorAll('.tab').forEach(tab => {
            tab.classList.remove('active');
        });
        event.target.classList.add('active');
        
        // Update tab content
        document.querySelectorAll('.tab-content').forEach(content => {
            content.classList.remove('active');
        });
        document.getElementById(tabName + 'Tab').classList.add('active');
    }
    
    // File Manager
    async loadFiles(path) {
        this.currentPath = path;
        document.getElementById('currentPath').value = path;
        
        const tree = document.getElementById('fileTree');
        tree.innerHTML = '<div class="loading">Loading files...</div>';
        
        try {
            const files = await ListFiles(path);
            this.renderFiles(files);
        } catch (err) {
            tree.innerHTML = `<div class="error">Failed to load files: ${err}</div>`;
        }
    }
    
    renderFiles(files) {
        const tree = document.getElementById('fileTree');
        tree.innerHTML = '';
        
        // Sort files: folders first, then by name
        files.sort((a, b) => {
            if (a.isDir !== b.isDir) return b.isDir - a.isDir;
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
        
        // Render files
        files.forEach(file => {
            const item = this.createFileItem(file);
            tree.appendChild(item);
        });
    }
    
    createFileItem(file, isParent = false) {
        const div = document.createElement('div');
        div.className = 'file-item';
        const icon = file.isDir ? '[DIR]' : '[FILE]';
        div.innerHTML = `
            <span class="file-icon">${icon}</span>
            <span class="file-name">${file.name}</span>
            <span class="file-size">${file.isDir ? '' : this.formatSize(file.size)}</span>
        `;
        
        div.addEventListener('click', () => {
            if (isParent) {
                // Go to parent directory
                const parentPath = this.currentPath.split('/').slice(0, -1).join('/') || '/';
                this.loadFiles(parentPath);
            } else if (file.isDir) {
                // Navigate into directory
                const newPath = this.currentPath === '/' 
                    ? '/' + file.name 
                    : this.currentPath + '/' + file.name;
                this.loadFiles(newPath);
            } else {
                // Select file
                document.querySelectorAll('.file-item').forEach(item => {
                    item.classList.remove('selected');
                });
                div.classList.add('selected');
                this.selectedFile = file;
                this.previewFile(file);
            }
        });
        
        // Right-click menu
        div.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            if (!isParent) {
                this.showContextMenu(e, file);
            }
        });
        
        return div;
    }
    
    async previewFile(file) {
        const preview = document.getElementById('filePreview');
        
        if (file.size > 1024 * 1024) { // 1MB limit for preview
            preview.innerHTML = '<div class="preview-empty">File too large to preview</div>';
            return;
        }
        
        try {
            const content = await GetFileContent(
                this.currentPath === '/' 
                    ? '/' + file.name 
                    : this.currentPath + '/' + file.name
            );
            
            preview.innerHTML = `
                <div class="preview-header">
                    <span>${file.name}</span>
                    <button onclick="window.app.editFile()">Edit</button>
                </div>
                <pre class="preview-content">${this.escapeHtml(content)}</pre>
            `;
        } catch (err) {
            preview.innerHTML = `<div class="error">Failed to load file: ${err}</div>`;
        }
    }
    
    showContextMenu(event, file) {
        const menu = document.getElementById('contextMenu');
        menu.style.left = event.clientX + 'px';
        menu.style.top = event.clientY + 'px';
        menu.classList.add('show');
        this.contextFile = file;
    }
    
    async refreshFiles() {
        await this.loadFiles(this.currentPath);
    }
    
    uploadFile() {
        document.getElementById('fileInput').click();
    }
    
    async handleFileUpload(files) {
        for (const file of files) {
            try {
                const buffer = await file.arrayBuffer();
                const path = this.currentPath === '/' 
                    ? '/' + file.name 
                    : this.currentPath + '/' + file.name;
                
                await UploadFile(path, new Uint8Array(buffer));
            } catch (err) {
                alert(`Failed to upload ${file.name}: ${err}`);
            }
        }
        await this.refreshFiles();
    }
    
    async newFolder() {
        const name = prompt('Folder name:');
        if (!name) return;
        
        try {
            const path = this.currentPath === '/' 
                ? '/' + name 
                : this.currentPath + '/' + name;
            await CreateFolder(path);
            await this.refreshFiles();
        } catch (err) {
            alert('Failed to create folder: ' + err);
        }
    }
    
    async deleteSelected() {
        if (!this.selectedFile) {
            alert('No file selected');
            return;
        }
        
        if (!confirm(`Delete ${this.selectedFile.name}?`)) return;
        
        try {
            const path = this.currentPath === '/' 
                ? '/' + this.selectedFile.name 
                : this.currentPath + '/' + this.selectedFile.name;
            await DeleteFiles([path]);
            await this.refreshFiles();
        } catch (err) {
            alert('Failed to delete: ' + err);
        }
    }
    
    // Settings
    showSettings() {
        document.getElementById('settingsModal').classList.add('show');
        
        // Load existing config
        GetConfig().then(config => {
            document.getElementById('panelUrl').value = config.panelURL || '';
            document.getElementById('apiKey').value = config.apiKey || '';
            document.getElementById('serverId').value = config.serverID || '';
        });
    }
    
    closeSettings() {
        document.getElementById('settingsModal').classList.remove('show');
    }
    
    async saveSettings() {
        const panelURL = document.getElementById('panelUrl').value;
        const apiKey = document.getElementById('apiKey').value;
        const serverID = document.getElementById('serverId').value;
        
        if (!panelURL || !apiKey || !serverID) {
            alert('All fields are required');
            return;
        }
        
        try {
            await SaveConfig(panelURL, apiKey, serverID);
            this.closeSettings();
            await this.connect();
        } catch (err) {
            alert('Failed to save settings: ' + err);
        }
    }
    
    // Console
    async connectConsole() {
        if (this.consoleConnected) {
            await DisconnectConsole();
            this.consoleConnected = false;
            document.getElementById('connectBtn').textContent = '🔌 Connect';
        } else {
            try {
                await ConnectConsole();
            } catch (err) {
                alert('Failed to connect console: ' + err);
            }
        }
    }
    
    async sendPower(signal) {
        try {
            await SetPowerState(signal);
        } catch (err) {
            alert('Failed to send power signal: ' + err);
        }
    }
    
    // Utilities
    formatSize(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
    }
    
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

// Create global app instance
window.app = new PterodactylManager();
