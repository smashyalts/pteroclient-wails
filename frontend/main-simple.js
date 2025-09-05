// Simplified non-module version for testing
console.log('Main script loading...');

// Wait for runtime to be available
function waitForRuntime() {
    if (typeof window.go !== 'undefined') {
        console.log('Runtime ready, initializing app...');
        initApp();
    } else {
        console.log('Waiting for runtime...');
        setTimeout(waitForRuntime, 100);
    }
}

function initApp() {
    console.log('Starting app initialization...');
    
    // Create the app object
    window.app = {
        // Test function
        showSettings: function() {
            console.log('Settings clicked!');
            const modal = document.getElementById('settingsModal');
            if (modal) {
                modal.classList.add('show');
                console.log('Modal shown');
            } else {
                console.log('Modal not found!');
            }
        },
        
        closeSettings: function() {
            const modal = document.getElementById('settingsModal');
            if (modal) modal.classList.remove('show');
        },
        
        switchTab: function(tabName) {
            console.log('Switching to tab:', tabName);
            // Update tabs
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            if (event && event.target) {
                event.target.classList.add('active');
            }
            
            // Update content
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            const tabContent = document.getElementById(tabName + 'Tab');
            if (tabContent) {
                tabContent.classList.add('active');
            }
        },
        
        refreshFiles: function() {
            console.log('Refresh files clicked');
            alert('Refresh files clicked');
        },
        
        uploadFile: function() {
            console.log('Upload clicked');
            document.getElementById('fileInput').click();
        },
        
        newFolder: function() {
            console.log('New folder clicked');
            const name = prompt('Folder name:');
            console.log('Folder name:', name);
        },
        
        deleteSelected: function() {
            console.log('Delete clicked');
            alert('Delete clicked');
        },
        
        connectConsole: function() {
            console.log('Connect console clicked');
            alert('Connect console clicked');
        },
        
        clearConsole: function() {
            console.log('Clear console clicked');
            const consoleEl = document.getElementById('console');
            if (consoleEl) consoleEl.innerHTML = '';
        },
        
        sendCommand: function() {
            console.log('Send command clicked');
            const input = document.getElementById('commandInput');
            if (input) {
                console.log('Command:', input.value);
                input.value = '';
            }
        },
        
        sendPower: function(signal) {
            console.log('Power signal:', signal);
            alert('Power: ' + signal);
        },
        
        saveSettings: async function() {
            console.log('Save settings clicked');
            const panelURL = document.getElementById('panelUrl')?.value;
            const apiKey = document.getElementById('apiKey')?.value;
            const serverID = document.getElementById('serverId')?.value;
            
            console.log('Settings:', { panelURL, apiKey, serverID });
            
            if (!panelURL || !apiKey || !serverID) {
                alert('All fields are required');
                return;
            }
            
            // Try to call backend
            try {
                if (window.go && window.go.main && window.go.main.App && window.go.main.App.SaveConfig) {
                    await window.go.main.App.SaveConfig(panelURL, apiKey, serverID);
                    this.closeSettings();
                    alert('Settings saved!');
                } else {
                    console.error('Backend not available');
                    alert('Backend not connected');
                }
            } catch (err) {
                console.error('Save failed:', err);
                alert('Failed to save: ' + err);
            }
        },
        
        // Context menu functions
        openFile: function() { console.log('Open file'); },
        editFile: function() { console.log('Edit file'); },
        renameFile: function() { console.log('Rename file'); },
        downloadFile: function() { console.log('Download file'); },
        deleteFile: function() { console.log('Delete file'); }
    };
    
    console.log('App object created:', window.app);
    
    // Test if we can access Go functions
    if (window.go && window.go.main && window.go.main.App) {
        console.log('Go functions available!');
        console.log('Available methods:', Object.keys(window.go.main.App));
    } else {
        console.log('Go functions NOT available');
    }
}

// Start when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', waitForRuntime);
} else {
    waitForRuntime();
}
