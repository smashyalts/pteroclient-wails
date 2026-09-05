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
        // selection holds every picked row as path -> entry. Each entry
        // carries the full remote path it was rendered with, so a later
        // navigation cannot repoint a stale selection at a same-named file in
        // another directory. Deleting used to rebuild the path from
        // currentPath at click time, which did exactly that.
        selection: new Map(),
        // The row clicked last. Anchors shift-select, and is what the context
        // menu and the single-target actions act on.
        selectedFile: null,
        contextFile: null,
        // Rendered order, so a shift-click knows what lies between two rows.
        renderedRows: [],
        // 'left' | 'right' | 'bottom' | 'hidden'
        treeDock: 'left',
        // Console command history, oldest first.
        commandHistory: [],
        commandCursor: -1,
        // Live filter over the rows already rendered for this folder.
        filterQuery: '',
        // Set while showing the results of a recursive search instead of a
        // folder listing.
        searchResults: null,
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

        // Console reconnect state. consoleWanted is what was asked for, as
        // opposed to consoleConnected, which is what is actually true.
        consoleWanted: false,
        consoleRetries: 0,
        consoleRetryTimer: null,
        
        async init() {
            console.log('App init started');
            this.setupEditor();
            this.setupEventListeners();
            this.setupKeyboardShortcuts();
            this.setupMouseNavigation();
            this.setupDropUpload();
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
        
        // Shortcuts live in commands.js, driven by the manager in ux.js.
        // They used to be here, in split-view.js and inside Monaco all at once,
        // which is how Ctrl+S in a split pane also saved the main editor's file:
        // nothing could see the whole set.
        setupKeyboardShortcuts() {
            this.restoreCommandHistory();
            this.restoreTreeDock();
            this.restoreFileDetails();
            this.wireConsoleLinks();
            this.wireTreeEvents();
        },

        restoreCommandHistory() {
            try {
                const raw = localStorage.getItem('consoleHistory');
                this.commandHistory = raw ? JSON.parse(raw) : [];
            } catch (err) {
                this.commandHistory = [];
            }
            if (!Array.isArray(this.commandHistory)) this.commandHistory = [];
            this.commandCursor = this.commandHistory.length;
        },

        rememberCommand(command) {
            // Repeating the last command should not grow the list.
            if (this.commandHistory[this.commandHistory.length - 1] !== command) {
                this.commandHistory.push(command);
            }
            if (this.commandHistory.length > 200) {
                this.commandHistory = this.commandHistory.slice(-200);
            }
            this.commandCursor = this.commandHistory.length;
            try {
                localStorage.setItem('consoleHistory', JSON.stringify(this.commandHistory));
            } catch (err) { /* private mode */ }
        },

        // Up and down walk the history the way a shell does: the draft you were
        // typing is kept, so stepping back and forward again returns it.
        recallCommand(delta) {
            const input = document.getElementById('commandInput');
            if (!input || !this.commandHistory.length) return;

            if (this.commandCursor === this.commandHistory.length) this.commandDraft = input.value;

            const next = Math.min(this.commandHistory.length, Math.max(0, this.commandCursor + delta));
            if (next === this.commandCursor) return;
            this.commandCursor = next;

            input.value = next === this.commandHistory.length
                ? (this.commandDraft || '')
                : this.commandHistory[next];

            // Caret to the end, so editing a recalled command is immediate.
            requestAnimationFrame(() => input.setSelectionRange(input.value.length, input.value.length));
        },
        
        setupEventListeners() {
            // Backend events
            window.runtime.EventsOn('connected', (connected) => {
                console.log('Connection event:', connected);
                this.isConnected = connected;
                this.updateStatus(connected);
                if (connected) {
                    this.loadFiles('/');
                    this.ensureConsole();
                }
            });
            
            window.runtime.EventsOn('console-output', (message) => {
                this.appendConsole(message);
            });

            window.runtime.EventsOn('search-progress', (update) => {
                this.onSearchProgress(update);
            });
            
            window.runtime.EventsOn('console-error', (error) => {
                this.appendConsole('[ERROR] ' + error, 'error');
            });
            
            window.runtime.EventsOn('console-connected', (connected) => {
                console.log('Console connected:', connected);
                this.consoleConnected = connected;
                this.paintConnectButton();

                if (connected) {
                    // A connection that came up cancels whatever retry was
                    // pending and resets the count, so the next drop gets a
                    // full set of attempts rather than the tail of this one.
                    this.consoleRetries = 0;
                    this.cancelConsoleRetry();
                } else if (this.consoleWanted) {
                    // Dropped while the console was meant to be up - usually an
                    // expired token, sometimes the network. Get it back.
                    this.scheduleConsoleReconnect();
                }
                if (connected) {
                    this.appendConsole('=== Console connected ===', 'info');
                }
            });
            
            window.runtime.EventsOn('server-changed', (serverID) => {
                console.log('Server changed to:', serverID);

                // Where we were, before the reset below throws it away.
                const wasAt = this.currentPath || '/';

                this.navigationHistory = ['/'];
                this.navigationIndex = 0;
                // Clear console and reload files for the new server
                this.clearConsole();
                this.appendConsole('=== Switched to server: ' + serverID + ' ===', 'info');
                // Close all open files as they belong to the previous server
                this.closeAllFiles();
                this.loadNearest(wasAt);
                // SwitchServer drops the old console; bring the new one up
                // rather than making the Connect button a required step.
                //
                // Whether one is up is the Go side's answer, not a flag set
                // here. Forcing it false asked for a second connection over a
                // socket that was already the new server's — two sockets, two
                // copies of every line.
                this.refreshConsoleState().then(() => this.ensureConsole());
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
            const filterInput = document.getElementById('fileFilter');
            if (filterInput) {
                // input, not keyup: it fires for paste and for held keys too.
                filterInput.addEventListener('input', () => this.setFilter(filterInput.value));
                filterInput.addEventListener('keydown', (e) => {
                    if (e.key === 'Enter') {
                        e.preventDefault();
                        return this.searchTree(filterInput.value);
                    }
                    if (e.key !== 'Escape') return;
                    e.stopPropagation();
                    filterInput.value = '';
                    this.setFilter('');
                    filterInput.blur();
                });
            }
            const filterClear = document.getElementById('fileFilterClear');
            if (filterClear) {
                filterClear.addEventListener('click', () => {
                    const box = document.getElementById('fileFilter');
                    box.value = '';
                    this.setFilter('');
                    box.focus();
                });
            }

            const commandInput = document.getElementById('commandInput');
            if (commandInput) {
                commandInput.addEventListener('keydown', (e) => {
                    if (e.key === 'ArrowUp') {
                        e.preventDefault();
                        return this.recallCommand(-1);
                    }
                    if (e.key === 'ArrowDown') {
                        e.preventDefault();
                        return this.recallCommand(1);
                    }
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

            // And drops the filter, or the folder you just opened comes up
            // looking empty because of a word you typed somewhere else.
            const filterInput = document.getElementById('fileFilter');
            if (filterInput) filterInput.value = '';
            this.filterQuery = '';

            // Add to navigation history
            this.addToHistory(path);
            
            const pathInput = document.getElementById('currentPath');
            if (pathInput) pathInput.value = path;

            // The visible path is the breadcrumb bar in the shell; the input
            // above is kept as the value other code still reads.
            document.dispatchEvent(new CustomEvent('path:changed', { detail: path }));
            
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            
            // Draw the last known contents straight away and refresh behind
            // them. Stepping back out of a folder used to blank the list and
            // say "Loading files..." for something already seen a moment ago.
            const cacheKey = (config.serverID || '') + '\u0000' + path;
            const cached = this.dirCacheGet(cacheKey);
            const token = (this.listToken = (this.listToken || 0) + 1);

            // Only ever blank for a folder never seen before. A refresh, or
            // stepping back into somewhere already visited, keeps what is on
            // screen and swaps it out underneath — including the scroll
            // position, which a redraw would have thrown away.
            const sameFolder = this.lastRenderedPath === path;
            if (cached) {
                this.renderFiles(cached.slice(), { keepScroll: sameFolder });
                tree.classList.add('stale');
            } else if (sameFolder && this.renderedRows && this.renderedRows.length) {
                // No cache, but this is a refresh of the folder already drawn.
                tree.classList.add('stale');
            } else {
                tree.innerHTML = '<div class="loading">Loading files...</div>';
            }

            try {
                const files = await window.go.main.App.ListFiles(path);
                // A slower earlier request must not overwrite a newer folder.
                if (token !== this.listToken) return;
                const wasAt = this.lastRenderedPath === path;
                this.dirCachePut(cacheKey, files || []);
                // Ctrl+K can only offer a folder it has heard of, and this is
                // where it hears about them - including subfolders nobody has
                // opened yet.
                if (window.Folders) window.Folders.remember(config.serverID, path, files);
                this.renderFiles(files || [], { keepScroll: wasAt });
                tree.classList.remove('stale');
            } catch (err) {
                if (token !== this.listToken) return;
                console.error('Failed to load files:', err);
                tree.classList.remove('stale');
                if (cached) {
                    // The cached list is still on screen, which beats replacing
                    // something readable with an error.
                    window.UX.toast.bad('Could not refresh this folder: ' + err);
                } else {
                    tree.innerHTML = '<div class="error">Failed to load files: ' + err + '</div>';
                }
            }
        },

        /* ------------------------------------------------- directory cache */

        // Bounded by entries rather than bytes: a listing is names and sizes,
        // and 120 folders of them is a few hundred KB at worst. Oldest use
        // goes first, so walking a deep tree does not evict the top of it.
        dirCacheGet(key) {
            if (!this.dirCache) this.dirCache = new Map();
            const hit = this.dirCache.get(key);
            if (!hit) return null;
            if (Date.now() - hit.at > 5 * 60 * 1000) {
                this.dirCache.delete(key);
                return null;
            }
            // Touch, so this becomes the most recently used.
            this.dirCache.delete(key);
            this.dirCache.set(key, hit);
            return hit.files;
        },

        dirCachePut(key, files) {
            if (!this.dirCache) this.dirCache = new Map();
            const DIR_CACHE_MAX = 120;
            const ENTRY_MAX = 3000;
            if (files.length > ENTRY_MAX) {
                // A folder this size is slow to render from cache anyway, and
                // holding several of them is where the memory would go.
                this.dirCache.delete(key);
                return;
            }
            this.dirCache.delete(key);
            this.dirCache.set(key, { files: files.slice(), at: Date.now() });
            while (this.dirCache.size > DIR_CACHE_MAX) {
                this.dirCache.delete(this.dirCache.keys().next().value);
            }
        },

        // Anything that changes a folder makes its cached copy a lie.
        dirCacheForget(paths) {
            if (!this.dirCache) return;
            const dirs = new Set();
            (paths || []).forEach((p) => {
                dirs.add(p.slice(0, p.lastIndexOf('/')) || '/');
                dirs.add(p);
            });
            Array.from(this.dirCache.keys()).forEach((key) => {
                const path = key.slice(key.indexOf('\u0000') + 1);
                if (dirs.has(path)) this.dirCache.delete(key);
            });
        },

        dirCacheClear() {
            if (this.dirCache) this.dirCache.clear();
        },
        
        renderFiles(files, opts) {
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            // The listeners live on the tree, not on its contents, so emptying
            // it below costs nothing to re-establish.
            this.wireTreeEvents();

            // Where the list was, so a refresh does not jump back to the top.
            const keep = opts && opts.keepScroll;
            const wasScrolled = keep ? tree.scrollTop : 0;

            tree.innerHTML = '';
            this.renderedRows = [];
            
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

            this.applyFilter();
            this.updateSelectionUI();

            if (files.length === 0 && this.currentPath === '/') {
                tree.innerHTML = '<div class="preview-empty">No files found</div>';
            }

            this.lastRenderedPath = this.currentPath;
            if (keep && wasScrolled) {
                // After the rows exist, or there is nothing to scroll within.
                tree.scrollTop = Math.min(wasScrolled, tree.scrollHeight);
            }
        },

        /**
         * Hides rows that do not match. The parent-directory row always stays,
         * or filtering to nothing would trap you in the folder.
         */
        applyFilter() {
            const query = String(this.filterQuery || '').trim();
            const countEl = document.getElementById('fileFilterCount');
            const clearEl = document.getElementById('fileFilterClear');

            // Over the rows the render already built, not a fresh DOM query
            // with a textContent read and a toLowerCase per row per keystroke.
            // The parent row is not among them, so the way out always stays.
            const match = window.UX.matcher(query);

            let shown = 0;
            for (let i = 0; i < this.renderedRows.length; i++) {
                const row = this.renderedRows[i];
                const hit = !match || match.test(row.name, row.lower);
                // Only when it changes: assigning hidden is a style
                // invalidation whether or not the value is different.
                if (row.el.hidden === hit) row.el.hidden = !hit;
                if (hit) shown++;
            }

            const total = this.renderedRows.length;
            if (countEl) {
                countEl.hidden = !query;
                countEl.textContent = shown + ' of ' + total;
                countEl.classList.toggle('none', query && shown === 0);
            }
            if (clearEl) clearEl.hidden = !query;

            let none = document.getElementById('fileFilterEmpty');
            if (query && shown === 0) {
                if (!none) {
                    none = document.createElement('div');
                    none.id = 'fileFilterEmpty';
                    none.className = 'preview-empty';
                    document.getElementById('fileTree').appendChild(none);
                }
                none.textContent = 'Nothing in this folder matches "' + query + '"';
                none.hidden = false;
            } else if (none) {
                none.hidden = true;
            }
        },

        setFilter(query) {
            this.filterQuery = query || '';
            // Typing again goes back to filtering the folder; leaving the
            // results up while the word changed under them would be a lie.
            if (this.searchResults) {
                this.searchResults = null;
                this.refreshFiles();
                return;
            }
            this.applyFilter();
        },

        /**
         * Walks everything below the current folder for `query`.
         *
         * The panel has no search endpoint, so this is one listing request per
         * folder. The backend runs eight of them at once and reports matches as
         * it finds them, so results appear while the walk is still going rather
         * than all at the end.
         */
        async searchTree(query) {
            const needle = String(query || '').trim();
            if (!needle) return;

            const root = this.currentPath || '/';

            // An empty frame first, so there is somewhere for hits to land the
            // moment the first one arrives.
            this.searchResults = {
                query: needle, root: root, hits: [], folders: 0, scanned: 0,
                truncated: false, reason: '', running: true
            };
            this.renderSearchResults(this.searchResults);

            let result;
            try {
                result = await window.go.main.App.SearchFiles(root, needle);
            } catch (err) {
                if (this.searchResults) this.searchResults.running = false;
                this.updateSearchHead();
                await this.say('Search failed', String(err));
                return;
            }

            // The events carry the same hits, so this is a reconciliation
            // rather than a second render: whatever the run ended up with wins.
            this.searchResults = Object.assign({ running: false }, result);
            this.renderSearchResults(this.searchResults);

            if (!result.hits.length) {
                window.UX.toast.warn('Nothing under ' + root + ' matches "' + needle + '"');
            } else if (result.truncated) {
                window.UX.toast.warn(result.hits.length + ' found — ' + result.reason);
            } else {
                window.UX.toast.ok(result.hits.length + ' found in ' + result.folders + ' folder(s)');
            }
        },

        /**
         * Adds hits to the list as the walk turns them up.
         *
         * Rows are appended rather than the list being redrawn: a redraw on
         * every batch would throw away the scroll position of someone already
         * reading the results.
         */
        onSearchProgress(update) {
            const state = this.searchResults;
            if (!state || !state.running || !update) return;

            // A run this window is no longer waiting for.
            if (state.id !== undefined && update.id !== undefined && update.id !== state.id) return;
            if (state.id === undefined && update.id !== undefined) state.id = update.id;

            state.folders = update.folders || state.folders;
            state.scanned = update.scanned || state.scanned;

            const tree = document.getElementById('fileTree');
            (update.hits || []).forEach((hit) => {
                state.hits.push(hit);
                if (tree) tree.appendChild(this.searchRow(hit));
            });

            if (update.done) state.running = false;
            this.updateSearchHead();
        },

        /** One result row, shared by the live path and the final render. */
        searchRow(hit) {
            const row = this.createFileItem({
                name: hit.name,
                isDir: hit.is_dir,
                size: hit.size,
                path: hit.path
            });
            // The folder it was found in is the whole point of a search result,
            // so it replaces the size column's neighbours.
            const where = document.createElement('span');
            where.className = 'search-where mono';
            where.textContent = hit.dir;
            where.title = hit.path;
            row.insertBefore(where, row.querySelector('.file-size'));
            return row;
        },

        /** Keeps the counter above the list honest while the walk runs. */
        updateSearchHead() {
            const state = this.searchResults;
            const head = document.getElementById('searchCount');
            if (!state || !head) return;
            const n = state.hits.length;
            head.textContent = n + ' match' + (n === 1 ? '' : 'es') +
                (state.running ? ' so far · ' + (state.folders || 0) + ' folder(s) searched' : '');

            const spin = document.getElementById('searchSpin');
            if (spin) spin.hidden = !state.running;

            const warn = document.getElementById('searchWarn');
            if (warn) {
                // Whenever there is something to say, not only when the walk
                // was cut short: a search that skipped folders it could not
                // read has a hole in it and has to admit to it.
                warn.hidden = !state.reason;
                warn.textContent = state.reason || '';
            }

            const none = document.getElementById('searchNone');
            if (none) none.hidden = state.running || n > 0;
        },

        /** Draws search hits in place of the folder listing. */
        renderSearchResults(result) {
            const tree = document.getElementById('fileTree');
            if (!tree) return;

            tree.innerHTML = '';
            this.renderedRows = [];
            this.clearSelection();

            const head = document.createElement('div');
            head.className = 'search-head';
            head.innerHTML =
                '<span id="searchSpin" class="search-spin"' + (result.running ? '' : ' hidden') + '>' +
                (window.Icons ? window.Icons.svg('refresh', 'spin ic-14') : '') + '</span>' +
                '<span id="searchCount"></span>' +
                '<span>under <span class="mono">' + this.escapeHtml(result.root) + '</span></span>' +
                '<span class="search-warn" id="searchWarn" hidden></span>' +
                '<button class="sm" id="searchBackBtn" type="button">Back to the folder</button>';
            tree.appendChild(head);

            result.hits.forEach((hit) => tree.appendChild(this.searchRow(hit)));

            // Kept in the DOM rather than added later, so a walk that ends with
            // nothing does not have to redraw to say so.
            const none = document.createElement('div');
            none.id = 'searchNone';
            none.className = 'preview-empty';
            none.textContent = 'Nothing matched';
            none.hidden = true;
            tree.appendChild(none);

            this.updateSearchHead();

            const back = document.getElementById('searchBackBtn');
            if (back) {
                back.addEventListener('click', () => {
                    // Leaving the results stops the walk: eight workers listing
                    // folders for a list nobody is looking at is just load on
                    // somebody's panel.
                    if (this.searchResults && this.searchResults.running) {
                        window.go.main.App.CancelSearch();
                    }
                    this.searchResults = null;
                    const box = document.getElementById('fileFilter');
                    if (box) box.value = '';
                    this.filterQuery = '';
                    this.refreshFiles();
                });
            }
        },

        focusFilter() {
            const input = document.getElementById('fileFilter');
            if (!input) return;
            input.focus();
            input.select();
        },

        createFileItem(file, isParent = false) {
            const div = document.createElement('div');
            div.className = 'file-item';

            // Resolved once, at render time, and carried on the row. Every
            // action on this row uses it instead of recomputing from
            // currentPath, which may have moved on by then.
            // A search hit arrives with its own path from somewhere else in
            // the tree; everything else is relative to the folder on screen.
            const fullPath = isParent
                ? null
                : (file.path || (this.currentPath === '/' ? '/' + file.name : this.currentPath + '/' + file.name));
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

            // When it changed, short enough to fit a narrow pane, with the
            // full timestamp on hover.
            const date = document.createElement('span');
            date.className = 'file-date';
            if (!isParent) {
                date.textContent = this.formatWhen(file.modTime);
                date.title = this.formatWhenFull(file.modTime);
            }

            const mode = document.createElement('span');
            mode.className = 'file-mode';
            if (!isParent) mode.textContent = file.mode || '';

            if (!isParent) {
                name.title = fullPath + (file.isSymlink ? ' (symlink)' : '');
            }

            div.appendChild(icon);
            div.appendChild(name);
            div.appendChild(size);
            div.appendChild(date);
            div.appendChild(mode);
            
            // No listeners here. Nine of them per row meant 7,200 closures for
            // a folder of 800, which was 44 of the 56 ms it took to draw one —
            // the tree carries one set of each instead and finds the row by
            // index. See wireTreeEvents.
            if (isParent) {
                div.dataset.parent = '1';
            } else {
                div.setAttribute('draggable', 'true');
                div.dataset.index = String(this.renderedRows.length);
                // The lowercase name is kept with the row. Filtering used to
                // build it again for every row on every keystroke, which for a
                // folder of a few thousand is the whole cost of typing.
                this.renderedRows.push({
                    path: fullPath, entry: entry, el: div, file: file,
                    name: file.name, lower: String(file.name || '').toLowerCase()
                });
            }

            return div;
        },

        /**
         * Everything the file rows react to, on the tree rather than the rows.
         *
         * Wired once. A row is found from the event's target and looked up by
         * the index it carries, so the handlers do not close over anything
         * per-row and drawing a folder is only building elements.
         */
        wireTreeEvents() {
            const tree = document.getElementById('fileTree');
            if (!tree || tree.dataset.wired) return;
            tree.dataset.wired = '1';

            // The row an event happened in, and what it stands for.
            const rowOf = (e) => {
                const el = e.target.closest && e.target.closest('.file-item');
                if (!el || !tree.contains(el)) return null;
                if (el.dataset.parent) return { el: el, parent: true };
                const hit = this.renderedRows[Number(el.dataset.index)];
                if (!hit) return null;
                return { el: el, parent: false, entry: hit.entry, file: hit.file, path: hit.path };
            };

            const goUp = () => {
                const parts = this.currentPath.split('/').filter(p => p);
                parts.pop();
                this.loadFiles('/' + parts.join('/') || '/');
            };

            // Press feedback. Clicking a row used to do nothing visible until
            // the file had loaded, which on a slow panel read as a dead click.
            tree.addEventListener('pointerdown', (e) => {
                const row = rowOf(e);
                if (!row) return;
                row.el.classList.remove('pressed');
                // Restart the animation rather than letting a repeat click be
                // swallowed by the class already being there.
                void row.el.offsetWidth;
                row.el.classList.add('pressed');
            });

            tree.addEventListener('animationend', (e) => {
                const el = e.target.closest && e.target.closest('.file-item');
                if (el) el.classList.remove('pressed');
            });

            tree.addEventListener('click', (e) => {
                const row = rowOf(e);
                if (!row) return;
                if (row.parent) return goUp();

                // Ctrl toggles one row, Shift takes the run from the anchor.
                // Neither opens anything: picking several files and having the
                // last one load its content is not what was asked for.
                if (e.ctrlKey || e.metaKey) return this.toggleSelected(row.entry);
                if (e.shiftKey) return this.selectRangeTo(row.entry);

                this.markSelected(row.el, row.entry);
                if (row.file.isDir) {
                    row.el.classList.add('opening');
                    this.loadFiles(row.path);
                } else if (this.searchResults) {
                    // A hit lives somewhere else; open its folder so the tree
                    // and the editor agree about where we are.
                    const parent = row.path.slice(0, row.path.lastIndexOf('/')) || '/';
                    this.searchResults = null;
                    const box = document.getElementById('fileFilter');
                    if (box) box.value = '';
                    this.filterQuery = '';
                    this.loadFiles(parent).then(() => this.openFile(row.entry));
                } else {
                    this.openFile(row.entry);
                }
            });

            // Middle click loads the file into a tab and leaves you where you
            // are. auxclick is the event that fires for button 1; mousedown
            // alone would also autoscroll.
            tree.addEventListener('auxclick', (e) => {
                if (e.button !== 1) return;
                const row = rowOf(e);
                if (!row || row.parent || row.file.isDir) return;
                e.preventDefault();
                e.stopPropagation();
                this.openFile(row.entry, { background: true });
            });

            tree.addEventListener('mousedown', (e) => {
                if (e.button !== 1) return;
                const row = rowOf(e);
                // Suppress the autoscroll cursor without swallowing the click.
                if (row && !row.parent && !row.file.isDir) e.preventDefault();
            });

            tree.addEventListener('contextmenu', (e) => {
                const row = rowOf(e);
                if (!row) return;
                e.preventDefault();
                if (row.parent) return;
                // Right-clicking inside an existing selection keeps it, so the
                // menu can act on all of it.
                if (!this.selection.has(row.path)) this.markSelected(row.el, row.entry);
                else this.selectedFile = row.entry;
                this.showContextMenu(e, row.entry);
            });

            // The signed URL has to be on the dataTransfer synchronously, but
            // the panel issues it over the network — so it is fetched on hover
            // as well as on selection, which covers dragging a row without
            // clicking it first. mouseover, not mouseenter: mouseenter does not
            // bubble, so it cannot be delegated.
            tree.addEventListener('mouseover', (e) => {
                const row = rowOf(e);
                if (row && !row.parent && !row.file.isDir) this.prefetchDownloadURL(row.entry);
            });

            tree.addEventListener('dragstart', (e) => {
                const row = rowOf(e);
                if (!row || row.parent) return;
                this.startDragOut(e, row.entry, row.file);
            });

            tree.addEventListener('dragend', (e) => this.endDragOut(e));
        },
        
        markSelected(row, entry) {
            this.selection.clear();
            if (entry && entry.path) this.selection.set(entry.path, entry);
            this.prefetchDownloadURL(entry);
            this.selectedFile = entry;
            this.contextFile = entry;
            this.paintSelection();
        },

        toggleSelected(entry) {
            if (!entry || !entry.path) return;
            if (this.selection.has(entry.path)) {
                this.selection.delete(entry.path);
                if (this.selectedFile && this.selectedFile.path === entry.path) {
                    const last = Array.from(this.selection.values()).pop();
                    this.selectedFile = last || null;
                }
            } else {
                this.selection.set(entry.path, entry);
                this.selectedFile = entry;
            }
            this.contextFile = this.selectedFile;
            this.paintSelection();
        },

        // Shift-click takes everything between the last plain click and here,
        // in the order the rows are rendered.
        selectRangeTo(entry) {
            if (!entry || !entry.path) return;

            const paths = this.renderedRows.map(r => r.path);
            const to = paths.indexOf(entry.path);
            const anchor = this.selectedFile ? paths.indexOf(this.selectedFile.path) : -1;

            if (to === -1) return;
            if (anchor === -1) return this.markSelected(null, entry);

            const from = Math.min(anchor, to);
            const until = Math.max(anchor, to);

            this.selection.clear();
            for (let i = from; i <= until; i++) {
                this.selection.set(this.renderedRows[i].path, this.renderedRows[i].entry);
            }
            this.contextFile = entry;
            this.paintSelection();
        },

        selectAllFiles() {
            // What is on screen, not what the folder holds: selecting rows a
            // filter is hiding is how you delete something you cannot see.
            const visible = this.renderedRows.filter(r => !r.el.hidden);
            this.selection.clear();
            visible.forEach(r => this.selection.set(r.path, r.entry));
            this.selectedFile = visible.length ? visible[visible.length - 1].entry : null;
            this.contextFile = this.selectedFile;
            this.paintSelection();
        },

        clearSelection() {
            this.selection.clear();
            this.selectedFile = null;
            this.contextFile = null;
            this.paintSelection();
        },

        paintSelection() {
            document.querySelectorAll('#fileTree .file-item').forEach(item => {
                item.classList.toggle('selected', this.selection.has(item.dataset.path));
            });
            this.updateSelectionUI();
        },

        // The toolbar summary. Without it a multi-row selection is only visible
        // as highlighting, and the count matters before pressing Delete.
        updateSelectionUI() {
            const bar = document.getElementById('selectionBar');
            if (!bar) return;

            const count = this.selection.size;
            bar.hidden = count === 0;
            if (!count) return;

            let dirs = 0;
            this.selection.forEach(entry => { if (entry.isDir) dirs++; });

            const label = count === 1
                ? (this.selectedFile ? this.selectedFile.name : '1 item')
                : count + ' selected' + (dirs ? ' (' + dirs + ' folder' + (dirs === 1 ? '' : 's') + ')' : '');

            const countEl = document.getElementById('selectionCount');
            if (countEl) countEl.textContent = label;
        },

        // Paths for whatever is selected, in rendered order.
        selectionPaths() {
            const ordered = this.renderedRows
                .filter(r => this.selection.has(r.path))
                .map(r => r.path);
            return ordered.length ? ordered : Array.from(this.selection.keys());
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
        /**
         * Opens a file in the editor.
         *
         * opts.background loads it and adds its tab without moving off
         * whatever is on screen — what a middle click should do, and what
         * "Open in new tab" always meant but never did.
         */
        async openFile(file, opts) {
            if (!file || file.isDir) return;
            const background = !!(opts && opts.background);

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
                if (!background) this.switchToFile(filePath);
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

                if (!background) {
                    this.switchToFile(filePath);
                    this.updateFileType(file.name);
                } else {
                    // The tab is there and the content is loaded; the editor
                    // stays on whatever was already open.
                    window.UX.toast.show('Opened ' + file.name + ' in a tab', { duration: 1600 });
                }

                if (window.Session) window.Session.save();

            } catch (err) {
                await this.say('Failed to open file', String(err));
            }
        },
        
        openInNewTab() {
            const file = this.contextFile || this.selectedFile;
            if (file && !file.isDir) this.openFile(file, { background: true });
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

            // Middle-click closes, the way it does in every editor with tabs.
            // auxclick is the event that actually fires for button 1; mousedown
            // is suppressed only to stop the browser's autoscroll cursor.
            tab.addEventListener('auxclick', (e) => {
                if (e.button !== 1) return;
                e.preventDefault();
                this.closeFileTab(path);
            });
            tab.addEventListener('mousedown', (e) => {
                if (e.button === 1) e.preventDefault();
            });

            tabsContainer.appendChild(tab);
        },
        
        /**
         * Ctrl+Tab through the open files.
         *
         * Insertion order, not most-recently-used: the tab strip is in that
         * order too, and a cycle that does not match what is on screen is a
         * cycle nobody can follow.
         */
        cycleEditorTab(delta) {
            const paths = Array.from(this.openFiles.keys());
            if (paths.length < 2) return false;
            const at = paths.indexOf(this.activeFile);
            const next = paths[((at < 0 ? 0 : at) + delta + paths.length) % paths.length];
            this.switchToFile(next);
            return true;
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
                // Ctrl+S used to be silent, so there was no way to tell a save
                // that worked from one that never fired.
                this.reportSaved(file.name, res);
                return true;
            } catch (err) {
                await this.say('Failed to save file', String(err));
                return false;
            }
        },

        /**
         * Says a save happened, in two places.
         *
         * The toast is for the save you asked for; the button flash is for the
         * one you did with Ctrl+S while looking at the text, where a toast in
         * the corner is easy to miss.
         */
        reportSaved(name, res) {
            const version = res && res.version_id
                ? ' · previous version kept'
                : '';
            window.UX.toast.ok('Saved ' + name + version, { duration: 2200 });

            const btn = document.getElementById('saveBtn');
            if (!btn) return;
            if (this.saveFlashTimer) clearTimeout(this.saveFlashTimer);
            const label = btn.dataset.label || btn.textContent;
            btn.dataset.label = label;
            btn.textContent = 'Saved';
            btn.classList.add('is-saved');
            this.saveFlashTimer = setTimeout(() => {
                btn.textContent = btn.dataset.label || 'Save';
                btn.classList.remove('is-saved');
                this.saveFlashTimer = null;
            }, 1400);
        },

        async saveAllFiles() {
            let written = 0;
            for (const [path, file] of this.openFiles) {
                if (file.modified) {
                    if (await this.writeFile(path, file, false)) written++;
                }
            }
            console.log('Save all finished');
            if (written > 1) {
                window.UX.toast.ok(written + ' files saved', { duration: 2200 });
            }
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
            if (window.Session) window.Session.save();
            
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
        
        /**
         * Drag the edge between the tree and the editor.
         *
         * The pane used to carry `resize: horizontal`, which puts the handle in
         * its bottom-right corner — under the file list's scrollbar, and
         * nowhere near the border people actually aim at. This is the same
         * grip the split view has.
         *
         * The size is measured from the pane's own edge rather than from a
         * delta, so it stays right when the tree is docked right or bottom and
         * the flex direction is reversed.
         */
        wireTreeGrip() {
            const grip = document.getElementById('fileGrip');
            const pane = document.querySelector('.file-manager > .file-pane');
            if (!grip || !pane) return;

            const MIN = 120;
            const apply = (event) => {
                const box = pane.getBoundingClientRect();
                if (this.treeDock === 'bottom') {
                    const max = Math.max(MIN, window.innerHeight - 160);
                    const h = Math.min(max, Math.max(MIN, box.bottom - event.clientY));
                    pane.style.height = h + 'px';
                    pane.style.width = '';
                    this.treeSize = h;
                    return;
                }
                const max = Math.max(MIN, window.innerWidth - 220);
                const w = this.treeDock === 'right'
                    ? box.right - event.clientX
                    : event.clientX - box.left;
                const clamped = Math.min(max, Math.max(MIN, w));
                pane.style.width = clamped + 'px';
                pane.style.height = '';
                this.treeSize = clamped;
            };

            grip.addEventListener('pointerdown', (down) => {
                if (this.treeDock === 'hidden') return;
                down.preventDefault();
                grip.setPointerCapture(down.pointerId);
                document.body.classList.add('resizing');

                const move = (e) => apply(e);
                const up = () => {
                    grip.removeEventListener('pointermove', move);
                    grip.removeEventListener('pointerup', up);
                    grip.removeEventListener('pointercancel', up);
                    document.body.classList.remove('resizing');
                    try {
                        localStorage.setItem('treeSize:' + this.treeDock, String(this.treeSize));
                    } catch (err) { /* private mode */ }
                };

                grip.addEventListener('pointermove', move);
                grip.addEventListener('pointerup', up);
                grip.addEventListener('pointercancel', up);
            });

            // Double-click puts it back where it started.
            grip.addEventListener('dblclick', () => {
                pane.style.width = '';
                pane.style.height = '';
                try { localStorage.removeItem('treeSize:' + this.treeDock); } catch (err) { /* private mode */ }
            });
        },

        // Each dock remembers its own size: a sensible width on the left is
        // not a sensible height along the bottom.
        restoreTreeSize() {
            const pane = document.querySelector('.file-manager > .file-pane');
            if (!pane) return;
            pane.style.width = '';
            pane.style.height = '';
            if (this.treeDock === 'hidden') return;

            let stored = null;
            try { stored = localStorage.getItem('treeSize:' + this.treeDock); } catch (err) { /* private mode */ }
            const size = parseInt(stored, 10);
            if (!size || size < 120) return;

            if (this.treeDock === 'bottom') pane.style.height = size + 'px';
            else pane.style.width = size + 'px';
        },

        // The tree can sit on either side, along the bottom, or be folded away
        // for a full-width editor.
        applyTreeDock(dock) {
            const manager = document.querySelector('.file-manager');
            if (!manager) return;
            ['dock-left', 'dock-right', 'dock-bottom', 'dock-hidden'].forEach(c => manager.classList.remove(c));
            manager.classList.add('dock-' + dock);
            this.treeDock = dock;
            try { localStorage.setItem('treeDock', dock); } catch (err) { /* private mode */ }
            this.restoreTreeSize();
        },

        cycleTreeDock() {
            const order = ['left', 'right', 'bottom', 'hidden'];
            const next = order[(order.indexOf(this.treeDock) + 1) % order.length];
            this.applyTreeDock(next);
            window.UX.toast.show('File tree: ' + next, { duration: 1200 });
        },

        restoreTreeDock() {
            let stored = 'left';
            try { stored = localStorage.getItem('treeDock') || 'left'; } catch (err) { /* private mode */ }
            this.applyTreeDock(['left', 'right', 'bottom', 'hidden'].indexOf(stored) === -1 ? 'left' : stored);
            this.wireTreeGrip();
        },

        /**
         * Opens `path` on the current server, or the deepest part of it that
         * exists.
         *
         * Two servers on one host usually share a layout, so carrying the path
         * across a switch saves walking back down it. When the folder is not
         * there, each parent is tried in turn — /Kite/scripts missing still
         * leaves you at /Kite — and the root is the last resort.
         */
        async loadNearest(path) {
            const parts = String(path || '/').split('/').filter(Boolean);

            while (parts.length) {
                const candidate = '/' + parts.join('/');
                try {
                    await window.go.main.App.ListFiles(candidate);
                    await this.loadFiles(candidate);
                    if (candidate !== path) {
                        window.UX.toast.show('This server has no ' + path + ' — opened ' + candidate,
                            { duration: 3400 });
                    }
                    return candidate;
                } catch (err) {
                    parts.pop();
                }
            }

            await this.loadFiles('/');
            if (path && path !== '/') {
                window.UX.toast.show('This server has no ' + path + ' — opened /', { duration: 3400 });
            }
            return '/';
        },

        // File operations
        async refreshFiles() {
            // A refresh means a refresh: drop the cached copy of this folder
            // so the listing comes off the panel rather than out of memory.
            this.dirCacheForget([this.currentPath]);
            await this.loadFiles(this.currentPath);
        },
        
        async uploadFile() {
            // Over SFTP the transfer reads from disk, and a browser file input
            // hands over contents without a path. So it asks the OS for the
            // paths instead, which also allows whole folders.
            if (window.SFTP && window.SFTP.isConnected()) {
                let picked;
                try {
                    picked = await window.go.main.App.PickLocalFiles();
                } catch (err) {
                    window.UX.toast.bad(String(err));
                    return;
                }
                if (!picked || !picked.length) return;
                await window.SFTP.upload(picked, this.currentPath || '/');
                return;
            }

            const input = document.getElementById('fileInput');
            if (input) input.click();
        },

        /**
         * Walks a drop into a flat list of {file, relPath}.
         *
         * dataTransfer.files is flat and loses the folder a file came from, so
         * the entries API is used when the browser offers it and the plain
         * list is the fallback.
         */
        async collectDrop(dataTransfer) {
            const MAX = 500;
            const out = [];

            const items = Array.from(dataTransfer.items || []);
            const roots = items
                .map((it) => (it.webkitGetAsEntry ? it.webkitGetAsEntry() : null))
                .filter(Boolean);

            if (!roots.length) {
                Array.from(dataTransfer.files || []).forEach((file) => {
                    out.push({ file: file, relPath: file.name });
                });
                return { entries: out.slice(0, MAX), truncated: out.length > MAX };
            }


            let truncated = false;

            async function walk(entry, prefix) {
                if (out.length >= MAX) {
                    truncated = true;
                    return;
                }
                if (entry.isFile) {
                    const file = await new Promise((res, rej) => entry.file(res, rej));
                    out.push({ file: file, relPath: prefix + entry.name });
                    return;
                }
                // readEntries hands back at most a hundred at a time and
                // signals the end with an empty batch.
                const reader = entry.createReader();
                for (;;) {
                    const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
                    if (!batch.length) break;
                    for (const child of batch) await walk(child, prefix + entry.name + '/');
                    if (out.length >= MAX) {
                        truncated = true;
                        break;
                    }
                }
            }

            for (const root of roots) await walk(root, '');
            return { entries: out, truncated: truncated };
        },

        /**
         * Base64 for one file, read natively.
         *
         * readAsDataURL is the browser's own encoder; building it in JS means
         * btoa over a String.fromCharCode of the whole buffer, which blows the
         * argument limit on anything sizeable.
         */
        fileToBase64(file) {
            return new Promise((resolve, reject) => {
                const reader = new FileReader();
                reader.onerror = () => reject(reader.error);
                reader.onload = () => {
                    const url = String(reader.result);
                    resolve(url.slice(url.indexOf(',') + 1));
                };
                reader.readAsDataURL(file);
            });
        },

        /**
         * Whether replacing something on this server keeps a copy first.
         *
         * Asked at the moment it matters rather than cached: the setting lives
         * in the Vault tab and can be turned off between one upload and the
         * next. Treated as off if the question itself fails — the safe answer
         * to "can this be undone?" is no.
         */
        async binState() {
            try {
                return await window.go.main.App.BinAvailable('');
            } catch (err) {
                return { enabled: false, reason: String(err) };
            }
        },

        /**
         * Uploads a walked drop into the current folder.
         *
         * One batched call rather than one per file: the backend creates the
         * folders once, lists each target directory once, and holds the file
         * lock once. Collisions come back as a list and are asked about
         * together.
         */
        async uploadDrop(entries, truncated) {
            if (!entries.length) return;

            const busy = window.UX.toast.show('Reading ' + entries.length + ' file(s)…', { duration: 600000 });

            const items = [];
            const failed = [];
            let bytes = 0;

            for (const entry of entries) {
                try {
                    const b64 = await this.fileToBase64(entry.file);
                    items.push({ rel_path: entry.relPath, base64: b64 });
                    bytes += entry.file.size || 0;
                } catch (err) {
                    failed.push(entry.relPath + ' — could not be read');
                }
            }

            if (!items.length) {
                busy.dismiss();
                if (failed.length) await this.say('Nothing could be read', failed.join('\n'));
                return;
            }

            busy.dismiss();
            const sending = window.UX.toast.show(
                'Uploading ' + items.length + ' file(s), ' + this.formatSize(bytes) + '…', { duration: 600000 });

            let result;
            try {
                result = await window.go.main.App.UploadBatch(this.currentPath, items, false, true);
            } catch (err) {
                sending.dismiss();
                await this.say('Upload failed', String(err));
                return;
            }
            sending.dismiss();

            let done = result.uploaded.length;
            failed.push(...(result.failed || []));

            if (result.conflicts && result.conflicts.length) {
                // With the recycle bin off for this server there is nothing to
                // keep a copy in, so offering the choice would be offering
                // something the app cannot do. It says that instead, and the
                // dialog is one click.
                const bin = await this.binState();

                const list = '<p class="form-hint" style="margin-bottom:12px">' +
                    result.conflicts.slice(0, 8).map(c => '<span class="mono">' + this.escapeHtml(c) + '</span>').join('<br>') +
                    (result.conflicts.length > 8 ? '<br>…and ' + (result.conflicts.length - 8) + ' more' : '') +
                    '</p>';

                const answer = await window.Shell.dialog.form(
                    result.conflicts.length + ' already there',
                    bin.enabled ? [{
                        name: 'keep', type: 'checkbox', value: true,
                        label: 'Keep a copy of what I am replacing',
                        hint: 'Each copy is a full download of the old file before the new one goes up. ' +
                              'Turning this off is noticeably faster and makes the replacements unrecoverable.'
                    }] : [],
                    {
                        confirmLabel: 'Replace them',
                        danger: true,
                        intro: list + (bin.enabled ? '' :
                            '<p class="form-hint" style="margin-bottom:12px;color:var(--danger-text)">' +
                            '<b>The recycle bin is off for this server.</b> Nothing is copied first, so ' +
                            'what these replace cannot be brought back. Turn it on in ' +
                            'Vault \u2192 Recycle bin per server.</p>')
                    });

                if (answer) {
                    // Exact paths, not a suffix test. "/dir/sub/one.txt"
                    // ends with "one.txt", so matching loosely re-sent a
                    // different one.txt with overwrite set and replaced a file
                    // the prompt never mentioned.
                    const base = this.currentPath === '/' ? '' : this.currentPath;
                    const wanted = new Set(result.conflicts);
                    const again = items.filter(i => wanted.has(base + '/' + i.rel_path));
                    const retry = window.UX.toast.show('Replacing ' + again.length + ' file(s)…', { duration: 600000 });
                    try {
                        const second = await window.go.main.App.UploadBatch(
                            this.currentPath, again, true, !!(bin.enabled && answer.keep));
                        done += second.replaced.length + second.uploaded.length;
                        failed.push(...(second.failed || []));
                    } catch (err) {
                        failed.push('replacing: ' + err);
                    }
                    retry.dismiss();
                }
            }

            await this.refreshFiles();

            if (done) window.UX.toast.ok('Uploaded ' + done + ' file(s)');
            if (truncated) window.UX.toast.warn('Only the first 500 files were taken from that drop');
            if (failed.length) await this.say('Some files did not upload', failed.join('\n'));
        },

        /** Wires the explorer as a drop target for files from the desktop. */
        setupDropUpload() {
            const pane = document.querySelector('.file-pane');
            if (!pane) return;

            const isFileDrop = (e) => Array.from(e.dataTransfer.types || []).indexOf('Files') !== -1;

            pane.addEventListener('dragover', (e) => {
                if (!isFileDrop(e)) return;
                e.preventDefault();
                e.dataTransfer.dropEffect = 'copy';
                pane.classList.add('drop-target');
            });

            pane.addEventListener('dragleave', (e) => {
                if (!pane.contains(e.relatedTarget)) pane.classList.remove('drop-target');
            });

            pane.addEventListener('drop', async (e) => {
                if (!isFileDrop(e)) return;
                e.preventDefault();
                pane.classList.remove('drop-target');

                const walked = await this.collectDrop(e.dataTransfer);
                await this.uploadDrop(walked.entries, walked.truncated);
            });
        },
        
        // The Upload button and a drop from the desktop are the same thing
        // once the files are in hand.
        async handleFileUpload(files) {
            const entries = Array.from(files || []).map(file => ({ file: file, relPath: file.name }));
            await this.uploadDrop(entries, false);
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
        
        // Exactly one row, for the actions that only make sense on one.
        singleSelection() {
            if (this.selection.size === 1) return Array.from(this.selection.values())[0];
            if (this.selection.size === 0 && this.selectedFile) return this.selectedFile;
            return null;
        },

        async deleteSelected() {
            const paths = this.selectionPaths();
            if (!paths.length) {
                await this.say('Nothing selected', 'Pick a file or folder in the tree first.');
                return;
            }
            await this.deletePaths(paths);
        },

        async archiveSelected() {
            const paths = this.selectionPaths();
            if (!paths.length) {
                await this.say('Nothing selected', 'Pick what you want in the archive first.');
                return;
            }

            const ok = await this.ask('Archive ' + paths.length + ' item' + (paths.length === 1 ? '' : 's'),
                'The panel packs them into a new <span class="mono">.tar.gz</span> in ' +
                '<span class="mono">' + this.escapeHtml(this.currentPath) + '</span>. ' +
                'Nothing is removed.',
                { confirmLabel: 'Archive' });
            if (!ok) return;

            const busy = window.UX.toast.show('Archiving ' + paths.length + ' item(s)…', { duration: 60000 });
            try {
                const result = await window.go.main.App.CompressFiles(paths);
                busy.dismiss();
                await this.refreshFiles();
                window.UX.toast.ok('Created ' + result.name + ' (' + this.formatSize(result.size) + ')');
            } catch (err) {
                busy.dismiss();
                await this.say('Could not archive', String(err));
            }
        },

        async extractSelected() {
            const entry = this.singleSelection();
            const path = this.resolvePath(entry);
            if (!path || (entry && entry.isDir)) {
                await this.say('Pick one archive', 'Select a single archive file to extract.');
                return;
            }

            const where = await window.Shell.dialog.choose(
                'Extract ' + this.escapeHtml(entry.name), '',
                [
                    { key: 'new', label: 'Extract to folder',
                      detail: 'A new folder named <span class="mono">' +
                              this.escapeHtml(entry.name.replace(/\.(tar\.gz|tgz|tar|zip|gz|7z|rar)$/i, '')) +
                              '</span>. Nothing already here is touched.' },
                    { key: 'here', label: 'Replace into current folder', danger: true,
                      detail: 'Files the archive contains replace the ones in <span class="mono">' +
                              this.escapeHtml(this.currentPath) + '</span>. Those replacements are the one thing ' +
                              'Vault cannot recover.' }
                ]);
            if (!where) return;

            const inPlace = where === 'here';

            const busy = window.UX.toast.show('Extracting ' + entry.name + '…', { duration: 120000 });
            try {
                const target = await window.go.main.App.DecompressFile(path, !inPlace);
                busy.dismiss();
                await this.refreshFiles();
                window.UX.toast.ok('Extracted into ' + target, {
                    action: inPlace ? null : { label: 'Open', run: () => this.loadFiles(target) }
                });
            } catch (err) {
                busy.dismiss();
                await this.say('Could not extract', String(err));
            }
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

            // The dialog goes up now and fills itself in. Planning walks the
            // tree — one request per folder — so on a plugins directory it
            // takes seconds, and waiting for that before showing anything made
            // pressing Delete look like nothing had happened.
            const answer = window.Shell.dialog.open({
                title: 'Delete ' + paths.length + ' item(s)',
                body: '<div class="loading">' +
                    (window.Icons ? window.Icons.svg('refresh', 'spin') : '') +
                    '<div>Working out what would go…</div></div>',
                confirmLabel: 'Delete',
                danger: true,
                busy: true
            });

            let plan;
            try {
                plan = await window.go.main.App.PlanDelete('', paths);
            } catch (err) {
                window.Shell.dialog.close(null);
                await answer;
                await this.say('Cannot delete', String(err));
                return;
            }

            // It may have been dismissed while the plan was being worked out.
            // Filling in a dialog that is gone would leave this waiting on a
            // confirmation nobody can give.
            if (!document.getElementById('appDialog').classList.contains('show')) {
                await answer;
                return;
            }

            const intro = this.renderDeletePlan(plan);
            // Typing it out is for what cannot be taken back: something the
            // backend flagged as critical, or a selection the recycle bin
            // cannot hold. A folder that is fully copied first is recoverable,
            // and making people type DELETE for every one of those trained
            // them to type it without reading.
            // ...and the App settings tab decides whether it is ever asked
            // for. Two people wanted different answers here — one arguing that
            // no backup means more ceremony, the other that they have never
            // once deleted something they needed — so it is a setting rather
            // than a guess. "double" still shows exactly the same dialog and
            // the same warnings; it just does not make you type.
            const style = plan.confirm_style ||
                (window.AppSettings ? window.AppSettings.deleteConfirm() : 'typed');
            const strict = style !== 'double' &&
                ((plan.critical && plan.critical.length > 0) || !plan.recoverable);

            const noWayBack = (plan.critical && plan.critical.length > 0) || !plan.recoverable;

            if (strict) {
                window.Shell.dialog.update({
                    title: 'Delete ' + plan.roots.length + ' item(s)',
                    body: intro +
                        '<div class="form-group" style="margin-top:14px">' +
                        '<label>Type DELETE to confirm</label>' +
                        '<input type="text" class="mono" data-field="confirm" placeholder="DELETE">' +
                        '</div>',
                    confirmTwice: '',
                    busy: false
                });
                if (!await answer) return;
                if (String(window.Shell.dialog.field('confirm') || '').trim().toUpperCase() !== 'DELETE') {
                    await this.say('Nothing deleted', 'The confirmation did not match, so nothing was removed.');
                    return;
                }
            } else {
                // Two clicks rather than a typed word: enough that a stray
                // click cannot delete anything, without the ceremony.
                window.Shell.dialog.update({
                    title: 'Delete ' + plan.roots.length + ' item(s)',
                    body: intro,
                    // The second click says which case this is, since the only
                    // difference between them is whether it can be undone.
                    confirmTwice: noWayBack
                        ? 'Click again — this cannot be undone'
                        : 'Click again to delete',
                    busy: false
                });
                if (!await answer) return;
            }

            // Says what is happening, not what usually happens: with the bin
            // off for this server nothing is copied anywhere.
            const deleting = window.UX.toast.show(
                plan.bin_enabled === false ? 'Deleting…' : 'Copying to the recycle bin and deleting…',
                { duration: 600000 });
            try {
                const outcome = await window.go.main.App.SafeDeleteFiles(plan.token);
                deleting.dismiss();
                this.dirCacheForget(plan.roots);
                this.forgetOpenFilesUnder(plan.roots);
                await this.refreshFiles();

                // A dialog for the result made you dismiss a box to get back to
                // work. A toast says the same thing and carries the undo, and
                // gets out of the way on its own.
                const undo = outcome.batch ? {
                    label: 'Undo',
                    run: async () => {
                        try {
                            const back = await window.go.main.App.RestoreBinBatch(outcome.batch, false);
                            await this.refreshFiles();
                            window.UX.toast.ok((back.restored || []).length + ' file(s) put back');
                        } catch (err) {
                            window.UX.toast.bad('Could not undo: ' + err);
                        }
                    }
                } : null;

                // A delete can now partly succeed: each selected root is
                // removed on its own, so one failure no longer takes the rest
                // of the operation — or their recycle-bin copies — with it.
                if (outcome.failed && outcome.failed.length) {
                    await this.say('Some of that could not be deleted',
                        outcome.deleted.length + ' removed, ' + outcome.failed.length + ' left alone:\n' +
                        outcome.failed.join('\n'));
                }

                // Files written into a selected folder after the preview was
                // taken. They were copied and deleted like the rest, but they
                // were never on the list the dialog showed.
                if (outcome.appeared && outcome.appeared.length) {
                    window.UX.toast.warn(
                        outcome.appeared.length + ' file(s) appeared after the preview and went too');
                }

                if (outcome.skipped && outcome.skipped.length) {
                    window.UX.toast.warn(
                        outcome.captured + ' in the recycle bin, ' + outcome.skipped.length +
                        ' could not be copied first and are gone for good',
                        { action: undo });
                } else if (outcome.captured > 0 || outcome.deleted.length) {
                    window.UX.toast.ok(
                        outcome.captured + ' file' + (outcome.captured === 1 ? '' : 's') + ' moved to the recycle bin',
                        { action: undo });
                }
            } catch (err) {
                deleting.dismiss();
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

            if (plan.bin_enabled === false) {
                // Not a shade of "some of this": nothing is copied at all.
                html += '<p style="color:var(--danger-text)"><b>The recycle bin is off for this server.</b> ' +
                    'Nothing here is copied anywhere first, and none of it can be restored. ' +
                    'Turn it back on in Vault → Recycle bin per server.</p>';
            } else if (plan.recoverable) {
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
            await this.downloadSelected();
        },

        /**
         * Saves the selection onto this machine.
         *
         * One file gets a Save-as dialog, anything else asks for a folder. A
         * folder in the selection is archived on the panel first and the
         * archive is what comes down — the client API serves files, not trees —
         * and the archive is cleaned up afterwards.
         */
        async downloadSelected() {
            const paths = this.selectionPaths();
            if (!paths.length) {
                await this.say('Nothing selected', 'Pick what you want to download first.');
                return;
            }

            // Connected, this comes down directly and in parallel — no archive
            // made on the panel, no size cap, and folders keep their shape.
            if (window.SFTP && window.SFTP.isConnected()) {
                await window.SFTP.download(paths);
                return;
            }

            // One file comes down as itself; anything else is archived on the
            // panel first and arrives as a single file.
            const folders = Array.from(this.selection.values()).filter(e => e.isDir).length;
            const asArchive = paths.length > 1 || folders > 0;

            const busy = window.UX.toast.show(
                asArchive ? 'Archiving ' + paths.length + ' item(s) and downloading…' : 'Downloading…',
                { duration: 600000 });

            try {
                const out = await window.go.main.App.DownloadToDisk(paths);
                busy.dismiss();

                if (out.cancelled) return;

                if (out.files.length) {
                    window.UX.toast.ok('Saved ' + out.files[0] + ' (' + this.formatSize(out.bytes) + ')');
                } else {
                    window.UX.toast.warn('Nothing was downloaded');
                }
                if (out.skipped && out.skipped.length) {
                    await this.say('Note', out.skipped.join('\n'));
                }
            } catch (err) {
                busy.dismiss();
                await this.say('Could not download', String(err));
            }
        },

        /**
         * Best-effort drag of a file out to the desktop.
         *
         * The shell only accepts a drop when the drag carries a DownloadURL,
         * and that has to be on the dataTransfer synchronously — but the panel
         * hands out signed URLs over the network. So the URL is fetched when a
         * file is selected, which is always a moment or two before the drag,
         * and the drag uses it if it arrived. If it did not, the drag simply
         * does not leave the window; use Download instead.
         */
        /**
         * Drag a selection out of the window and onto the desktop.
         *
         * A single file has a URL of its own and goes on the drag straight
         * away. A folder does not — one has to be archived on the panel first,
         * and that is a network round trip. Since the URL has to be on the
         * event synchronously, the first drag of a folder starts the packing
         * and says so, and the drag after it carries the archive.
         */
        startDragOut(e, entry, file) {
            // Dragging a row that is part of the selection takes the whole
            // selection; dragging one outside it takes only that row.
            let paths = [entry.path];
            if (this.selection && this.selection.size > 1 && this.selection.has(entry.path)) {
                paths = Array.from(this.selection.keys());
            }

            e.dataTransfer.setData('text/plain', paths.join('\n'));
            e.dataTransfer.effectAllowed = 'copy';

            // One plain file needs nothing made for it.
            if (paths.length === 1 && !file.isDir) {
                const url = this.downloadURLs && this.downloadURLs.get(entry.path);
                if (url) {
                    // Chromium's contract for a drag the OS shell accepts.
                    e.dataTransfer.setData('DownloadURL',
                        'application/octet-stream:' + file.name + ':' + url);
                }
                return;
            }

            const key = paths.slice().sort().join('\u0000');
            const ready = this.dragArchives && this.dragArchives.get(key);
            if (ready) {
                e.dataTransfer.setData('DownloadURL',
                    'application/octet-stream:' + ready.name + ':' + ready.url);
                this.dragInFlight = ready;
                return;
            }

            // Nothing to carry yet. Start packing and let this drag fizzle.
            this.prepareDragArchive(key, paths);
        },

        async prepareDragArchive(key, paths, accept) {
            if (!this.dragArchives) this.dragArchives = new Map();
            if (this.dragPending === key) return;
            this.dragPending = key;

            const busy = window.UX.toast.show(
                'Packing ' + paths.length + ' item(s) for the drag — try again when this clears',
                { duration: 600000 });

            try {
                const made = await window.go.main.App.PrepareDragArchive(paths, !!accept);

                // Big enough to be worth asking about. Archiving is the
                // panel's CPU and disk, and a world folder dragged half an inch
                // by accident is not something to start quietly.
                if (made && made.needs_confirm) {
                    busy.dismiss();
                    this.dragPending = null;
                    const ok = await window.Shell.dialog.confirm('Pack this for the drag?',
                        'That is <b>' + this.escapeHtml(made.summary) + '</b>' +
                        (made.partial ? ' — it was still counting when it stopped' : '') +
                        '.<br><br>Packing it happens on the server, and something this size ' +
                        'takes a while and works the disk while it runs.',
                        { html: true, confirmLabel: 'Pack it' });
                    if (!ok) return;
                    return this.prepareDragArchive(key, paths, true);
                }
                busy.dismiss();
                this.dragArchives.set(key, made);
                // The signed URL is short lived and the archive is swept off
                // the panel after ten minutes, so this forgets well before
                // either of those runs out.
                setTimeout(() => {
                    if (this.dragArchives.get(key) === made) this.dragArchives.delete(key);
                }, 5 * 60 * 1000);
                window.UX.toast.ok(made.archived
                    ? 'Packed — drag it out now. The temporary archive comes off the server afterwards.'
                    : 'Ready — drag it out now');
            } catch (err) {
                busy.dismiss();
                window.UX.toast.bad('Could not pack that: ' + String(err));
            } finally {
                if (this.dragPending === key) this.dragPending = null;
            }
        },

        endDragOut(e) {
            const used = this.dragInFlight;
            this.dragInFlight = null;

            // A drag the shell refused leaves an archive with nothing to do, so
            // it comes off the panel now rather than waiting out its lifetime.
            if (used && used.archived && e.dataTransfer.dropEffect === 'none') {
                if (this.dragArchives) {
                    this.dragArchives.forEach((value, key) => {
                        if (value === used) this.dragArchives.delete(key);
                    });
                }
                window.go.main.App.DiscardDragArchive(used.path).catch(() => {});
            }
        },

        prefetchDownloadURL(entry) {
            if (!entry || entry.isDir || !entry.path) return;
            if (this.downloadURLs && this.downloadURLs.has(entry.path)) return;

            if (!this.downloadURLs) this.downloadURLs = new Map();
            window.go.main.App.GetFileDownloadURL(entry.path).then((url) => {
                // Signed URLs are short lived, so this is a cache with a
                // deliberately short memory rather than a store.
                this.downloadURLs.set(entry.path, url);
                setTimeout(() => this.downloadURLs.delete(entry.path), 60000);
            }).catch(() => {});
        },
        
        showContextMenu(event, file) {
            const menu = document.getElementById('contextMenu');
            if (!menu) return;

            this.contextFile = file;

            // Only the entries that mean something for what was clicked. A
            // folder cannot be opened in the editor, duplicated or extracted,
            // and offering those put items in the menu that did nothing.
            const isDir = !!(file && file.isDir);
            menu.querySelectorAll('[data-files-only]').forEach(el => { el.hidden = isDir; });
            menu.querySelectorAll('[data-dirs-only]').forEach(el => { el.hidden = !isDir; });

            // body carries a CSS `zoom`, so the pointer's clientX/clientY are
            // unzoomed viewport pixels while `left`/`top` inside the body are
            // multiplied by that zoom again. At 110% the menu landed 10% down
            // and right of the cursor.
            const scale = (window.UX && window.UX.scale && window.UX.scale()) || 1;
            let x = event.clientX / scale;
            let y = event.clientY / scale;

            // Measure before placing, so a menu near the edge opens inwards
            // rather than off the window.
            menu.classList.add('show');
            const box = menu.getBoundingClientRect();
            const limitX = window.innerWidth / scale;
            const limitY = window.innerHeight / scale;
            if (x + box.width / scale > limitX) x = Math.max(0, limitX - box.width / scale - 4);
            if (y + box.height / scale > limitY) y = Math.max(0, limitY - box.height / scale - 4);

            menu.style.left = x + 'px';
            menu.style.top = y + 'px';
        },

        // "Open folder" from the context menu, for the entry that was clicked
        // rather than whatever happens to be selected.
        openContextFolder() {
            const file = this.contextFile;
            if (file && file.isDir) this.navigateTo(this.resolvePath(file));
        },
        
        // Console operations
        appendConsole(message, type = '') {
            const consoleEl = document.getElementById('console');
            if (!consoleEl) return;
            
            const line = document.createElement('div');
            line.className = 'console-line';
            if (type) line.classList.add(type);
            
            // Convert ANSI codes to HTML for colored output, then make any
            // links in it clickable.
            line.innerHTML = this.linkifyConsole(this.ansiToHtml(message));
            
            consoleEl.appendChild(line);
            consoleEl.scrollTop = consoleEl.scrollHeight;
            
            while (consoleEl.children.length > 1000) {
                consoleEl.removeChild(consoleEl.firstChild);
            }
        },
        
        /**
         * Turn http(s) links in a console line into something clickable.
         *
         * Only the text between tags is scanned. Running the pattern over the
         * whole string would find "http" inside a style attribute the ANSI
         * pass had just written, and wrap a tag in an anchor.
         *
         * The text arriving here is already HTML-escaped, so an entity like
         * &amp; inside a query string survives into the attribute and the DOM
         * decodes it back to & when the handler reads it.
         */
        linkifyConsole(html) {
            const URL_RE = /https?:\/\/[^\s<>"']+/g;
            // Trailing punctuation is almost never part of the link — a URL at
            // the end of a sentence, or one in brackets.
            const TAIL = /[.,;:!?)\]}'"]$|&(?:quot|#39|gt|lt|amp);$/;

            return html.split(/(<[^>]*>)/).map((part) => {
                if (part.charAt(0) === '<') return part;
                return part.replace(URL_RE, (match) => {
                    let url = match;
                    let tail = '';
                    let cut;
                    while (url && (cut = url.match(TAIL))) {
                        tail = url.slice(cut.index) + tail;
                        url = url.slice(0, cut.index);
                    }
                    if (!url) return match;
                    return '<a class="console-link" role="link" tabindex="0" data-url="' +
                        url.replace(/"/g, '&quot;') +
                        '" title="Open in your browser">' + url + '</a>' + tail;
                });
            }).join('');
        },

        /**
         * One handler on the console rather than one per line: lines are
         * appended constantly and capped at a thousand, so per-line listeners
         * would be churn for nothing.
         *
         * The window itself never navigates. The link goes to the system
         * browser through the Go side, which checks the scheme — console output
         * comes from whatever is running on the server, and it is not trusted
         * to name what gets opened.
         */
        wireConsoleLinks() {
            const consoleEl = document.getElementById('console');
            if (!consoleEl || consoleEl.dataset.linksWired) return;
            consoleEl.dataset.linksWired = '1';

            const open = (link) => {
                const url = link && link.dataset.url;
                if (!url) return;
                window.go.main.App.OpenExternalURL(url).catch((err) => {
                    window.UX.toast.bad('Could not open that link: ' + err);
                });
            };

            consoleEl.addEventListener('click', (e) => {
                const link = e.target.closest('.console-link');
                if (!link) return;
                e.preventDefault();
                open(link);
            });

            // Reachable without a mouse, since it is focusable.
            consoleEl.addEventListener('keydown', (e) => {
                if (e.key !== 'Enter' && e.key !== ' ') return;
                const link = e.target.closest && e.target.closest('.console-link');
                if (!link) return;
                e.preventDefault();
                open(link);
            });
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
        
        /**
         * Opens this server's console in its own OS window.
         *
         * A second process, because Wails v2 gives one window per process. It
         * reads the same config and opens its own websocket, which the panel
         * serves alongside this one.
         */
        async popOutConsole() {
            try {
                await window.go.main.App.OpenConsoleWindow('');
                window.UX.toast.ok('Console window opening — it is a separate window, so it can go on another monitor');
            } catch (err) {
                await this.say('Could not open the console window', String(err));
            }
        },

        async connectConsole() {
            try {
                if (this.consoleConnected || this.consoleRetryTimer) {
                    // Pressing Disconnect also calls off a reconnect in
                    // progress. Being reconnected to something you just asked
                    // to leave is not what the button says.
                    this.consoleWanted = false;
                    this.cancelConsoleRetry();
                    await window.go.main.App.DisconnectConsole();
                    this.consoleConnected = false;
                    this.paintConnectButton();
                } else {
                    console.log('Connecting to console...');
                    this.consoleWanted = true;
                    this.consoleRetries = 0;
                    await window.go.main.App.ConnectConsole();
                }
            } catch (err) {
                console.error('Console connection failed:', err);
                window.UX.toast.bad('Console: ' + err);
                // A failed connect emits no event, so without this the button
                // would sit on Connect having been asked to connect, and a
                // later stray disconnect would start retrying out of nowhere.
                if (this.consoleWanted) this.scheduleConsoleReconnect();
                else this.paintConnectButton();
            }
        },

        /**
         * The console's token lasts about ten minutes, and a dropped socket
         * used to mean typing Connect again - after noticing, since the button
         * still said Disconnect.
         *
         * The Go side renews an expiring token in place, so this is the
         * backstop for what cannot be renewed: a token that ran out while the
         * machine was asleep, a panel briefly unreachable, a network that came
         * and went.
         */
        CONSOLE_RETRY_DELAYS: [1000, 3000, 8000, 20000],

        scheduleConsoleReconnect() {
            if (this.consoleRetryTimer || !this.consoleWanted) return;

            const attempt = this.consoleRetries || 0;
            if (attempt >= this.CONSOLE_RETRY_DELAYS.length) {
                this.appendConsole(
                    '=== Could not reconnect. Press Connect to try again ===', 'error');
                this.consoleWanted = false;
                this.paintConnectButton();
                return;
            }

            const wait = this.CONSOLE_RETRY_DELAYS[attempt];
            this.consoleRetries = attempt + 1;
            this.consoleRetryTimer = setTimeout(async () => {
                this.consoleRetryTimer = null;
                if (!this.consoleWanted || this.consoleConnected) return;
                this.paintConnectButton();
                try {
                    await window.go.main.App.ConnectConsole();
                } catch (err) {
                    this.appendConsole('[ERROR] Reconnect failed: ' + err, 'error');
                    // A failed connect emits no console-connected event, so the
                    // next attempt is booked here rather than waited for.
                    this.scheduleConsoleReconnect();
                }
            }, wait);

            this.paintConnectButton();
            this.appendConsole(
                '=== Reconnecting in ' + Math.round(wait / 1000) + 's ' +
                '(attempt ' + this.consoleRetries + ' of ' + this.CONSOLE_RETRY_DELAYS.length + ') ===',
                'warn');
        },

        cancelConsoleRetry() {
            if (!this.consoleRetryTimer) return;
            clearTimeout(this.consoleRetryTimer);
            this.consoleRetryTimer = null;
        },

        // One place decides what the button says, so a reconnect in progress
        // cannot leave it claiming to be connected.
        paintConnectButton() {
            const btn = document.getElementById('connectBtn');
            if (!btn) return;
            if (this.consoleConnected) btn.textContent = 'Disconnect';
            else if (this.consoleRetryTimer) btn.textContent = 'Reconnecting...';
            else btn.textContent = 'Connect';
        },

        // What the Go side says about the console, rather than what this
        // window last remembered. The two drift whenever a socket is replaced.
        async refreshConsoleState() {
            try {
                this.consoleConnected = await window.go.main.App.ConsoleConnected();
            } catch (err) {
                this.consoleConnected = false;
            }
            this.paintConnectButton();
            return this.consoleConnected;
        },

        // Connect if not already. connectConsole() toggles, so auto-connecting
        // through it would disconnect a console that was already up.
        async ensureConsole() {
            if (this.consoleConnected || this.consoleConnecting) return;
            // A retry that is already booked would open a second socket a
            // moment after this one. This connect is that retry.
            this.cancelConsoleRetry();
            this.consoleConnecting = true;
            this.consoleWanted = true;
            this.consoleRetries = 0;
            try {
                await window.go.main.App.ConnectConsole();
            } catch (err) {
                console.warn('Console auto-connect failed:', err);
            } finally {
                this.consoleConnecting = false;
            }
        },

        /**
         * Pulls the server's log file in.
         *
         * The websocket only replays what wings still has buffered, which is a
         * few dozen lines — everything older lives in the log file, so that is
         * where it has to come from.
         */
        async loadServerLog() {
            const busy = window.UX.toast.show('Reading the log file…', { duration: 30000 });
            try {
                const tail = await window.go.main.App.GetServerLogTail(1000);
                busy.dismiss();

                if (!tail.found) {
                    window.UX.toast.warn('No log file found in the usual places for this egg');
                    return;
                }

                const consoleEl = document.getElementById('console');
                if (consoleEl) consoleEl.innerHTML = '';

                this.appendConsole('=== ' + tail.path +
                    (tail.truncated ? ' (last ' + tail.lines + ' lines)' : '') + ' ===', 'info');
                tail.content.split('\n').forEach(line => this.appendConsole(line));
                this.appendConsole('=== end of file; live output continues below ===', 'info');

                window.UX.toast.ok('Loaded ' + tail.lines + ' lines from ' + tail.path);
            } catch (err) {
                busy.dismiss();
                window.UX.toast.bad('Could not read the log: ' + err);
            }
        },
        
        async sendCommand() {
            const input = document.getElementById('commandInput');
            if (!input) return;
            
            const command = input.value.trim();
            if (!command) return;

            this.rememberCommand(command);
            this.commandDraft = '';
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

                // A server on another panel moves the panel too, and the
                // dropdowns still held the old one's list — so setting the
                // value silently did nothing and the header read the old panel
                // and "No Server". Both are rebuilt from what is now active.
                await this.loadPanels();
                await this.loadServers();

                const dropdown = document.getElementById('serverDropdown');
                if (dropdown && dropdown.value !== serverID) {
                    // Only if the reload did not already select it, which it
                    // does whenever the server is on the panel now active.
                    dropdown.value = serverID;
                }
            } catch (err) {
                console.error('Failed to switch server:', err);
                window.UX.toast.bad('Could not switch server: ' + err);
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
        
        /**
         * Short relative age: minutes and hours for today, days for the last
         * fortnight, a date after that. A file list is scanned, not read, so
         * the useful form is the one that fits in eight characters.
         */
        /**
         * The date column.
         *
         * Built once and reused. toLocaleDateString with an options object
         * constructs a fresh Intl.DateTimeFormat on every call — about 35
         * microseconds — which made this two thirds of the time it took to draw
         * a folder: 30 ms of the 44 ms for 800 rows.
         *
         * The answers are memoised too. A listing repeats timestamps, and a
         * re-render repeats all of them; the cache is dropped when the minute
         * turns so "5m" does not go stale.
         */
        _dateFmt: null,

        dateFormatters() {
            if (!this._dateFmt) {
                this._dateFmt = {
                    sameYear: new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }),
                    otherYear: new Intl.DateTimeFormat(undefined, { year: '2-digit', month: 'short' }),
                    full: new Intl.DateTimeFormat(undefined, {
                        dateStyle: 'medium', timeStyle: 'short'
                    })
                };
            }
            return this._dateFmt;
        },

        formatWhen(value) {
            if (!value) return '';

            // One clock reading per minute rather than two Date objects per
            // row. Relative labels are minute-grained anyway.
            const now = Date.now();
            const bucket = Math.floor(now / 60000);
            if (this._whenBucket !== bucket) {
                this._whenBucket = bucket;
                this._whenCache = new Map();
                this._thisYear = new Date(now).getFullYear();
            }

            const hit = this._whenCache.get(value);
            if (hit !== undefined) return hit;

            const then = new Date(value);
            const at = then.getTime();
            if (isNaN(at)) {
                this._whenCache.set(value, '');
                return '';
            }

            const secs = (now - at) / 1000;
            let out;
            if (secs < 90) out = 'now';
            else if (secs < 3600) out = Math.round(secs / 60) + 'm';
            else if (secs < 86400) out = Math.round(secs / 3600) + 'h';
            else if (secs < 14 * 86400) out = Math.round(secs / 86400) + 'd';
            else {
                const fmt = this.dateFormatters();
                out = (then.getFullYear() === this._thisYear ? fmt.sameYear : fmt.otherYear).format(then);
            }

            this._whenCache.set(value, out);
            return out;
        },

        formatWhenFull(value) {
            if (!value) return '';

            if (!this._fullCache) this._fullCache = new Map();
            const hit = this._fullCache.get(value);
            if (hit !== undefined) return hit;

            const then = new Date(value);
            const out = isNaN(then.getTime()) ? '' : this.dateFormatters().full.format(then);
            // An absolute date never changes, so this one never expires. Capped
            // so a long session browsing large folders cannot grow it without
            // limit.
            if (this._fullCache.size > 4000) this._fullCache.clear();
            this._fullCache.set(value, out);
            return out;
        },

        /** Adds the permission column. */
        toggleFileDetails() {
            const tree = document.getElementById('fileTree');
            if (!tree) return;
            const on = tree.classList.toggle('details');
            try { localStorage.setItem('fileDetails', on ? '1' : '0'); } catch (err) { /* private mode */ }
            window.UX.toast.show(on ? 'Showing permissions' : 'Permissions hidden', { duration: 1200 });
        },

        restoreFileDetails() {
            let on = false;
            try { on = localStorage.getItem('fileDetails') === '1'; } catch (err) { /* private mode */ }
            const tree = document.getElementById('fileTree');
            if (tree && on) tree.classList.add('details');
        },

        // Utilities
        formatSize(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
        },
        
        // Quotes included: this goes into attributes as often as into text,
        // and textContent -> innerHTML leaves them alone.
        escapeHtml(text) {
            return String(text == null ? '' : text)
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#39;');
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
