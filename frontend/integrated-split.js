// Integrated Split View for Main Editor
// This adds split view functionality directly to the main editor interface

class IntegratedSplitView {
    constructor() {
        this.isActive = false;
        this.splitRatio = 0.5;
        this.orientation = 'vertical'; // 'vertical' or 'horizontal'
        this.monacoEditors = new Map(); // Store Monaco editor instances
        this.monacoModels = new Map(); // Store Monaco models for each pane
        this.activePane = 1;
        this.paneFiles = new Map(); // Track which file is open in which pane
        this.pane2Server = null; // Track which server pane 2 is connected to
        this.pane2Files = new Map(); // Store file content from pane 2 server
        
        // Store reference to main app
        this.app = null;
        
        // Splitter dragging state
        this.isDragging = false;
        this.dragStartX = 0;
        this.dragStartY = 0;
        this.dragStartRatio = 0.5;
        
        // Navigation history for each pane
        this.navigationHistory = new Map();
        this.navigationHistory.set(1, { history: ['/'], currentIndex: 0 });
        this.navigationHistory.set(2, { history: ['/'], currentIndex: 0 });
    }
    
    init(app) {
        this.app = app;
        this.setupSplitButton();
    }
    
    setupSplitButton() {
        // The split button is already in the HTML, just hook it up
        const splitBtn = document.getElementById('splitViewBtn');
        if (splitBtn) {
            splitBtn.addEventListener('click', () => this.toggleSplit());
        }
    }
    
    toggleSplit() {
        if (this.isActive) {
            this.deactivateSplit();
        } else {
            this.activateSplit();
        }
    }
    
    activateSplit() {
        console.log('Activating split view...');
        this.isActive = true;
        
        const fileManager = document.querySelector('.file-manager');
        const fileTree = document.querySelector('.file-tree');
        const editorContainer = document.querySelector('.editor-container');
        
        if (!fileManager || !editorContainer) {
            console.error('Required containers not found');
            return;
        }
        
        // Hide the original file tree and editor
        fileTree.style.display = 'none';
        editorContainer.style.display = 'none';
        
        // Create new split container that will replace the entire file manager area
        const splitContainer = document.createElement('div');
        splitContainer.className = 'split-view-container';
        splitContainer.style.cssText = `
            display: flex;
            flex-direction: row;
            width: 100%;
            height: 100%;
            gap: 4px;
        `;
        
        // Create first pane with file tree and editor
        const pane1 = this.createPane(1, 'Main Server');
        
        // Create splitter between file tree and editor for pane 1
        const splitter1 = this.createSplitter('vertical');
        
        // Create center splitter
        const centerSplitter = this.createSplitter('vertical', true);
        
        // Create second pane with file tree and editor
        const pane2 = this.createPane(2, 'Compare Server');
        
        // Create splitter between file tree and editor for pane 2
        const splitter2 = this.createSplitter('vertical');
        
        // Assemble split container
        splitContainer.appendChild(pane1);
        splitContainer.appendChild(centerSplitter);
        splitContainer.appendChild(pane2);
        
        // Replace the entire file manager content
        fileManager.appendChild(splitContainer);
        
        // Initialize editors after DOM is ready
        setTimeout(() => {
            this.initializePaneEditors();
        }, 100);
        
        // Setup splitter dragging for center splitter
        this.setupCenterSplitterDrag(centerSplitter, splitContainer);
        
        // Update split button
        const splitBtn = document.getElementById('splitViewBtn');
        if (splitBtn) {
            splitBtn.textContent = '⬌ Close Split';
            splitBtn.style.background = 'var(--accent)';
        }
        
        // Setup pane click handlers
        this.setupPaneHandlers();
    }
    
    deactivateSplit() {
        console.log('Deactivating split view...');
        this.isActive = false;
        
        const splitContainer = document.querySelector('.split-editor-container');
        const editorDiv = document.getElementById('editor');
        
        if (splitContainer) {
            // Get the active file from pane 1
            const activeFile = this.paneFiles.get(1) || this.paneFiles.get(2);
            
            // Restore original editor
            splitContainer.remove();
            editorDiv.style.display = '';
            
            // Restore editor content
            if (this.app.editor && activeFile) {
                const fileData = this.app.openFiles.get(activeFile);
                if (fileData) {
                    this.app.editor.setValue(fileData.content);
                    this.app.activeFile = activeFile;
                }
            }
        }
        
        // Dispose Monaco editors and models
        this.monacoEditors.forEach(editor => editor.dispose());
        this.monacoModels.forEach(model => model.dispose());
        this.monacoEditors.clear();
        this.monacoModels.clear();
        this.paneFiles.clear();
        
        // Update split button
        const splitBtn = document.getElementById('splitViewBtn');
        if (splitBtn) {
            splitBtn.textContent = '⬌ Split View';
            splitBtn.style.background = '#10b981';
        }
    }
    
