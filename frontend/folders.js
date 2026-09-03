/**
 * What folders exist, so Ctrl+K can jump straight to one.
 *
 * The panel has no search endpoint, and walking a tree to find a folder is one
 * request per directory — far too slow to sit behind a keystroke. So this
 * remembers instead of searching.
 *
 * Two things are recorded whenever a folder is listed:
 *
 *   - the folder itself, as visited
 *   - every subfolder in the listing, as known
 *
 * The second is what makes this useful. Opening /plugins means every plugin's
 * folder is findable by name from then on, without ever having opened one.
 *
 * It is a cache, not a record: entries are per server, capped, and the
 * least recently seen go first. Nothing here is authoritative — a folder that
 * has since been deleted is dropped the moment navigating to it fails.
 */
(function () {
    'use strict';

    const KEY = 'folderMemory';

    // Enough to hold a big plugins directory several times over, small enough
    // that the whole thing stays a few hundred KB of localStorage.
    const MAX_PER_SERVER = 1200;
    const MAX_SERVERS = 12;

    // { serverID: { path: lastSeenMillis } }
    let memory = null;

    function load() {
        if (memory) return memory;
        try {
            const raw = localStorage.getItem(KEY);
            const parsed = raw ? JSON.parse(raw) : null;
            memory = (parsed && typeof parsed === 'object') ? parsed : {};
        } catch (err) {
            memory = {};
        }
        return memory;
    }

    let writeTimer = null;

    function save() {
        // Listings arrive in bursts; writing once per folder would serialise
        // the whole cache on every keystroke of navigation.
        if (writeTimer) return;
        writeTimer = setTimeout(() => {
            writeTimer = null;
            try {
                localStorage.setItem(KEY, JSON.stringify(memory));
            } catch (err) { /* private mode, or the quota */ }
        }, 400);
    }

    function join(dir, name) {
        if (!dir || dir === '/') return '/' + name;
        return dir.replace(/\/+$/, '') + '/' + name;
    }

    function trim(forServer) {
        const paths = Object.keys(forServer);
        if (paths.length <= MAX_PER_SERVER) return;
        // Oldest first, and drop back to nine tenths so this does not run on
        // every single insert once the cap is reached.
        paths.sort((a, b) => forServer[a] - forServer[b]);
        const cut = paths.length - Math.floor(MAX_PER_SERVER * 0.9);
        for (let i = 0; i < cut; i++) delete forServer[paths[i]];
    }

    function trimServers(store) {
        const ids = Object.keys(store);
        if (ids.length <= MAX_SERVERS) return;
        // Newest activity per server decides which servers stay.
        const freshest = (id) => Math.max.apply(null, [0].concat(Object.values(store[id])));
        ids.sort((a, b) => freshest(a) - freshest(b));
        for (let i = 0; i < ids.length - MAX_SERVERS; i++) delete store[ids[i]];
    }

    /**
     * Records one listing.
     *
     * `entries` is whatever the file list handed back; anything with a truthy
     * isDir (or is_dir) counts as a folder.
     */
    function remember(serverID, path, entries) {
        if (!serverID || !path) return;
        const store = load();
        const forServer = store[serverID] || (store[serverID] = {});
        const now = Date.now();

        forServer[path] = now;

        (entries || []).forEach((entry) => {
            if (!entry) return;
            const isDir = entry.isDir || entry.is_dir;
            const name = entry.name;
            if (!isDir || !name || name === '..') return;
            const full = join(path, name);
            // Only refresh what is already known if it is stale, so a listing
            // does not reorder everything it contains ahead of somewhere the
            // user actually went.
            if (!forServer[full]) forServer[full] = now - 1;
        });

        trim(forServer);
        trimServers(store);
        save();
    }

    // Somewhere that turned out not to exist is worse than not knowing it.
    function forget(serverID, path) {
        const store = load();
        if (!store[serverID]) return;
        delete store[serverID][path];
        save();
    }

    /** Every known folder for one server, most recently seen first. */
    function list(serverID) {
        if (!serverID) return [];
        const forServer = load()[serverID];
        if (!forServer) return [];
        return Object.keys(forServer).sort((a, b) => forServer[b] - forServer[a]);
    }

    function clear(serverID) {
        const store = load();
        if (serverID) delete store[serverID];
        else memory = {};
        save();
    }

    function count(serverID) {
        const forServer = load()[serverID];
        return forServer ? Object.keys(forServer).length : 0;
    }

    /**
     * Resolves what someone typed into a path.
     *
     * "/a/b" is from the root. "~/a" is from where they are. Anything else is
     * not a path at all, and the caller treats it as a search.
     */
    function resolve(typed, currentPath) {
        const raw = String(typed || '').trim();
        if (!raw) return null;

        let out;
        if (raw.indexOf('~/') === 0) {
            const base = currentPath && currentPath !== '/' ? currentPath.replace(/\/+$/, '') : '';
            out = base + '/' + raw.slice(2);
        } else if (raw.charAt(0) === '/') {
            out = raw;
        } else {
            return null;
        }

        // Collapse . and .., the same way the Go side does before it will
        // touch anything.
        const parts = [];
        out.split('/').forEach((part) => {
            if (!part || part === '.') return;
            if (part === '..') { parts.pop(); return; }
            parts.push(part);
        });
        return '/' + parts.join('/');
    }

    window.Folders = {
        remember: remember,
        forget: forget,
        list: list,
        clear: clear,
        count: count,
        resolve: resolve
    };
}());
