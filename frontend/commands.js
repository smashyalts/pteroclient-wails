/**
 * Every hotkey in the app, in one list.
 *
 * Loaded last, so window.app, Shell, Vault and SplitView all exist. Each entry
 * carries its own `when`, and a command whose `when` is false lets the key fall
 * through — Ctrl+W closes a file tab on the Files tab and does nothing on
 * Console, rather than being swallowed everywhere.
 */
(function () {
    'use strict';

    const app = () => window.app;
    const tab = () => (window.Shell ? window.Shell.currentTab() : '');
    const onFiles = () => tab() === 'files';
    const hasOpenFile = () => !!(app() && app().activeFile);

    // The split view owns the keyboard while the caret is inside it: each
    // workspace saves its own focused tab, which the main editor's Ctrl+S
    // knows nothing about.
    const inSplit = () => !!(document.activeElement && document.activeElement.closest &&
        document.activeElement.closest('#splitRoot'));

    // Escape has two jobs. Closing whatever is on top comes first, or a dialog
    // opened over a selected file could not be dismissed.
    const overlayOpen = () => !!document.querySelector('.modal.show, .palette.show');

    function register() {
        const R = window.UX.registerCommand;

        /* ------------------------------------------------------ navigation */

        R({
            id: 'palette.servers', group: 'Navigation', key: 'Ctrl+K',
            label: 'Switch server',
            allowInField: true,
            run: () => window.UX.openPalette('servers')
        });

        R({
            id: 'palette.all', group: 'Navigation', key: 'Ctrl+Shift+P',
            label: 'Command palette',
            allowInField: true,
            run: () => window.UX.openPalette('all')
        });

        const TABS = [
            ['console', 'Console'], ['files', 'Files'], ['databases', 'Databases'],
            ['schedules', 'Schedules'], ['users', 'Users'], ['network', 'Network'],
            ['startup', 'Startup'], ['vault', 'Vault'], ['activity', 'Activity'],
            ['settings', 'Settings']
        ];
        TABS.forEach((entry, i) => {
            R({
                id: 'tab.' + entry[0], group: 'Navigation',
                key: i < 9 ? 'Ctrl+' + (i + 1) : '',
                label: 'Go to ' + entry[1],
                run: () => window.Shell.showTab(entry[0])
            });
        });

        R({
            id: 'nav.back', group: 'Navigation', key: 'Alt+ArrowLeft',
            label: 'Back (file tree)',
            when: onFiles,
            run: () => app().navigateBack()
        });

        R({
            id: 'nav.forward', group: 'Navigation', key: 'Alt+ArrowRight',
            label: 'Forward (file tree)',
            when: onFiles,
            run: () => app().navigateForward()
        });

        R({
            id: 'nav.up', group: 'Navigation', key: 'Alt+ArrowUp',
            label: 'Up one folder',
            when: onFiles,
            run: () => {
                const parts = String(app().currentPath || '/').split('/').filter(Boolean);
                parts.pop();
                app().loadFiles(parts.length ? '/' + parts.join('/') : '/');
            }
        });

        /* ----------------------------------------------------------- files */

        R({
            id: 'files.newFile', group: 'Files', key: 'Ctrl+N',
            label: 'New file',
            when: onFiles,
            run: () => app().newFile()
        });

        R({
            id: 'files.newFolder', group: 'Files', key: 'Ctrl+Shift+N',
            label: 'New folder',
            when: onFiles,
            run: () => app().newFolder()
        });

        R({
            id: 'files.find', group: 'Files', key: 'Ctrl+F',
            label: 'Filter this folder',
            allowInField: true,
            when: onFiles,
            run: () => {
                // In the split view each workspace has its own box; the
                // focused one is the one being looked at.
                if (window.SplitView && window.SplitView.isActive()) return window.SplitView.focusFilter();
                app().focusFilter();
            }
        });

        R({
            id: 'files.refresh', group: 'Files', key: 'F5',
            label: 'Refresh the file list',
            when: onFiles,
            run: () => app().refreshFiles()
        });

        R({
            id: 'files.upload', group: 'Files', key: 'Ctrl+U',
            label: 'Upload files',
            when: onFiles,
            run: () => app().uploadFile()
        });

        R({
            id: 'files.selectAll', group: 'Files', key: 'Ctrl+A',
            label: 'Select everything in this folder',
            when: onFiles,
            run: () => app().selectAllFiles()
        });

        R({
            id: 'files.clearSelection', group: 'Files', key: 'Escape',
            label: 'Clear the selection',
            when: () => onFiles() && !overlayOpen() && app().selection.size > 0,
            run: () => app().clearSelection()
        });

        R({
            id: 'files.delete', group: 'Files', key: 'Delete',
            label: 'Delete the selection',
            when: () => onFiles() && app().selection.size > 0,
            run: () => app().deleteSelected()
        });

        R({
            id: 'files.rename', group: 'Files', key: 'F2',
            label: 'Rename',
            when: () => onFiles() && app().selection.size === 1,
            run: () => app().renameFile()
        });

        R({
            id: 'files.archive', group: 'Files', key: 'Ctrl+Shift+A',
            label: 'Archive the selection',
            when: () => onFiles() && app().selection.size > 0,
            run: () => app().archiveSelected()
        });

        R({
            id: 'files.extract', group: 'Files', key: 'Ctrl+Shift+E',
            label: 'Extract the selected archive',
            when: () => onFiles() && app().selection.size === 1,
            run: () => app().extractSelected()
        });

        R({
            id: 'files.copyPath', group: 'Files', key: 'Ctrl+Shift+C',
            label: 'Copy path',
            when: () => onFiles() && app().selection.size === 1,
            run: () => app().copyPath()
        });

        /* --------------------------------------------------------- editing */

        R({
            id: 'file.save', group: 'Editor', key: 'Ctrl+S',
            label: 'Save',
            allowInField: true,
            when: () => hasOpenFile() && !inSplit(),
            run: () => app().saveFile()
        });

        R({
            id: 'file.saveAll', group: 'Editor', key: 'Ctrl+Shift+S',
            label: 'Save every open file',
            allowInField: true,
            when: () => !!(app() && app().openFiles.size) && !inSplit(),
            run: () => app().saveAllFiles()
        });

        R({
            id: 'file.close', group: 'Editor', key: 'Ctrl+W',
            label: 'Close the open file',
            allowInField: true,
            when: () => hasOpenFile() && !inSplit(),
            run: () => app().closeFile()
        });

        R({
            id: 'file.history', group: 'Editor', key: 'Ctrl+H',
            label: 'History of the open file',
            when: hasOpenFile,
            run: () => window.Vault.showHistoryFor(app().activeFile)
        });

        R({
            id: 'file.format', group: 'Editor', key: 'Alt+Shift+F',
            label: 'Format the document',
            allowInField: true,
            when: () => !!(app() && app().formatDocument && hasOpenFile()),
            run: () => app().formatDocument()
        });

        /* ------------------------------------------------------------ view */

        R({
            id: 'view.split', group: 'View', key: 'Ctrl+\\',
            label: 'Toggle split view',
            when: onFiles,
            run: () => window.SplitView.toggle()
        });

        R({
            id: 'view.dock', group: 'View', key: 'Ctrl+Shift+B',
            label: 'Move the file tree (left / right / bottom / hidden)',
            when: onFiles,
            run: () => app().cycleTreeDock()
        });

        R({
            id: 'view.zoomIn', group: 'View', key: 'Ctrl+=',
            label: 'Zoom in',
            allowInField: true,
            run: () => window.UX.zoomIn()
        });

        R({
            id: 'view.zoomOut', group: 'View', key: 'Ctrl+-',
            label: 'Zoom out',
            allowInField: true,
            run: () => window.UX.zoomOut()
        });

        R({
            id: 'view.zoomReset', group: 'View', key: 'Ctrl+0',
            label: 'Reset zoom',
            allowInField: true,
            run: () => window.UX.zoomReset()
        });

        /* --------------------------------------------------------- console */

        R({
            id: 'console.reconnect', group: 'Console', key: '',
            label: 'Reconnect the console',
            run: () => app().connectConsole()
        });

        R({
            id: 'console.loadLog', group: 'Console', key: '',
            label: 'Load the full log file',
            run: () => app().loadServerLog()
        });

        R({
            id: 'console.clear', group: 'Console', key: '',
            label: 'Clear the console',
            run: () => app().clearConsole()
        });

        /* ------------------------------------------------------------ help */

        R({
            id: 'help.hotkeys', group: 'Help', key: 'Ctrl+/',
            label: 'Hotkeys',
            allowInField: true,
            run: () => window.UX.openHotkeys()
        });

        window.UX.refreshBindings();
    }

    // window.app is created asynchronously once the Wails runtime is up.
    function waitForApp() {
        if (window.app && window.UX && window.Shell && window.SplitView) {
            register();
            return;
        }
        setTimeout(waitForApp, 60);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', waitForApp);
    } else {
        waitForApp();
    }
})();