    createTabsContainer(id, paneNum) {
        const container = document.createElement('div');
        container.className = 'editor-header';
        container.id = id;
        container.style.cssText = `
            display: flex;
            align-items: center;
            padding: 8px 12px;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--bg-tertiary);
            min-height: 40px;
            gap: 12px;
        `;
        
        // Add server selector for pane 2
        if (paneNum === 2) {
            const serverControl = document.createElement('div');
            serverControl.style.cssText = `
                display: flex;
                align-items: center;
                gap: 8px;
                padding: 0 12px;
                border-right: 1px solid var(--bg-tertiary);
            `;
            
            const serverLabel = document.createElement('span');
            serverLabel.style.cssText = `
                font-size: 12px;
                color: var(--text-secondary);
                font-weight: 500;
                text-transform: uppercase;
                letter-spacing: 0.5px;
            `;
            serverLabel.textContent = 'Server:';
            
            const serverSelect = document.createElement('select');
            serverSelect.id = 'pane2ServerSelect';
            serverSelect.style.cssText = `
                background: var(--bg-tertiary);
                color: var(--text-primary);
                border: 1px solid var(--accent);
                padding: 5px 12px;
                border-radius: 4px;
                font-size: 13px;
                font-weight: 500;
                cursor: pointer;
                outline: none;
                transition: all 0.2s ease;
                min-width: 150px;
            `;
            
            // Add hover effect
            serverSelect.addEventListener('mouseenter', () => {
                serverSelect.style.borderColor = 'var(--accent-hover)';
                serverSelect.style.background = 'var(--bg-secondary)';
            });
            
            serverSelect.addEventListener('mouseleave', () => {
                serverSelect.style.borderColor = this.pane2Server ? '#f5576c' : 'var(--accent)';
                serverSelect.style.background = 'var(--bg-tertiary)';
            });
            
            serverSelect.addEventListener('change', (e) => {
                this.switchPane2Server(e.target.value);
            });
            
            serverControl.appendChild(serverLabel);
            serverControl.appendChild(serverSelect);
            container.appendChild(serverControl);
            
            // Populate servers
            this.loadPane2Servers();
        }
        
        const tabs = document.createElement('div');
        tabs.className = 'editor-tabs';
        tabs.style.cssText = `
            display: flex;
            flex: 1;
            gap: 4px;
            align-items: center;
            overflow-x: auto;
        `;
        
        container.appendChild(tabs);
        return container;
    }
    
    setupSplitterDrag(splitter, container) {
        splitter.addEventListener('mousedown', (e) => {
            this.isDragging = true;
            this.dragStartX = e.clientX;
            this.dragStartY = e.clientY;
            this.dragStartRatio = this.splitRatio;
            
            splitter.style.background = 'var(--accent)';
            document.body.style.cursor = this.orientation === 'vertical' ? 'col-resize' : 'row-resize';
            
            e.preventDefault();
        });
        
        document.addEventListener('mousemove', (e) => {
            if (!this.isDragging) return;
            
            const rect = container.getBoundingClientRect();
            let newRatio;
            
            if (this.orientation === 'vertical') {
                const deltaX = e.clientX - this.dragStartX;
                const containerWidth = rect.width;
                newRatio = this.dragStartRatio + (deltaX / containerWidth);
            } else {
                const deltaY = e.clientY - this.dragStartY;
                const containerHeight = rect.height;
                newRatio = this.dragStartRatio + (deltaY / containerHeight);
            }
            
            // Clamp ratio between 0.1 and 0.9
            newRatio = Math.max(0.1, Math.min(0.9, newRatio));
            this.splitRatio = newRatio;
            
            // Update pane sizes
            const pane1 = document.getElementById('editorPane1');
            const pane2 = document.getElementById('editorPane2');
            if (pane1 && pane2) {
                pane1.style.flex = newRatio;
                pane2.style.flex = 1 - newRatio;
            }
            
            e.preventDefault();
        });
        
        document.addEventListener('mouseup', () => {
            if (this.isDragging) {
                this.isDragging = false;
                document.body.style.cursor = '';
                const splitter = document.querySelector('.editor-splitter');
                if (splitter) {
                    splitter.style.background = 'var(--bg-tertiary)';
                }
            }
        });
    }
    
    setupPaneHandlers() {
        // Click on pane 1 to activate it
        const pane1 = document.getElementById('editorPane1');
        if (pane1) {
            pane1.addEventListener('click', () => {
                this.activePane = 1;
                this.updatePaneStyles();
            });
        }
        
        // Click on pane 2 to activate it
        const pane2 = document.getElementById('editorPane2');
        if (pane2) {
            pane2.addEventListener('click', () => {
                this.activePane = 2;
                this.updatePaneStyles();
            });
        }
    }
    
