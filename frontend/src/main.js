// Main application entry point
import './style.css';
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime.js';
import { 
    Connect, GetConfig, SaveConfig, GetServerState, SetPowerState,
    ConnectConsole, DisconnectConsole, SendCommand,
    ListFiles, GetFileContent, SaveFileContent, CreateFolder, 
    DeleteFiles, RenameFile, UploadFile
} from '../wailsjs/go/main/App.js';

// ANSI to HTML converter for console colors
function ansiToHtml(text) {
    // Basic ANSI code removal for now, can be enhanced
    return text
        .replace(/\x1b\[[0-9;]*m/g, '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

class PterodactylApp {
    constructor() {
        this.currentPath = '/';
        this.selectedFile = null;
        this.isConnected = false;
        this.consoleConnected = false;
        this.consoleLines = [];
        this.editorModal = null;
        this.currentEditFile = null;
    }
    
    async init() {
        console.log('Initializing Pterodactyl Manager...');
        
        // Wait for DOM
        if (document.readyState !== 'loading') {
            this.setupUI();
        } else {
            document.addEventListener('DOMContentLoaded', () => this.setupUI());
        }
    }
    
    setupUI() {
        console.log('Setting up UI...');
        
        // Setup event listeners
        this.setupEventListeners();
        
        // Setup file upload
        const fileInput = document.getElementById('fileInput');
        if (fileInput) {
            fileInput.addEventListener('change', (e) => {
                this.handleFileUpload(e.target.files);
            });
        }
        
        // Setup console input
        const commandInput = document.getElementById('commandInput');
        if (commandInput) {
            commandInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    this.sendCommand();
                }
            });
        }
        
        // Check for existing config and connect
        this.checkConfig();
    }
    
    async checkConfig() {
        try {
            const config = await GetConfig();
            console.log('Config loaded:', config);
            
            if (config && config.panelURL && config.apiKey && config.serverID) {
                await this.connect();
            } else {
                this.showSettings();
            }
        } catch (err) {
            console.error('Failed to load config:', err);
            this.showSettings();
        }
    }
    
    setupEventListeners() {
        // Backend events
        EventsOn('connected', (connected) => {
            console.log('Connection status:', connected);
            this.isConnected = connected;
            this.updateStatus(connected);
            if (connected) {
                this.loadFiles('/');
            }
        });
        
        EventsOn('console-output', (message) => {
            this.appendConsole(message);
        });
        
        EventsOn('console-error', (error) => {
            this.appendConsole(`[ERROR] ${error}`, 'error');
        });
        
        EventsOn('console-connected', (connected) => {
            this.consoleConnected = connected;
            const btn = document.getElementById('connectBtn');
            if (btn) {
                btn.textContent = connected ? 'Disconnect' : 'Connect';
            }
            if (connected) {
                this.appendConsole('=== Console connected ===', 'info');
            }
        });
        
        // Hide context menu on click
        document.addEventListener('click', () => {
            const menu = document.getElementById('contextMenu');
            if (menu) menu.classList.remove('show');
        });
    }
    
    async connect() {
        try {
            console.log('Connecting to server...');
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
    }
    
    // Tab Management
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
    }
    
    // File Manager
    async loadFiles(path) {
        console.log('Loading files from:', path);
        this.currentPath = path;
        
        const pathInput = document.getElementById('currentPath');
        if (pathInput) pathInput.value = path;
        
        const tree = document.getElementById('fileTree');
        if (!tree) return;
        
        tree.innerHTML = '<div class="loading">Loading files...</div>';
        
        try {
            const files = await ListFiles(path);
            console.log('Files loaded:', files);
            this.renderFiles(files);
        } catch (err) {
            console.error('Failed to load files:', err);
            tree.innerHTML = `<div class="error">Failed to load files: ${err}</div>`;
        }
    }
    
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
        
        // Render files
        files.forEach(file => {
            const item = this.createFileItem(file);
            tree.appendChild(item);
        });
    }
    
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
        
        div.addEventListener('click', () => {
            if (isParent) {
                const parentPath = this.currentPath.split('/').filter(p => p).slice(0, -1).join('/') || '/';
                this.loadFiles(parentPath);
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
        if (!preview) return;
        
        if (file.size > 1024 * 1024) {
            preview.innerHTML = '<div class="preview-empty">File too large to preview</div>';
            return;
        }
        
        try {
            const filePath = this.currentPath === '/' 
                ? '/' + file.name 
                : this.currentPath + '/' + file.name;
            
            const content = await GetFileContent(filePath);
            
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
        if (!menu) return;
        
        menu.style.left = event.clientX + 'px';
        menu.style.top = event.clientY + 'px';
        menu.classList.add('show');
        this.contextFile = file;
    }
    
    async refreshFiles() {
        await this.loadFiles(this.currentPath);
    }
    
    uploadFile() {
        const input = document.getElementById('fileInput');
        if (input) input.click();
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
    
    async editFile() {
        if (!this.selectedFile || !this.selectedFile.isFile) {
            alert('Please select a file to edit');
            return;
        }
        
        // For now, open in preview with edit capability
        // Monaco editor can be added here later
        const filePath = this.currentPath === '/' 
            ? '/' + this.selectedFile.name 
            : this.currentPath + '/' + this.selectedFile.name;
        
        const content = await GetFileContent(filePath);
        const newContent = prompt('Edit file content:', content);
        
        if (newContent !== null && newContent !== content) {
            try {
                await SaveFileContent(filePath, newContent);
                alert('File saved successfully');
                await this.previewFile(this.selectedFile);
            } catch (err) {
                alert('Failed to save file: ' + err);
            }
        }
    }
    
    // Console methods
    appendConsole(message, type = '') {
        const console = document.getElementById('console');
        if (!console) return;
        
        const line = document.createElement('div');
        line.className = 'console-line';
        if (type) line.classList.add(type);
        
        // Simple ANSI stripping for now
        line.textContent = ansiToHtml(message);
        
        console.appendChild(line);
        
        // Auto scroll
        console.scrollTop = console.scrollHeight;
        
        // Limit lines
        while (console.children.length > 1000) {
            console.removeChild(console.firstChild);
        }
    }
    
    clearConsole() {
        const console = document.getElementById('console');
        if (console) {
            console.innerHTML = '';
            this.appendConsole('Console cleared', 'info');
        }
    }
    
    async connectConsole() {
        if (this.consoleConnected) {
            await DisconnectConsole();
            this.consoleConnected = false;
            document.getElementById('connectBtn').textContent = 'Connect';
        } else {
            try {
                await ConnectConsole();
            } catch (err) {
                alert('Failed to connect console: ' + err);
            }
        }
    }
    
    async sendCommand() {
        const input = document.getElementById('commandInput');
        if (!input) return;
        
        const command = input.value.trim();
        if (!command) return;
        
        this.appendConsole(`> ${command}`, 'command');
        input.value = '';
        
        try {
            await SendCommand(command);
        } catch (err) {
            this.appendConsole(`Failed to send command: ${err}`, 'error');
        }
    }
    
    async sendPower(signal) {
        try {
            await SetPowerState(signal);
            this.appendConsole(`Power signal sent: ${signal}`, 'info');
        } catch (err) {
            alert('Failed to send power signal: ' + err);
        }
    }
    
    // Settings
    showSettings() {
        const modal = document.getElementById('settingsModal');
        if (modal) {
            modal.classList.add('show');
            
            // Load existing config
            GetConfig().then(config => {
                const urlInput = document.getElementById('panelUrl');
                const keyInput = document.getElementById('apiKey');
                const idInput = document.getElementById('serverId');
                
                if (urlInput) urlInput.value = config.panelURL || '';
                if (keyInput) keyInput.value = config.apiKey || '';
                if (idInput) idInput.value = config.serverID || '';
            });
        }
    }
    
    closeSettings() {
        const modal = document.getElementById('settingsModal');
        if (modal) modal.classList.remove('show');
    }
    
    async saveSettings() {
        const panelURL = document.getElementById('panelUrl')?.value;
        const apiKey = document.getElementById('apiKey')?.value;
        const serverID = document.getElementById('serverId')?.value;
        
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

// Initialize app
const app = new PterodactylApp();
window.app = app;

// Start initialization
app.init();
