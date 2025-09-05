// Ultra-fast console implementation using virtual scrolling
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime.js';
import { ConnectConsole, SendCommand, SetPowerState } from '../wailsjs/go/main/App.js';

// Minimal ANSI -> HTML converter
function escapeHtml(str) {
    return str
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/\"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

function ansiToHtml(input) {
    let html = '';
    let i = 0;
    let open = false;
    let style = { fg: null, bg: null, bold: false };

    const openSpan = () => {
        const classes = ['ansi'];
        if (style.bold) classes.push('bold');
        if (style.fg != null) classes.push(`fg-${style.fg}`);
        if (style.bg != null) classes.push(`bg-${style.bg}`);
        html += `<span class="${classes.join(' ')}">`;
        open = true;
    };
    const closeSpan = () => {
        if (open) {
            html += '</span>';
            open = false;
        }
    };

    while (i < input.length) {
        const ch = input[i];
        if (ch === "\x1b" && input[i+1] === '[') {
            // Flush current span
            closeSpan();
            // Parse CSI parameters until 'm'
            i += 2; // skip ESC[
            let params = '';
            while (i < input.length && input[i] !== 'm') {
                params += input[i++];
            }
            // Skip 'm'
            i++;
            // Apply params
            const codes = params.split(';').filter(Boolean).map(n => parseInt(n, 10));
            if (codes.length === 0) codes.push(0);
            for (const code of codes) {
                if (code === 0) { // reset
                    style = { fg: null, bg: null, bold: false };
                } else if (code === 1) {
                    style.bold = true;
                } else if (code === 22) {
                    style.bold = false;
                } else if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
                    style.fg = code;
                } else if (code === 39) {
                    style.fg = null;
                } else if ((code >= 40 && code <= 47) || (code >= 100 && code <= 107)) {
                    style.bg = code;
                } else if (code === 49) {
                    style.bg = null;
                }
            }
            // Open new span if style is non-default
            if (style.bold || style.fg != null || style.bg != null) {
                openSpan();
            }
            continue;
        }
        // Normal text
        if (!open && (style.bold || style.fg != null || style.bg != null)) {
            openSpan();
        }
        html += escapeHtml(ch);
        i++;
    }
    closeSpan();
    return html;
}

class FastConsole {
    constructor() {
        this.consoleEl = document.getElementById('console');
        this.commandInput = document.getElementById('commandInput');
        this.lines = [];
        this.maxLines = 10000;
        this.batchSize = 100;
        this.pendingLines = [];
        this.updateScheduled = false;
        
        this.init();
    }
    
    init() {
        // Set up event listeners
        EventsOn('console-output', (message) => {
            this.appendLine(message);
        });
        
        EventsOn('console-connected', (connected) => {
            this.appendLine('=== Console connected ===', 'info');
            document.getElementById('connectBtn').textContent = 'Disconnect';
        });
        
        EventsOn('console-error', (error) => {
            this.appendLine(`Error: ${error}`, 'error');
        });
        
        // Command input
        this.commandInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                this.sendCommand();
            }
        });
        
        // Start batch processor
        setInterval(() => this.processBatch(), 50); // 20 FPS max
    }
    
    appendLine(text, type = '') {
        // Add to pending
        this.pendingLines.push({ text, type, timestamp: Date.now() });
        
        // Limit pending size
        if (this.pendingLines.length > 1000) {
            this.pendingLines = this.pendingLines.slice(-800);
        }
    }
    
    processBatch() {
        if (this.pendingLines.length === 0) return;
        
        // Take pending lines
        const toProcess = this.pendingLines.splice(0, this.batchSize);
        
        // Add to main buffer
        this.lines.push(...toProcess);
        
        // Trim if needed
        if (this.lines.length > this.maxLines) {
            this.lines = this.lines.slice(-this.maxLines);
        }
        
        // Render using DocumentFragment for performance
        this.render(toProcess);
    }
    
    render(newLines) {
        // Create fragment for batch insert
        const fragment = document.createDocumentFragment();
        
        for (const line of newLines) {
            const div = document.createElement('div');
            div.className = 'console-line';
            if (line.type) {
                div.classList.add(line.type);
            }
            // Render ANSI with colors
            div.innerHTML = ansiToHtml(line.text);
            fragment.appendChild(div);
        }
        
        // Append all at once
        this.consoleEl.appendChild(fragment);
        
        // Remove old lines if too many DOM nodes
        while (this.consoleEl.children.length > 1000) {
            this.consoleEl.removeChild(this.consoleEl.firstChild);
        }
        
        // Auto-scroll
        this.consoleEl.scrollTop = this.consoleEl.scrollHeight;
    }
    
    clear() {
        this.lines = [];
        this.pendingLines = [];
        this.consoleEl.innerHTML = '';
        this.appendLine('Console cleared', 'info');
    }
    
    async sendCommand() {
        const command = this.commandInput.value.trim();
        if (!command) return;
        
        this.appendLine(`> ${command}`, 'command');
        this.commandInput.value = '';
        
        try {
            await SendCommand(command);
        } catch (err) {
            this.appendLine(`Failed to send command: ${err}`, 'error');
        }
    }
}

// Export functions for HTML onclick
window.app = {
    console: null,
    
    init() {
        this.console = new FastConsole();
    },
    
    async connectConsole() {
        try {
            await ConnectConsole();
        } catch (err) {
            console.error('Failed to connect:', err);
        }
    },
    
    async sendPower(signal) {
        try {
            await SetPowerState(signal);
        } catch (err) {
            console.error('Failed to send power signal:', err);
        }
    },
    
    sendCommand() {
        this.console.sendCommand();
    },
    
    clearConsole() {
        this.console.clear();
    },
    
    showSettings() {
        // TODO: Implement settings modal
    }
};

// Initialize on load
document.addEventListener('DOMContentLoaded', () => {
    window.app.init();
});