    updatePaneStyles() {
        const pane1 = document.getElementById('editorPane1');
        const pane2 = document.getElementById('editorPane2');
        
        if (pane1) {
            pane1.style.boxShadow = this.activePane === 1 ? 
                'inset 0 0 0 2px var(--accent)' : 'none';
        }
        if (pane2) {
            pane2.style.boxShadow = this.activePane === 2 ? 
                'inset 0 0 0 2px var(--accent)' : 'none';
        }
    }
    
    openFileInPane(filePath, content, paneNum = null) {
        if (!this.isActive) return;
        
        // Use active pane if not specified
        const targetPane = paneNum || this.activePane;
        const editor = this.monacoEditors.get(targetPane);
        
        if (editor) {
            // Get or create model for this file
            const modelKey = `${targetPane}-${filePath}`;
            let model = this.monacoModels.get(modelKey);
            
            if (!model) {
                const ext = filePath.split('.').pop().toLowerCase();
                const language = this.getLanguageFromExt(ext);
                model = monaco.editor.createModel(content, language);
                this.monacoModels.set(modelKey, model);
            } else {
                model.setValue(content);
            }
            
            editor.setModel(model);
            this.paneFiles.set(targetPane, filePath);
            this.updatePaneTabs(targetPane);
            
            // Update active pane
            this.activePane = targetPane;
            this.updatePaneStyles();
        }
    }
    
    updatePaneTabs(paneNum) {
        // For pane 2, update the file info in header instead of tabs
        if (paneNum === 2) {
            const fileInfo = document.getElementById('pane2FileInfo');
            const closeBtn = document.getElementById('pane2CloseBtn');
            const activeFile = this.paneFiles.get(2);
            
            if (fileInfo) {
                if (activeFile) {
                    const fileName = activeFile.split('/').pop();
                    fileInfo.textContent = fileName;
                    fileInfo.style.color = 'var(--text-primary)';
                    if (closeBtn) {
                        closeBtn.style.display = 'block';
                    }
                } else {
                    fileInfo.textContent = 'No file open';
                    fileInfo.style.color = 'var(--text-secondary)';
                    if (closeBtn) {
                        closeBtn.style.display = 'none';
                    }
                }
            }
        }
        // Pane 1 uses the main editor tabs, no update needed here
    }
    
    closeFileInPane(paneNum) {
        const filePath = this.paneFiles.get(paneNum);
        if (filePath) {
            // Dispose the model
            const modelKey = `${paneNum}-${filePath}`;
            const model = this.monacoModels.get(modelKey);
            if (model) {
                model.dispose();
                this.monacoModels.delete(modelKey);
            }
            
            this.paneFiles.delete(paneNum);
            const editor = this.monacoEditors.get(paneNum);
            if (editor) {
                // Create a new welcome model
                const welcomeModel = monaco.editor.createModel(
                    `// Pane ${paneNum}\n// Open a file from the tree\n`,
                    'javascript'
                );
                editor.setModel(welcomeModel);
            }
            this.updatePaneTabs(paneNum);
        }
    }
    
    swapPanes() {
        if (!this.isActive) return;
        
        const file1 = this.paneFiles.get(1);
        const file2 = this.paneFiles.get(2);
        const editor1 = this.editors.get(1);
        const editor2 = this.editors.get(2);
        
        if (editor1 && editor2) {
            const content1 = editor1.getValue();
            const content2 = editor2.getValue();
            
            editor1.setValue(content2);
            editor2.setValue(content1);
            
            if (file1) this.paneFiles.set(2, file1);
            if (file2) this.paneFiles.set(1, file2);
            
            this.updatePaneTabs(1);
            this.updatePaneTabs(2);
        }
    }
    
    createMonacoEditor(container, paneNum) {
        // Create Monaco editor instance for a pane
        const editor = monaco.editor.create(container, {
            value: `// Pane ${paneNum}\n// Open a file from the tree\n`,
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
        editor.onDidChangeModelContent(() => {
            const activeFile = this.paneFiles.get(paneNum);
            if (activeFile && this.app.onEditorChange) {
                // Update the app's file data
                const fileData = this.app.openFiles.get(activeFile);
                if (fileData) {
                    fileData.content = editor.getValue();
                    fileData.modified = true;
                }
            }
        });
        
        return editor;
    }
    
    getLanguageFromExt(ext) {
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
        return LANGUAGE_MAP[ext] || 'plaintext';
    }
    
    async loadPane2Servers() {
        try {
            const servers = await window.go.main.App.ListServers();
            const select = document.getElementById('pane2ServerSelect');
            if (!select) return;
            
            // Clear existing options
            select.innerHTML = '';
            
            // Add "Same as Main" option with gradient style
            const sameOption = document.createElement('option');
            sameOption.value = 'same';
            sameOption.textContent = '🔗 Same as Main';
            sameOption.selected = true;
            select.appendChild(sameOption);
            
            // Add separator
            const separator = document.createElement('option');
            separator.disabled = true;
            separator.textContent = '────────────';
            select.appendChild(separator);
            
            // Get current main server
            const config = await window.go.main.App.GetConfig();
            const currentServerID = config.serverID;
            
            // Add server options
            servers.forEach(server => {
                if (server.id !== currentServerID) {
                    const option = document.createElement('option');
                    option.value = server.id;
                    option.textContent = `🖥️ ${server.name}`;
                    select.appendChild(option);
                }
            });
            
            // Style the select based on selection
            this.updateServerSelectStyle(select);
            
        } catch (err) {
            console.error('Failed to load servers for pane 2:', err);
        }
    }
    
    updateServerSelectStyle(select) {
        if (select.value === 'same') {
            select.style.borderColor = 'var(--accent)';
            select.style.color = 'var(--text-primary)';
        } else {
            select.style.borderColor = '#f5576c';
            select.style.color = '#f5576c';
        }
    }
    
    async switchPane2Server(serverID) {
        const select = document.getElementById('pane2ServerSelect');
        if (select) {
            this.updateServerSelectStyle(select);
        }
        
        if (serverID === 'same' || !serverID) {
            this.pane2Server = null;
            // Clear any pane 2 specific content
            const editor = this.monacoEditors.get(2);
            if (editor) {
                const welcomeModel = monaco.editor.createModel(
                    `// Pane 2 - Using main server\n// Files from the main server will open here\n`,
                    'javascript'
                );
                editor.setModel(welcomeModel);
            }
            this.updatePaneTabs(2);
            
            // Show notification
            this.showNotification('Pane 2 using main server', 'info');
        } else {
            this.pane2Server = serverID;
            
            // Show loading in editor
            const editor = this.monacoEditors.get(2);
            if (editor) {
                const loadingModel = monaco.editor.createModel(
                    `// Loading files from different server...\n// Server ID: ${serverID}\n`,
                    'javascript'
                );
                editor.setModel(loadingModel);
            }
            
            // Show notification
            this.showNotification(`Pane 2 switched to server: ${serverID}`, 'success');
            
            // Note: When user clicks a file, we'll check if pane2Server is set
            // and fetch from that server instead
        }
    }
    
    showNotification(message, type = 'info') {
        // Create a temporary notification
        const notification = document.createElement('div');
        notification.style.cssText = `
            position: fixed;
            bottom: 20px;
            right: 20px;
            padding: 12px 20px;
            background: ${type === 'success' ? 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)' : 
                          type === 'error' ? 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)' :
                          'linear-gradient(135deg, #89f7fe 0%, #66a6ff 100%)'};
            color: white;
            border-radius: 8px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            font-size: 14px;
            font-weight: 500;
            z-index: 10000;
            animation: slideIn 0.3s ease;
        `;
        notification.textContent = message;
        
        // Add animation
        const style = document.createElement('style');
        style.textContent = `
            @keyframes slideIn {
                from { transform: translateX(100%); opacity: 0; }
                to { transform: translateX(0); opacity: 1; }
            }
            @keyframes slideOut {
                from { transform: translateX(0); opacity: 1; }
                to { transform: translateX(100%); opacity: 0; }
            }
        `;
        document.head.appendChild(style);
        
        document.body.appendChild(notification);
        
        // Remove after 3 seconds
        setTimeout(() => {
            notification.style.animation = 'slideOut 0.3s ease';
            setTimeout(() => {
                notification.remove();
                style.remove();
            }, 300);
        }, 3000);
    }
    
    async openFileInPane2FromDifferentServer(filePath) {
        if (!this.pane2Server) return;
        
        try {
            // Temporarily switch server context
            const originalServer = await window.go.main.App.GetConfig();
            await window.go.main.App.SwitchServer(this.pane2Server);
            
            // Get file content from the different server
            const content = await window.go.main.App.GetFileContent(filePath);
            
            // Switch back to original server
            await window.go.main.App.SwitchServer(originalServer.serverID);
            
            // Open in pane 2
            this.openFileInPane(filePath, content, 2);
            
            // Store in pane2Files cache
            this.pane2Files.set(filePath, content);
            
        } catch (err) {
            console.error('Failed to load file from different server:', err);
            this.showNotification(`Failed to load file: ${err}`, 'error');
        }
    }
    
    createPane2Header() {
        const container = document.createElement('div');
        container.className = 'pane2-header';
        container.style.cssText = `
            display: flex;
            align-items: center;
            padding: 8px 12px;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--bg-tertiary);
            min-height: 40px;
            gap: 12px;
        `;
        
        // Add server selector
        const serverControl = document.createElement('div');
        serverControl.style.cssText = `
            display: flex;
            align-items: center;
            gap: 8px;
        `;
        
        const serverLabel = document.createElement('span');
        serverLabel.style.cssText = `
            font-size: 12px;
            color: var(--text-secondary);
            font-weight: 500;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        `;
        serverLabel.textContent = 'SERVER:';
        
        const serverSelect = document.createElement('select');
        serverSelect.id = 'pane2ServerSelect';
        serverSelect.style.cssText = `
            background: var(--bg-tertiary);
            color: var(--text-primary);
            border: 1px solid var(--border);
            padding: 4px 10px;
            border-radius: 4px;
            font-size: 13px;
            cursor: pointer;
            outline: none;
            transition: all 0.2s ease;
            min-width: 150px;
        `;
        
        // Add hover effect
        serverSelect.addEventListener('mouseenter', () => {
            serverSelect.style.borderColor = 'var(--accent)';
        });
        
        serverSelect.addEventListener('mouseleave', () => {
            serverSelect.style.borderColor = this.pane2Server ? '#f5576c' : 'var(--border)';
        });
        
        serverSelect.addEventListener('change', (e) => {
            this.switchPane2Server(e.target.value);
        });
        
        serverControl.appendChild(serverLabel);
        serverControl.appendChild(serverSelect);
        container.appendChild(serverControl);
        
        // Add file name display
        const fileInfo = document.createElement('div');
        fileInfo.id = 'pane2FileInfo';
        fileInfo.style.cssText = `
            flex: 1;
            text-align: center;
            color: var(--text-secondary);
            font-size: 13px;
        `;
        fileInfo.textContent = 'No file open';
        container.appendChild(fileInfo);
        
        // Add close button for file
        const closeBtn = document.createElement('button');
        closeBtn.id = 'pane2CloseBtn';
        closeBtn.style.cssText = `
            background: transparent;
            border: none;
            color: var(--text-secondary);
            cursor: pointer;
            padding: 4px 8px;
            font-size: 16px;
            display: none;
        `;
        closeBtn.textContent = '×';
        closeBtn.onclick = () => this.closeFileInPane(2);
        container.appendChild(closeBtn);
        
        // Populate servers
        this.loadPane2Servers();
        
        return container;
    }
    
    createPane(paneNum, title) {
        const pane = document.createElement('div');
        pane.id = `pane${paneNum}`;
        pane.style.cssText = `
            flex: 1;
            display: flex;
            flex-direction: row;
            overflow: hidden;
            background: var(--bg-primary);
        `;
        
        // Create file tree container
        const fileTreeContainer = document.createElement('div');
        fileTreeContainer.className = `file-tree-pane${paneNum}`;
        fileTreeContainer.style.cssText = `
            width: 250px;
            min-width: 150px;
            background: var(--bg-secondary);
            border-right: 1px solid var(--bg-tertiary);
            display: flex;
            flex-direction: column;
            overflow: hidden;
        `;
        
        // Create header with server selector
        const header = document.createElement('div');
        header.style.cssText = `
            padding: 10px;
            background: var(--bg-tertiary);
            border-bottom: 1px solid var(--border);
            display: flex;
            flex-direction: column;
            gap: 8px;
        `;
        
        // Title
        const titleDiv = document.createElement('div');
        titleDiv.style.cssText = `
            font-size: 12px;
            font-weight: 600;
            color: var(--text-primary);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        `;
        titleDiv.textContent = title;
        header.appendChild(titleDiv);
        
        // Server selector for pane 2
        if (paneNum === 2) {
            const serverSelect = document.createElement('select');
            serverSelect.id = `pane${paneNum}ServerSelect`;
            serverSelect.style.cssText = `
                width: 100%;
                background: var(--bg-secondary);
                color: var(--text-primary);
                border: 1px solid var(--border);
                padding: 6px;
                border-radius: 4px;
                font-size: 12px;
                cursor: pointer;
                outline: none;
            `;
            serverSelect.addEventListener('change', (e) => {
                this.switchPaneServer(paneNum, e.target.value);
            });
            header.appendChild(serverSelect);
            
            // Load servers later
            setTimeout(() => this.loadPaneServers(paneNum), 100);
        }
        
        fileTreeContainer.appendChild(header);
        
        // Create file tree
        const fileTree = document.createElement('div');
        fileTree.id = `fileTree${paneNum}`;
        fileTree.className = 'file-tree';
        fileTree.style.cssText = `
            flex: 1;
            overflow-y: auto;
            padding: 10px;
        `;
        fileTree.innerHTML = '<div class="loading">Loading files...</div>';
        fileTreeContainer.appendChild(fileTree);
        
        // Create editor container
        const editorContainer = document.createElement('div');
        editorContainer.style.cssText = `
            flex: 1;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        `;
        
        // Create tabs header
        const tabsHeader = document.createElement('div');
        tabsHeader.style.cssText = `
            display: flex;
            align-items: center;
            padding: 8px 12px;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--bg-tertiary);
            min-height: 40px;
        `;
        
        const fileName = document.createElement('div');
        fileName.id = `pane${paneNum}FileName`;
        fileName.style.cssText = `
            flex: 1;
            font-size: 13px;
            color: var(--text-secondary);
        `;
        fileName.textContent = 'No file open';
        tabsHeader.appendChild(fileName);
        
        const closeBtn = document.createElement('button');
        closeBtn.style.cssText = `
            background: transparent;
            border: none;
            color: var(--text-secondary);
            cursor: pointer;
            padding: 4px 8px;
            font-size: 16px;
            display: none;
        `;
        closeBtn.textContent = '×';
        closeBtn.onclick = () => this.closeFileInPane(paneNum);
        closeBtn.id = `pane${paneNum}CloseBtn`;
        tabsHeader.appendChild(closeBtn);
        
        editorContainer.appendChild(tabsHeader);
        
        // Create Monaco editor container
        const monacoContainer = document.createElement('div');
        monacoContainer.id = `editor${paneNum}`;
        monacoContainer.style.cssText = `
            flex: 1;
            overflow: hidden;
        `;
        editorContainer.appendChild(monacoContainer);
        
        // Assemble pane
        pane.appendChild(fileTreeContainer);
        pane.appendChild(editorContainer);
        
        // Setup mouse navigation for this pane's file tree
        this.setupMouseNavigation(fileTreeContainer, paneNum);
        
        return pane;
    }
    
    createSplitter(orientation, isCenter = false) {
        const splitter = document.createElement('div');
        splitter.className = isCenter ? 'center-splitter' : 'pane-splitter';
        splitter.style.cssText = `
            background: ${isCenter ? 'var(--accent)' : 'var(--bg-tertiary)'};
            ${orientation === 'vertical' ? 'width: 4px; cursor: col-resize;' : 'height: 4px; cursor: row-resize;'}
            user-select: none;
            transition: background 0.2s;
        `;
        
        splitter.addEventListener('mouseenter', () => {
            splitter.style.background = 'var(--accent)';
        });
        
        splitter.addEventListener('mouseleave', () => {
            splitter.style.background = isCenter ? 'var(--accent)' : 'var(--bg-tertiary)';
        });
        
        return splitter;
    }
    
    setupCenterSplitterDrag(splitter, container) {
        let isDragging = false;
        let startX = 0;
        let startRatio = 0.5;
        
        splitter.addEventListener('mousedown', (e) => {
            isDragging = true;
            startX = e.clientX;
            startRatio = this.splitRatio;
            document.body.style.cursor = 'col-resize';
            e.preventDefault();
        });
        
        document.addEventListener('mousemove', (e) => {
            if (!isDragging) return;
            
            const rect = container.getBoundingClientRect();
            const deltaX = e.clientX - startX;
            const newRatio = startRatio + (deltaX / rect.width);
            
            this.splitRatio = Math.max(0.2, Math.min(0.8, newRatio));
            
            const pane1 = document.getElementById('pane1');
            const pane2 = document.getElementById('pane2');
            if (pane1 && pane2) {
                pane1.style.flex = this.splitRatio;
                pane2.style.flex = 1 - this.splitRatio;
            }
        });
        
        document.addEventListener('mouseup', () => {
            if (isDragging) {
                isDragging = false;
                document.body.style.cursor = '';
            }
        });
    }
    
    async initializePaneEditors() {
        // Initialize Monaco editors for both panes
        const editor1Container = document.getElementById('editor1');
        const editor2Container = document.getElementById('editor2');
        
        if (editor1Container) {
            const editor1 = this.createMonacoEditor(editor1Container, 1);
            this.monacoEditors.set(1, editor1);
        }
        
        if (editor2Container) {
            const editor2 = this.createMonacoEditor(editor2Container, 2);
            this.monacoEditors.set(2, editor2);
        }
        
        // Load files for pane 1 (main server)
        await this.loadPaneFiles(1, '/');
    }
    
    async loadPaneServers(paneNum) {
        if (paneNum !== 2) return;
        
        try {
            const servers = await window.go.main.App.ListServers();
            const select = document.getElementById(`pane${paneNum}ServerSelect`);
            if (!select) return;
            
            select.innerHTML = '';
            
            // Add "Same as Main" option
            const sameOption = document.createElement('option');
            sameOption.value = 'same';
            sameOption.textContent = 'Same as Main';
            sameOption.selected = true;
            select.appendChild(sameOption);
            
            // Get current main server
            const config = await window.go.main.App.GetConfig();
            const currentServerID = config.serverID;
            
            // Add other servers
            servers.forEach(server => {
                const option = document.createElement('option');
                option.value = server.id;
                option.textContent = server.name;
                if (server.id === currentServerID) {
                    option.textContent += ' (Main)';
                }
                select.appendChild(option);
            });
        } catch (err) {
            console.error('Failed to load servers:', err);
        }
    }
    
    async switchPaneServer(paneNum, serverID) {
        if (paneNum === 2) {
            if (serverID === 'same') {
                this.pane2Server = null;
            } else {
                this.pane2Server = serverID;
            }
            
            // Reload files for pane 2
            await this.loadPaneFiles(2, '/');
        }
    }
    
    async loadPaneFiles(paneNum, path, addToHistory = true) {
        const fileTree = document.getElementById(`fileTree${paneNum}`);
        if (!fileTree) return;
        
        fileTree.innerHTML = '<div class="loading">Loading files...</div>';
        
        try {
            let files;
            
            if (paneNum === 2 && this.pane2Server) {
                // Load from different server
                const originalServer = await window.go.main.App.GetConfig();
                await window.go.main.App.SwitchServer(this.pane2Server);
                files = await window.go.main.App.ListFiles(path);
                await window.go.main.App.SwitchServer(originalServer.serverID);
            } else {
                // Load from main server
                files = await window.go.main.App.ListFiles(path);
            }
            
            this.renderPaneFiles(paneNum, files || [], path);
            
            // Update navigation history
            if (addToHistory) {
                this.addToNavigationHistory(paneNum, path);
            }
            
            // Update navigation button states (if they exist)
            this.updateNavigationButtons(paneNum);
        } catch (err) {
            fileTree.innerHTML = `<div class="error">Failed to load files: ${err}</div>`;
        }
    }
    
    renderPaneFiles(paneNum, files, currentPath) {
        const fileTree = document.getElementById(`fileTree${paneNum}`);
        if (!fileTree) return;
        
        fileTree.innerHTML = '';
        
        // Sort files
        files.sort((a, b) => {
            if (a.isDir !== b.isDir) return b.isDir ? 1 : -1;
            return a.name.localeCompare(b.name);
        });
        
        // Add parent directory if not root
        if (currentPath !== '/') {
            const parentDiv = this.createFileItem({ name: '..', isDir: true }, paneNum, currentPath, true);
            fileTree.appendChild(parentDiv);
        }
        
        // Render files
        files.forEach(file => {
            const item = this.createFileItem(file, paneNum, currentPath);
            fileTree.appendChild(item);
        });
    }
    
    createFileItem(file, paneNum, currentPath, isParent = false) {
        const div = document.createElement('div');
        div.className = 'file-item';
        div.style.cssText = `
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 4px 8px;
            cursor: pointer;
            font-size: 13px;
        `;
        
        div.addEventListener('mouseenter', () => {
            div.style.background = 'var(--bg-tertiary)';
        });
        
        div.addEventListener('mouseleave', () => {
            div.style.background = '';
        });
        
        const icon = document.createElement('span');
        icon.textContent = file.isDir ? '📁' : '📄';
        div.appendChild(icon);
        
        const name = document.createElement('span');
        name.style.flex = '1';
        name.textContent = file.name;
        div.appendChild(name);
        
        div.addEventListener('click', async () => {
            if (isParent) {
                const parts = currentPath.split('/').filter(p => p);
                parts.pop();
                const parentPath = '/' + parts.join('/');
                await this.loadPaneFiles(paneNum, parentPath || '/', true);
            } else if (file.isDir) {
                const newPath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name;
                await this.loadPaneFiles(paneNum, newPath, true);
            } else {
                // Open file
                const filePath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name;
                await this.openFileInPaneFromTree(paneNum, filePath, file.name);
            }
        });
        
        return div;
    }
    
    async openFileInPaneFromTree(paneNum, filePath, fileName) {
        try {
            let content;
            
            if (paneNum === 2 && this.pane2Server) {
                // Load from different server
                const originalServer = await window.go.main.App.GetConfig();
                await window.go.main.App.SwitchServer(this.pane2Server);
                content = await window.go.main.App.GetFileContent(filePath);
                await window.go.main.App.SwitchServer(originalServer.serverID);
            } else {
                // Load from main server
                content = await window.go.main.App.GetFileContent(filePath);
            }
            
            // Open in editor
            const editor = this.monacoEditors.get(paneNum);
            if (editor) {
                const ext = fileName.split('.').pop().toLowerCase();
                const language = this.getLanguageFromExt(ext);
                const model = monaco.editor.createModel(content, language);
                editor.setModel(model);
                
                // Update file name display
                const fileNameEl = document.getElementById(`pane${paneNum}FileName`);
                const closeBtn = document.getElementById(`pane${paneNum}CloseBtn`);
                if (fileNameEl) {
                    fileNameEl.textContent = fileName;
                    fileNameEl.style.color = 'var(--text-primary)';
                }
                if (closeBtn) {
                    closeBtn.style.display = 'block';
                }
                
                this.paneFiles.set(paneNum, filePath);
            }
        } catch (err) {
            console.error('Failed to open file:', err);
            this.showNotification(`Failed to open file: ${err}`, 'error');
        }
    }
    
    toggleOrientation() {
        if (!this.isActive) return;
        
        this.orientation = this.orientation === 'vertical' ? 'horizontal' : 'vertical';
        
        const splitContainer = document.querySelector('.split-editor-container');
        const splitter = document.querySelector('.editor-splitter');
        
        if (splitContainer) {
            splitContainer.style.flexDirection = this.orientation === 'vertical' ? 'row' : 'column';
        }
        
        if (splitter) {
            if (this.orientation === 'vertical') {
                splitter.style.width = '4px';
                splitter.style.height = '';
                splitter.style.cursor = 'col-resize';
            } else {
                splitter.style.width = '';
                splitter.style.height = '4px';
                splitter.style.cursor = 'row-resize';
            }
        }
    }
    
    // Navigation history methods
    addToNavigationHistory(paneNum, path) {
        const nav = this.navigationHistory.get(paneNum);
        if (!nav) return;
        
        // If we're not at the end of history, remove everything after current index
        if (nav.currentIndex < nav.history.length - 1) {
            nav.history = nav.history.slice(0, nav.currentIndex + 1);
        }
        
        // Add new path if it's different from current
        if (nav.history[nav.currentIndex] !== path) {
            nav.history.push(path);
            nav.currentIndex++;
            
            // Limit history to 50 items
            if (nav.history.length > 50) {
                nav.history.shift();
                nav.currentIndex--;
            }
        }
    }
    
    async navigateBack(paneNum) {
        const nav = this.navigationHistory.get(paneNum);
        if (!nav || nav.currentIndex <= 0) return;
        
        nav.currentIndex--;
        const path = nav.history[nav.currentIndex];
        await this.loadPaneFiles(paneNum, path, false);
    }
    
    async navigateForward(paneNum) {
        const nav = this.navigationHistory.get(paneNum);
        if (!nav || nav.currentIndex >= nav.history.length - 1) return;
        
        nav.currentIndex++;
        const path = nav.history[nav.currentIndex];
        await this.loadPaneFiles(paneNum, path, false);
    }
    
    updateNavigationButtons(paneNum) {
        const nav = this.navigationHistory.get(paneNum);
        if (!nav) return;
        
        // This can be used to update navigation buttons if they exist in the UI
        const backBtn = document.getElementById(`pane${paneNum}BackBtn`);
        const forwardBtn = document.getElementById(`pane${paneNum}ForwardBtn`);
        
        if (backBtn) {
            backBtn.disabled = nav.currentIndex <= 0;
            backBtn.style.opacity = backBtn.disabled ? '0.5' : '1';
        }
        
        if (forwardBtn) {
            forwardBtn.disabled = nav.currentIndex >= nav.history.length - 1;
            forwardBtn.style.opacity = forwardBtn.disabled ? '0.5' : '1';
        }
    }
    
    setupMouseNavigation(container, paneNum) {
        // Listen for mouse button 4 (back) and 5 (forward)
        container.addEventListener('mousedown', async (e) => {
            // Mouse button 3 is back (some browsers)
            if (e.button === 3) {
                e.preventDefault();
                await this.navigateBack(paneNum);
            }
            // Mouse button 4 is forward (some browsers)
            else if (e.button === 4) {
                e.preventDefault();
                await this.navigateForward(paneNum);
            }
        });
        
        // Also handle auxclick for mouse buttons 4 and 5
        container.addEventListener('auxclick', async (e) => {
            // Prevent default middle-click behavior
            e.preventDefault();
            
            // Note: Browser support varies - test needed to confirm button mapping
            // Typically button 3 = back, button 4 = forward
            if (e.button === 3) {
                await this.navigateBack(paneNum);
            } else if (e.button === 4) {
                await this.navigateForward(paneNum);
            }
        });
    }
}

// Export globally
window.IntegratedSplitView = IntegratedSplitView;
