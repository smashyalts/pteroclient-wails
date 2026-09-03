package main

// Safe file operations.
//
// Everything in this file exists to make two classes of mistake impossible
// rather than merely unlikely:
//
//   1. Overwriting a panel file and having no way back. Every write takes a
//      local copy of the *remote* bytes first, and a write is refused outright
//      when the remote file changed under the editor since it was opened.
//
//   2. Deleting the wrong thing. A delete is a two-step handshake: the caller
//      asks for a plan, the plan is shown, and the execute call must quote the
//      plan's token back. The token is derived from the exact path set, so a
//      stale selection, a re-render, or a second click cannot delete anything
//      the user did not see listed.
//
// Deleted content lands in a local recycle bin (100 MB, oldest-first
// eviction) so an accepted delete is still recoverable.

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/pterodactyl"
	"pteroclient-wails/pkg/safestore"
)

/* ------------------------------------------------------------ path safety */

// normalizeRemotePath turns whatever the UI sent into a clean absolute panel
// path, or refuses it. Refusing is the point: `..`, backslashes and empty
// segments are how a write or delete ends up somewhere nobody chose.
func normalizeRemotePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return "", errors.New("path contains a null byte")
	}

	// The panel speaks POSIX paths; a Windows-style separator arriving here is
	// a bug in the caller, not a path component.
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	cleaned := path.Clean(p)
	if cleaned == "." {
		cleaned = "/"
	}
	// path.Clean leaves a leading ".." in place; that would escape the root.
	if !strings.HasPrefix(cleaned, "/") || cleaned == "/.." || strings.HasPrefix(cleaned, "/../") {
		return "", fmt.Errorf("path escapes the server root: %s", p)
	}
	return cleaned, nil
}

// splitRemote splits a normalized path into the directory and the entry name
// the panel's file routes expect. The root itself has no name and is refused.
func splitRemote(p string) (dir, name string, err error) {
	cleaned, err := normalizeRemotePath(p)
	if err != nil {
		return "", "", err
	}
	if cleaned == "/" {
		return "", "", errors.New("refusing to operate on the server root itself")
	}

	dir = path.Dir(cleaned)
	name = path.Base(cleaned)
	if name == "" || name == "." || name == ".." || name == "/" {
		return "", "", fmt.Errorf("not a valid entry name: %q", name)
	}
	return dir, name, nil
}

// joinRemote appends a single entry name to a directory path.
func joinRemote(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

/* ------------------------------------------------------- protected paths */

// Protection levels, lowest first.
const (
	ProtectNone      = "none"
	ProtectSensitive = "sensitive" // recoverable but worth naming in the dialog
	ProtectCritical  = "critical"  // losing this usually means losing the server
)

// Names that carry server state or configuration. Matched on the basename,
// case-insensitively. This is a warning list, not a block list — the point is
// that the confirm dialog says what is about to go.
var criticalNames = map[string]string{
	"server.properties":        "the server configuration",
	"eula.txt":                 "the EULA acceptance",
	"ops.json":                 "the operator list",
	"whitelist.json":           "the whitelist",
	"banned-players.json":      "the ban list",
	"banned-ips.json":          "the IP ban list",
	"permissions.yml":          "the permission map",
	"bukkit.yml":               "server configuration",
	"spigot.yml":               "server configuration",
	"paper.yml":                "server configuration",
	"paper-global.yml":         "server configuration",
	"paper-world-defaults.yml": "server configuration",
	"velocity.toml":            "proxy configuration",
	"config.yml":               "configuration",
	"settings.yml":             "configuration",
	"level.dat":                "world data",
	"level.dat_old":            "world data",
	"usercache.json":           "the user cache",
	".env":                     "environment secrets",
	"docker-compose.yml":       "container configuration",
}

// Directory names that hold world or player state.
var criticalDirs = map[string]string{
	"world":         "a world",
	"world_nether":  "a world",
	"world_the_end": "a world",
	"playerdata":    "player data",
	"region":        "world regions",
	"stats":         "player statistics",
	"advancements":  "player advancements",
	"plugins":       "the plugin set",
	"mods":          "the mod set",
	"config":        "configuration",
	"backups":       "on-server backups",
}

var sensitiveExts = map[string]string{
	".jar":     "a server or plugin binary",
	".db":      "a database",
	".sqlite":  "a database",
	".sqlite3": "a database",
	".mv.db":   "a database",
	".key":     "a key file",
	".pem":     "a key file",
}

// classifyPath reports how carefully a path should be treated.
func classifyPath(p string, isDir bool) (level, reason string) {
	base := strings.ToLower(path.Base(p))

	if isDir {
		if why, ok := criticalDirs[base]; ok {
			return ProtectCritical, why
		}
		return ProtectSensitive, "a directory and everything under it"
	}

	if why, ok := criticalNames[base]; ok {
		return ProtectCritical, why
	}
	for ext, why := range sensitiveExts {
		if strings.HasSuffix(base, ext) {
			return ProtectSensitive, why
		}
	}
	return ProtectNone, ""
}

/* --------------------------------------------------- client resolution */

// withServerClient runs fn against a client pointed at serverID, whichever
// panel that server lives on, and always restores the active client's server
// ID afterwards. Every safe operation goes through here so a mid-operation
// error cannot leave the shared client aimed at someone else's server.
func (a *App) withServerClient(serverID string, fn func(c *pterodactyl.Client, panel string) error) error {
	if a.client == nil {
		return errors.New("not connected")
	}

	if serverID == "" {
		serverID = a.client.GetServerID()
	}
	if serverID == "" {
		return errors.New("no server selected")
	}

	if a.serverPanelMap == nil {
		a.serverPanelMap = make(map[string]string)
	}
	panelName, ok := a.serverPanelMap[serverID]
	if !ok {
		a.RefreshAllServerMappings()
		panelName, ok = a.serverPanelMap[serverID]
		if !ok {
			return fmt.Errorf("server %s is not on any configured panel", serverID)
		}
	}

	if panelName == a.config.GetActivePanelName() {
		previous := a.client.GetServerID()
		a.client.SetServerID(serverID)
		defer a.client.SetServerID(previous)
		return fn(a.client, panelName)
	}

	for _, panel := range a.config.GetPanels() {
		if panel.Name != panelName {
			continue
		}
		panelURL := panel.PanelURL
		if !strings.HasPrefix(panelURL, "http://") && !strings.HasPrefix(panelURL, "https://") {
			panelURL = "https://" + panelURL
		}
		tmp := pterodactyl.NewClient(panelURL, panel.APIKey, serverID)
		defer tmp.Close()
		return fn(tmp, panelName)
	}

	return fmt.Errorf("panel %q is no longer configured", panelName)
}

// activeServerID is the server the main editor is pointed at.
func (a *App) activeServerID() string {
	if a.client == nil {
		return ""
	}
	return a.client.GetServerID()
}

/* ------------------------------------------------------- local capture */

// captureRemote copies the current remote bytes of a path into one of the two
// local stores before that path is written over or removed.
//
// kind decides which: KindVersion for an edit, where the file survives and this
// is one more state in its history; KindBin for content the app is taking away
// wholesale — a delete, an upload landing on an existing name, a restore
// writing over what is there.
//
// A file too large to hold still gets an entry, marked uncaptured, so the UI
// can say plainly that this one is not recoverable rather than implying it is.
func (a *App) captureRemote(kind safestore.Kind, c *pterodactyl.Client, panel, serverID, remotePath, reason string, knownSize int64) (safestore.Entry, error) {
	if a.store == nil {
		return safestore.Entry{}, errors.New("local store unavailable")
	}

	meta := safestore.Entry{
		Panel:  panel,
		Server: serverID,
		Path:   remotePath,
		Reason: reason,
	}

	if knownSize > safestore.MaxSingleCapture {
		meta.Note = fmt.Sprintf("file is %d bytes, over the %d byte capture limit", knownSize, safestore.MaxSingleCapture)
		return a.store.Put(kind, meta, nil)
	}

	// The download endpoint rather than /files/contents: the latter is the
	// file editor's, and the panel refuses anything past a few megabytes with
	// FileSizeTooLargeException — so the store could never hold a jar or a zip.
	content, err := c.DownloadFileBytes(remotePath, safestore.MaxSingleCapture)
	if err != nil {
		return safestore.Entry{}, err
	}

	entry, err := a.store.Put(kind, meta, content)
	if errors.Is(err, safestore.ErrTooLarge) {
		meta.Note = err.Error()
		return a.store.Put(kind, meta, nil)
	}
	return entry, err
}

/* --------------------------------------------------------------- saving */

// SaveResult is what the editor gets back from a save attempt.
type SaveResult struct {
	Saved       bool   `json:"saved"`
	Conflict    bool   `json:"conflict"`
	Created     bool   `json:"created"`
	Reason      string `json:"reason,omitempty"`
	VersionID   string `json:"version_id,omitempty"`
	VersionSize int64  `json:"version_size,omitempty"`
	// RemoteContent is filled only on a conflict, so the editor can show what
	// is on the panel instead of guessing.
	RemoteContent string `json:"remote_content,omitempty"`
}

// SafeSaveFileContent writes a file on the active server.
//
// expected is the content the editor loaded. When it does not match what the
// panel currently holds the write is refused and the remote content is
// returned, so an edit made elsewhere is never silently overwritten. force
// skips only that check — the local backup is taken either way.
func (a *App) SafeSaveFileContent(remotePath, content, expected string, force bool) (*SaveResult, error) {
	return a.SafeSaveFileContentToServer(a.activeServerID(), remotePath, content, expected, force)
}

// SafeSaveFileContentToServer is SafeSaveFileContent for an explicit server,
// used by the split editor when a pane is on another panel.
func (a *App) SafeSaveFileContentToServer(serverID, remotePath, content, expected string, force bool) (*SaveResult, error) {
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	if _, _, err := splitRemote(cleaned); err != nil {
		return nil, err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	result := &SaveResult{}

	err = a.withServerClient(serverID, func(c *pterodactyl.Client, panel string) error {
		remote, readErr := c.GetFileContent(cleaned)
		exists := readErr == nil

		if readErr != nil && !isNotFound(readErr) {
			// A read failure that is not "no such file" means we cannot tell
			// whether we are about to clobber something. Refuse.
			return fmt.Errorf("cannot verify the current file before writing: %w", readErr)
		}

		if !exists {
			// Writing to a path that does not exist creates it. That is only
			// safe when the caller believed it was empty too; otherwise the
			// file was deleted or renamed under the editor.
			if expected != "" && !force {
				result.Conflict = true
				result.Reason = "the file no longer exists on the panel"
				return nil
			}
			result.Created = true
		} else if remote != expected && !force {
			result.Conflict = true
			result.Reason = "the file changed on the panel since it was opened"
			// The editor diffs against this. Only send something it can render:
			// a binary or very large body would cross the bridge as mojibake or
			// as several megabytes nobody reads.
			if len(remote) <= conflictPreviewMax && utf8.ValidString(remote) {
				result.RemoteContent = remote
			}
			return nil
		}

		if exists {
			// The state being replaced becomes the newest entry in this file's
			// history. A write whose history entry failed is refused: saving
			// anyway would be the one case with no way back.
			entry, captureErr := a.captureRemote(safestore.KindVersion, c, panel,
				serverIDOr(serverID, c), cleaned, "edited", int64(len(remote)))
			if captureErr != nil {
				return fmt.Errorf("refusing to write: saving the previous version locally failed: %w", captureErr)
			}
			result.VersionID = entry.ID
			result.VersionSize = entry.Size
		}

		if writeErr := c.SaveFileContent(cleaned, content); writeErr != nil {
			return writeErr
		}
		result.Saved = true
		return nil
	})

	if err != nil {
		return nil, err
	}
	if result.Saved && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "file-saved", cleaned)
	}
	return result, nil
}

// CreateFileStrict creates a new empty file and refuses if anything already
// occupies the path. The old newFile path wrote an empty string straight over
// whatever was there.
func (a *App) CreateFileStrict(remotePath string) error {
	dir, name, err := splitRemote(remotePath)
	if err != nil {
		return err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		files, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot check %s before creating the file: %w", dir, listErr)
		}
		for _, f := range files {
			if strings.EqualFold(f.Name, name) {
				return fmt.Errorf("%s already exists in %s", name, dir)
			}
		}
		return c.SaveFileContent(joinRemote(dir, name), "")
	})
}

// CreateFolderStrict creates a directory and refuses if the name is taken.
func (a *App) CreateFolderStrict(remotePath string) error {
	dir, name, err := splitRemote(remotePath)
	if err != nil {
		return err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		files, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot check %s before creating the folder: %w", dir, listErr)
		}
		for _, f := range files {
			if strings.EqualFold(f.Name, name) {
				return fmt.Errorf("%s already exists in %s", name, dir)
			}
		}
		return c.CreateDirectory(dir, name)
	})
}

// RenameFileStrict renames within a directory and refuses to land on an
// existing entry, which the panel would otherwise overwrite.
func (a *App) RenameFileStrict(oldPath, newName string) error {
	dir, oldName, err := splitRemote(oldPath)
	if err != nil {
		return err
	}
	if strings.ContainsAny(newName, `/\`) || newName == "" || newName == "." || newName == ".." {
		return fmt.Errorf("not a valid file name: %q", newName)
	}
	if newName == oldName {
		return nil
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		files, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot check %s before renaming: %w", dir, listErr)
		}
		for _, f := range files {
			if strings.EqualFold(f.Name, newName) {
				return fmt.Errorf("%s already exists in %s", newName, dir)
			}
		}
		return c.RenameFile(dir, oldName, newName)
	})
}

/* -------------------------------------------------------------- reading */

// FileRead is a file the editor opened, with the flags it needs to decide
// whether editing it is safe at all.
type FileRead struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary"`
	TooBig  bool   `json:"too_big"`
}

// EditorMaxOpenBytes caps what the editor will load. Larger files open in the
// panel or over SFTP; loading them here only invites a truncated save.
const EditorMaxOpenBytes int64 = 8 << 20 // 8 MB

// conflictPreviewMax caps the remote body returned with a save conflict, which
// exists only so the editor can show what changed.
const conflictPreviewMax = 1 << 20 // 1 MB

// ReadFileForEdit fetches a file for the editor and refuses the two cases
// that turn a save into corruption: content that is not valid UTF-8, and a
// file too large to hold in the editor intact.
func (a *App) ReadFileForEdit(serverID, remotePath string) (*FileRead, error) {
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return nil, err
	}

	out := &FileRead{Path: cleaned}

	err = a.withServerClient(serverID, func(c *pterodactyl.Client, _ string) error {
		dir, name, splitErr := splitRemote(cleaned)
		if splitErr != nil {
			return splitErr
		}

		files, listErr := c.ListFiles(dir)
		if listErr == nil {
			for _, f := range files {
				if f.Name == name {
					out.Size = f.Size
					break
				}
			}
		}

		if out.Size > EditorMaxOpenBytes {
			out.TooBig = true
			return nil
		}

		content, readErr := c.GetFileContent(cleaned)
		if readErr != nil {
			return readErr
		}
		if !utf8.ValidString(content) {
			out.Binary = true
			return nil
		}
		out.Content = content
		if out.Size == 0 {
			out.Size = int64(len(content))
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return out, nil
}

/* -------------------------------------------------------------- deleting */

// DeleteItem is one file the plan would remove.
type DeleteItem struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	IsDir      bool   `json:"is_dir"`
	Level      string `json:"level"`
	Reason     string `json:"reason,omitempty"`
	Capturable bool   `json:"capturable"`
}

// DeletePlan is the preview the UI must show before anything is removed.
type DeletePlan struct {
	Token      string       `json:"token"`
	ServerID   string       `json:"server_id"`
	Roots      []string     `json:"roots"`
	Items      []DeleteItem `json:"items"`
	FileCount  int          `json:"file_count"`
	DirCount   int          `json:"dir_count"`
	TotalBytes int64        `json:"total_bytes"`
	// CaptureBytes is what will actually land in the recycle bin.
	CaptureBytes int64    `json:"capture_bytes"`
	BinFree      int64    `json:"bin_free"`
	BinLimit     int64    `json:"bin_limit"`
	Recoverable  bool     `json:"recoverable"`
	Truncated    bool     `json:"truncated"`
	Warnings     []string `json:"warnings"`
	Critical     []string `json:"critical"`

	createdAt time.Time
}

// Enumeration bounds. A runaway tree must not turn a delete preview into a
// thousand API calls.
const (
	maxPlanEntries = 4000
	maxPlanDepth   = 24
	planTTL        = 10 * time.Minute
)

// PlanDelete walks the selection and returns what would be removed. Nothing is
// deleted here. The returned token is the only way to call SafeDeleteFiles.
func (a *App) PlanDelete(serverID string, paths []string) (*DeletePlan, error) {
	if len(paths) == 0 {
		return nil, errors.New("nothing selected")
	}
	if a.store == nil {
		return nil, errors.New("local store unavailable; refusing to delete without a recycle bin")
	}

	cleanRoots := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		cleaned, err := normalizeRemotePath(p)
		if err != nil {
			return nil, err
		}
		if cleaned == "/" {
			return nil, errors.New("refusing to delete the server root")
		}
		if _, _, err := splitRemote(cleaned); err != nil {
			return nil, err
		}
		if !seen[cleaned] {
			seen[cleaned] = true
			cleanRoots = append(cleanRoots, cleaned)
		}
	}
	sort.Strings(cleanRoots)
	cleanRoots = collapseNested(cleanRoots)

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	// Pin the server now. Leaving this empty let SafeDeleteFiles resolve it
	// again at confirm time, against whatever server had since become active —
	// so switching servers while the dialog was open deleted the plan's paths
	// on a different machine.
	if serverID == "" {
		serverID = a.activeServerID()
	}
	if serverID == "" {
		return nil, errors.New("no server selected")
	}

	plan := &DeletePlan{
		ServerID:  serverID,
		Roots:     cleanRoots,
		Items:     []DeleteItem{},
		Warnings:  []string{},
		Critical:  []string{},
		BinFree:   a.store.Free(safestore.KindBin),
		BinLimit:  a.store.Limit(safestore.KindBin),
		createdAt: time.Now(),
	}

	err := a.withServerClient(serverID, func(c *pterodactyl.Client, _ string) error {
		for _, root := range cleanRoots {
			dir, name, err := splitRemote(root)
			if err != nil {
				return err
			}

			siblings, err := c.ListFiles(dir)
			if err != nil {
				return fmt.Errorf("cannot list %s: %w", dir, err)
			}

			var found *pterodactyl.FileInfo
			for i := range siblings {
				if siblings[i].Name == name {
					found = &siblings[i]
					break
				}
			}
			if found == nil {
				// The selection is stale. Say so rather than deleting whatever
				// happens to carry that name now.
				return fmt.Errorf("%s no longer exists — refresh the file list", root)
			}

			isDir := !found.IsFile && !found.IsSymlink
			if isDir {
				plan.DirCount++
				level, why := classifyPath(root, true)
				plan.Items = append(plan.Items, DeleteItem{
					Path: root, Name: found.Name, IsDir: true,
					Level: level, Reason: why, Capturable: false,
				})
				if level == ProtectCritical {
					plan.Critical = append(plan.Critical, root+" — "+why)
				}
				if err := a.walkForDelete(c, root, 1, plan); err != nil {
					return err
				}
			} else {
				a.addPlanFile(plan, root, found.Name, found.Size)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	plan.Recoverable = plan.CaptureBytes <= plan.BinLimit && !plan.Truncated
	for _, item := range plan.Items {
		if !item.IsDir && !item.Capturable {
			plan.Recoverable = false
			break
		}
	}

	if plan.Truncated {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("more than %d entries — the listing below is partial and not everything can be backed up", maxPlanEntries))
	}
	if plan.CaptureBytes > plan.BinFree {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("the recycle bin will evict its oldest %s to make room",
				humanBytes(plan.CaptureBytes-plan.BinFree)))
	}
	if plan.CaptureBytes > plan.BinLimit {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("this is %s, over the %s recycle bin — the oldest files will not be recoverable",
				humanBytes(plan.CaptureBytes), humanBytes(plan.BinLimit)))
	}

	plan.Token = planToken(serverID, cleanRoots)

	// Lock order, for the record: fileMu then planMu. This is the only place
	// both are held; SafeDeleteFiles releases planMu before it takes fileMu,
	// so there is no cycle to deadlock on.
	a.planMu.Lock()
	if a.deletePlans == nil {
		a.deletePlans = make(map[string]*DeletePlan)
	}
	a.deletePlans[plan.Token] = plan
	a.prunePlansLocked()
	a.planMu.Unlock()

	return plan, nil
}

// collapseNested drops any path that already sits inside another in the set.
//
// Selecting a folder and something inside it is ordinary, and without this the
// inner path is planned twice — captured twice, counted twice — and then
// deleted twice, the second time against a parent that is already gone. That
// failure used to take the whole operation's recycle-bin copies with it.
//
// The input is sorted, so an ancestor always precedes its descendants.
func collapseNested(sorted []string) []string {
	out := make([]string, 0, len(sorted))
	for _, candidate := range sorted {
		covered := false
		for _, kept := range out {
			if candidate == kept || strings.HasPrefix(candidate, strings.TrimSuffix(kept, "/")+"/") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, candidate)
		}
	}
	return out
}

// ownerRoot returns the selected root a planned item belongs to, so a failure
// on one root only rolls back that root's copies.
func ownerRoot(roots []string, itemPath string) string {
	best := ""
	for _, root := range roots {
		if itemPath == root || strings.HasPrefix(itemPath, strings.TrimSuffix(root, "/")+"/") {
			if len(root) > len(best) {
				best = root
			}
		}
	}
	return best
}

func (a *App) addPlanFile(plan *DeletePlan, fullPath, name string, size int64) {
	level, why := classifyPath(fullPath, false)
	capturable := size <= safestore.MaxSingleCapture

	plan.Items = append(plan.Items, DeleteItem{
		Path: fullPath, Name: name, Size: size, IsDir: false,
		Level: level, Reason: why, Capturable: capturable,
	})
	plan.FileCount++
	plan.TotalBytes += size
	if capturable {
		plan.CaptureBytes += size
	}
	if level == ProtectCritical {
		plan.Critical = append(plan.Critical, fullPath+" — "+why)
	}
}

func (a *App) walkForDelete(c *pterodactyl.Client, dir string, depth int, plan *DeletePlan) error {
	if depth > maxPlanDepth {
		plan.Truncated = true
		return nil
	}
	if len(plan.Items) >= maxPlanEntries {
		plan.Truncated = true
		return nil
	}

	files, err := c.ListFiles(dir)
	if err != nil {
		// A directory we cannot read is a directory we cannot back up.
		plan.Truncated = true
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("could not list %s: %v", dir, err))
		return nil
	}

	for i := range files {
		if len(plan.Items) >= maxPlanEntries {
			plan.Truncated = true
			return nil
		}

		f := files[i]
		full := joinRemote(dir, f.Name)
		isDir := !f.IsFile && !f.IsSymlink

		if isDir {
			plan.DirCount++
			level, why := classifyPath(full, true)
			plan.Items = append(plan.Items, DeleteItem{
				Path: full, Name: f.Name, IsDir: true, Level: level, Reason: why,
			})
			if level == ProtectCritical {
				plan.Critical = append(plan.Critical, full+" — "+why)
			}
			if err := a.walkForDelete(c, full, depth+1, plan); err != nil {
				return err
			}
			continue
		}

		if f.IsSymlink {
			// Following a symlink out of the tree would back up, and report,
			// files the delete is not going to touch.
			plan.Items = append(plan.Items, DeleteItem{
				Path: full, Name: f.Name, IsDir: false,
				Level: ProtectSensitive, Reason: "a symlink; its target is not captured",
			})
			plan.FileCount++
			continue
		}

		a.addPlanFile(plan, full, f.Name, f.Size)
	}
	return nil
}

// DeleteOutcome reports what actually happened.
type DeleteOutcome struct {
	Deleted  []string `json:"deleted"`
	Captured int      `json:"captured"`
	Skipped  []string `json:"skipped"`
	Failed   []string `json:"failed"`
	Batch    string   `json:"batch"`
	BinUsed  int64    `json:"bin_used"`
	BinLimit int64    `json:"bin_limit"`
}

// SafeDeleteFiles executes a plan. token must be the one PlanDelete returned
// for this exact selection: it is a hash of the server and the sorted paths,
// so a plan cannot be replayed against a different set of files.
func (a *App) SafeDeleteFiles(token string) (*DeleteOutcome, error) {
	if token == "" {
		return nil, errors.New("a delete plan is required; call PlanDelete first")
	}

	a.planMu.Lock()
	plan, ok := a.deletePlans[token]
	if ok {
		delete(a.deletePlans, token)
	}
	a.planMu.Unlock()

	if !ok {
		return nil, errors.New("that delete plan is unknown or has expired — review the selection again")
	}
	if time.Since(plan.createdAt) > planTTL {
		return nil, errors.New("that delete plan has expired — review the selection again")
	}
	if plan.Token != planToken(plan.ServerID, plan.Roots) {
		return nil, errors.New("delete plan integrity check failed")
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	outcome := &DeleteOutcome{
		Deleted: []string{},
		Skipped: []string{},
		Failed:  []string{},
	}

	// One id for the whole operation, so a folder that deleted into 200 bin
	// entries can be put back as one action rather than 200.
	batch := plan.Token[:12]
	outcome.Batch = batch

	err := a.withServerClient(plan.ServerID, func(c *pterodactyl.Client, panelName string) error {
		serverID := plan.ServerID

		// The plan is a snapshot. Confirm every root is still there, and still
		// the same kind of thing, before touching anything — a file that was
		// replaced by a directory (or by a different file of the same name)
		// since the dialog opened is not what the user agreed to delete.
		//
		// Item kinds are looked up once rather than scanned per root.
		wasDirOf := map[string]bool{}
		for _, item := range plan.Items {
			wasDirOf[item.Path] = item.IsDir
		}

		for _, root := range plan.Roots {
			dir, name, err := splitRemote(root)
			if err != nil {
				return err
			}

			siblings, err := c.ListFiles(dir)
			if err != nil {
				return fmt.Errorf("cannot re-check %s before deleting: %w", dir, err)
			}

			var found *pterodactyl.FileInfo
			for i := range siblings {
				if siblings[i].Name == name {
					found = &siblings[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("%s is no longer there — nothing was deleted; refresh and try again", root)
			}

			if isDir := !found.IsFile && !found.IsSymlink; isDir != wasDirOf[root] {
				return fmt.Errorf("%s changed between the preview and now — nothing was deleted; refresh and try again", root)
			}
		}

		// Capture second, tracked per root. A root whose delete later fails
		// gets its copies rolled back; a root that succeeded keeps them,
		// because those files really are gone. Rolling back everything on any
		// failure used to erase the only copies of files already deleted.
		capturedByRoot := map[string][]string{}
		rollback := func(root string) {
			for _, id := range capturedByRoot[root] {
				_ = a.store.Remove(safestore.KindBin, id)
			}
			delete(capturedByRoot, root)
		}

		record := func(root, id string) {
			capturedByRoot[root] = append(capturedByRoot[root], id)
		}

		for _, item := range plan.Items {
			if item.IsDir {
				continue
			}
			root := ownerRoot(plan.Roots, item.Path)

			meta := safestore.Entry{
				Panel:  panelName,
				Server: serverID,
				Path:   item.Path,
				Reason: "deleted",
				Batch:  batch,
			}

			if !item.Capturable {
				meta.Note = fmt.Sprintf("%d bytes, over the capture limit", item.Size)
				entry, err := a.store.Put(safestore.KindBin, meta, nil)
				if err != nil {
					return err
				}
				record(root, entry.ID)
				outcome.Skipped = append(outcome.Skipped, item.Path)
				continue
			}

			// The download endpoint, not the editor one: /files/contents is
			// capped by the panel and refuses anything sizeable.
			content, err := c.DownloadFileBytes(item.Path, safestore.MaxSingleCapture)
			if err != nil {
				meta.Note = "could not be read before deletion: " + err.Error()
				entry, putErr := a.store.Put(safestore.KindBin, meta, nil)
				if putErr != nil {
					return putErr
				}
				record(root, entry.ID)
				outcome.Skipped = append(outcome.Skipped, item.Path)
				continue
			}

			entry, err := a.store.Put(safestore.KindBin, meta, content)
			if err != nil {
				if errors.Is(err, safestore.ErrTooLarge) {
					meta.Note = err.Error()
					placeholder, putErr := a.store.Put(safestore.KindBin, meta, nil)
					if putErr != nil {
						return putErr
					}
					record(root, placeholder.ID)
					outcome.Skipped = append(outcome.Skipped, item.Path)
					continue
				}
				rollback(root)
				return fmt.Errorf("refusing to delete: the recycle bin copy of %s failed: %w", item.Path, err)
			}
			record(root, entry.ID)
			outcome.Captured++
		}

		// Delete last, one root at a time so a failure is isolated to the root
		// that caused it. Grouping by directory would put two roots in one
		// request and make a partial failure impossible to attribute.
		for _, root := range plan.Roots {
			dir, name, splitErr := splitRemote(root)
			if splitErr != nil {
				outcome.Failed = append(outcome.Failed, root+" — "+splitErr.Error())
				rollback(root)
				continue
			}
			if err := c.DeleteFiles(dir, []string{name}); err != nil {
				// This root is still there, so its copies would be bin entries
				// for files that exist. The other roots keep theirs.
				rollback(root)
				outcome.Failed = append(outcome.Failed, root+" — "+err.Error())
				continue
			}
			outcome.Deleted = append(outcome.Deleted, root)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	outcome.BinUsed = a.store.Usage(safestore.KindBin)
	outcome.BinLimit = a.store.Limit(safestore.KindBin)

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "files-deleted", outcome.Deleted)
	}
	return outcome, nil
}

func planToken(serverID string, roots []string) string {
	h := sha256.New()
	h.Write([]byte(serverID))
	for _, r := range roots {
		h.Write([]byte{0})
		h.Write([]byte(r))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (a *App) prunePlansLocked() {
	for token, plan := range a.deletePlans {
		if time.Since(plan.createdAt) > planTTL {
			delete(a.deletePlans, token)
		}
	}
}

/* ------------------------------------------------------- store bindings */

// StoreStats is the header of the Vault tab.
type StoreStats struct {
	Root         string `json:"root"`
	BinUsed      int64  `json:"bin_used"`
	BinLimit     int64  `json:"bin_limit"`
	BinCount     int    `json:"bin_count"`
	VersionUsed  int64  `json:"version_used"`
	VersionLimit int64  `json:"version_limit"`
	VersionCount int    `json:"version_count"`
}

// GetStoreStats reports what the local store is holding.
func (a *App) GetStoreStats() (*StoreStats, error) {
	if a.store == nil {
		return nil, errors.New("local store unavailable")
	}
	return &StoreStats{
		Root:         a.store.Root(),
		BinUsed:      a.store.Usage(safestore.KindBin),
		BinLimit:     a.store.Limit(safestore.KindBin),
		BinCount:     len(a.store.List(safestore.KindBin)),
		VersionUsed:  a.store.Usage(safestore.KindVersion),
		VersionLimit: a.store.Limit(safestore.KindVersion),
		VersionCount: len(a.store.List(safestore.KindVersion)),
	}, nil
}

// ListRecycleBin returns the bin, newest first.
func (a *App) ListRecycleBin() ([]safestore.Entry, error) {
	if a.store == nil {
		return nil, errors.New("local store unavailable")
	}
	return a.store.List(safestore.KindBin), nil
}

// ListFileVersions returns the saved states of one file, newest first — the
// history behind the Vault tab's version list. An empty path returns every
// version the store holds, across every file.
func (a *App) ListFileVersions(serverID, remotePath string) ([]safestore.Entry, error) {
	if a.store == nil {
		return nil, errors.New("local store unavailable")
	}
	if serverID == "" {
		serverID = a.activeServerID()
	}
	if remotePath == "" {
		return a.store.List(safestore.KindVersion), nil
	}
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	return a.store.ListFor(safestore.KindVersion, serverID, cleaned), nil
}

// ReadStoredCopy returns the text of a stored copy so the UI can preview or
// diff it. kind is "version" or "bin".
func (a *App) ReadStoredCopy(kind, id string) (string, error) {
	if a.store == nil {
		return "", errors.New("local store unavailable")
	}
	data, err := a.store.Read(safestore.Kind(kind), id)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("this copy is binary; restore it instead of previewing it")
	}
	return string(data), nil
}

// RestoreStoredCopy writes a stored copy back to the panel.
//
// It refuses when something already occupies the path unless overwrite is set,
// and an overwrite still takes a fresh backup of what it replaces.
func (a *App) RestoreStoredCopy(kind, id string, overwrite bool) error {
	if a.store == nil {
		return errors.New("local store unavailable")
	}

	entry, err := a.store.Get(safestore.Kind(kind), id)
	if err != nil {
		return err
	}
	if !entry.Captured {
		return fmt.Errorf("this entry holds no content: %s", entry.Note)
	}

	data, err := a.store.Read(safestore.Kind(kind), id)
	if err != nil {
		return err
	}

	// Validated here so a malformed stored path fails before any client work.
	if _, _, err := splitRemote(entry.Path); err != nil {
		return err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient(entry.Server, func(c *pterodactyl.Client, panel string) error {
		return a.restoreOne(c, panel, entry, data, overwrite)
	})
}

// restoreOne writes one stored copy back. The caller holds fileMu and supplies
// a client already pointed at the entry's server.
func (a *App) restoreOne(c *pterodactyl.Client, panel string, entry safestore.Entry, data []byte, overwrite bool) error {
	dir, name, err := splitRemote(entry.Path)
	if err != nil {
		return err
	}

	files, listErr := c.ListFiles(dir)
	if listErr != nil {
		// Only a genuine "no such directory" means the folder went with the
		// file. Any other failure — a timeout, a 500, a permission problem —
		// leaves us unable to tell whether something is already there, and
		// carrying on with an empty listing would skip the overwrite guard and
		// write straight over it.
		if dir == "/" || !isNotFound(listErr) {
			return fmt.Errorf("cannot list %s before restoring: %w", dir, listErr)
		}
		if mkErr := c.CreateDirectory("/", strings.TrimPrefix(dir, "/")); mkErr != nil {
			return fmt.Errorf("cannot recreate %s: %v (%w)", dir, mkErr, listErr)
		}
		files = nil
	}

	var existing *pterodactyl.FileInfo
	for i := range files {
		if files[i].Name == name {
			existing = &files[i]
			break
		}
	}

	if existing != nil {
		if !overwrite {
			return fmt.Errorf("%s already exists — restore with overwrite to replace it", entry.Path)
		}
		if !existing.IsFile {
			return fmt.Errorf("%s is a directory; refusing to replace it with a file", entry.Path)
		}
		// What the restore is about to displace goes to the bin, not to the
		// history: it is being taken away wholesale, not edited.
		if _, captureErr := a.captureRemote(safestore.KindBin, c, panel, entry.Server,
			entry.Path, "replaced by a restore", existing.Size); captureErr != nil {
			return fmt.Errorf("refusing to restore: keeping a copy of the current file failed: %w", captureErr)
		}
	}

	return c.SaveFileContent(entry.Path, string(data))
}

// BatchRestore reports what a whole-operation restore managed to put back.
type BatchRestore struct {
	Restored []string `json:"restored"`
	Skipped  []string `json:"skipped"`
	Failed   []string `json:"failed"`
}

// RestoreBinBatch puts back everything one delete removed.
//
// Entries are replayed oldest first, which is also the order the directories
// were walked in, so a nested tree comes back from the top down. One file that
// cannot be restored does not abort the rest: the outcome names it instead.
func (a *App) RestoreBinBatch(batch string, overwrite bool) (*BatchRestore, error) {
	if a.store == nil {
		return nil, errors.New("local store unavailable")
	}
	entries := a.store.ListBatch(safestore.KindBin, batch)
	if len(entries) == 0 {
		return nil, errors.New("nothing left in the bin from that delete")
	}

	out := &BatchRestore{Restored: []string{}, Skipped: []string{}, Failed: []string{}}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	// A delete only ever touches one server, but group anyway rather than
	// assume it: a wrong assumption here writes files onto the wrong machine.
	byServer := map[string][]safestore.Entry{}
	order := []string{}
	for _, e := range entries {
		if _, seen := byServer[e.Server]; !seen {
			order = append(order, e.Server)
		}
		byServer[e.Server] = append(byServer[e.Server], e)
	}

	for _, server := range order {
		group := byServer[server]
		err := a.withServerClient(server, func(c *pterodactyl.Client, panel string) error {
			for _, entry := range group {
				if !entry.Captured {
					out.Skipped = append(out.Skipped, entry.Path)
					continue
				}
				data, readErr := a.store.Read(safestore.KindBin, entry.ID)
				if readErr != nil {
					out.Failed = append(out.Failed, entry.Path+" — "+readErr.Error())
					continue
				}
				if err := a.restoreOne(c, panel, entry, data, overwrite); err != nil {
					out.Failed = append(out.Failed, entry.Path+" — "+err.Error())
					continue
				}
				out.Restored = append(out.Restored, entry.Path)
			}
			return nil
		})
		if err != nil {
			out.Failed = append(out.Failed, "server "+server+" — "+err.Error())
		}
	}

	return out, nil
}

// ForgetStoredCopy removes one entry from the local store. This is the only
// operation here that destroys data, and it destroys only the local copy.
func (a *App) ForgetStoredCopy(kind, id string) error {
	if a.store == nil {
		return errors.New("local store unavailable")
	}
	return a.store.Remove(safestore.Kind(kind), id)
}

// EmptyRecycleBin discards every bin entry.
func (a *App) EmptyRecycleBin() error {
	if a.store == nil {
		return errors.New("local store unavailable")
	}
	return a.store.Empty(safestore.KindBin)
}

// SetRecycleBinLimitMB changes the bin's cap. The default is 100 MB.
func (a *App) SetRecycleBinLimitMB(mb int64) error {
	if a.store == nil {
		return errors.New("local store unavailable")
	}
	if mb < 1 || mb > 4096 {
		return errors.New("the recycle bin limit must be between 1 and 4096 MB")
	}
	a.store.SetLimit(safestore.KindBin, mb<<20)
	return nil
}

/* ------------------------------------------------------------- helpers */

// isNotFound recognises the panel's "no such file" response. The client
// surfaces the HTTP status in the error text.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 404") || strings.Contains(msg, "notfoundhttpexception")
}

// serverIDOr falls back to the client's own server ID when the caller passed
// an empty one, so store entries are never filed under "".
func serverIDOr(serverID string, c *pterodactyl.Client) string {
	if serverID != "" {
		return serverID
	}
	return c.GetServerID()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

/* --------------------------------------------------------- downloading */

// GetFileDownloadURL returns a signed, short-lived URL for one file, which the
// frontend hands to the system browser. Read-only: nothing on the panel
// changes, so there is no backup to take.
func (a *App) GetFileDownloadURL(remotePath string) (string, error) {
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return "", err
	}
	if _, _, err := splitRemote(cleaned); err != nil {
		return "", err
	}

	var url string
	err = a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		got, urlErr := c.GetDownloadURL(cleaned)
		if urlErr != nil {
			return urlErr
		}
		url = got
		return nil
	})
	return url, err
}

/* ------------------------------------------------------------ archives */

// ArchiveResult describes the archive a compress produced.
type ArchiveResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// archiveExtensions are stripped to name the folder an archive extracts into.
// Longest first, so ".tar.gz" wins over ".gz".
var archiveExtensions = []string{
	".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst",
	".tgz", ".tbz2", ".txz", ".zip", ".tar", ".gz", ".rar", ".7z",
}

// CompressFiles archives a selection into a new tar.gz beside it.
//
// Every path has to sit in the same directory, which is what the file tree
// hands over anyway: the panel's compress route takes one root and a list of
// names within it.
func (a *App) CompressFiles(paths []string) (*ArchiveResult, error) {
	if len(paths) == 0 {
		return nil, errors.New("nothing selected")
	}

	var root string
	names := make([]string, 0, len(paths))
	seen := map[string]bool{}

	for _, p := range paths {
		dir, name, err := splitRemote(p)
		if err != nil {
			return nil, err
		}
		if root == "" {
			root = dir
		} else if dir != root {
			return nil, fmt.Errorf("everything being archived has to be in one folder; %s is not in %s", p, root)
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	out := &ArchiveResult{}
	err := a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		attrs, compressErr := c.CompressFiles(root, names)
		if compressErr != nil {
			return compressErr
		}
		if name, ok := attrs["name"].(string); ok {
			out.Name = name
			out.Path = joinRemote(root, name)
		}
		if size, ok := attrs["size"].(float64); ok {
			out.Size = int64(size)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out.Name == "" {
		return nil, errors.New("the panel did not say what it named the archive")
	}
	return out, nil
}

// DecompressFile extracts an archive and returns the folder it landed in.
//
// intoNewFolder is the safe form and the one the UI defaults to: the archive
// is moved into a fresh folder named after it, extracted there, and moved back
// out. Extracting in place is the only file operation here that can replace a
// file without the local store getting a copy first — the archive decides what
// it writes, and there is no way to know before it runs — so the caller has to
// ask for it explicitly.
func (a *App) DecompressFile(remotePath string, intoNewFolder bool) (string, error) {
	dir, name, err := splitRemote(remotePath)
	if err != nil {
		return "", err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	target := dir

	err = a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		if !intoNewFolder {
			return c.DecompressFile(dir, name)
		}

		existing, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot list %s: %w", dir, listErr)
		}
		taken := map[string]bool{}
		for _, f := range existing {
			taken[strings.ToLower(f.Name)] = true
		}

		folder := uniqueFolderName(archiveBaseName(name), taken)
		if createErr := c.CreateDirectory(dir, folder); createErr != nil {
			return fmt.Errorf("cannot create %s: %w", joinRemote(dir, folder), createErr)
		}
		target = joinRemote(dir, folder)

		// The panel's decompress route only reads an archive inside the root
		// it extracts into, so the archive goes in and comes back out.
		inner := folder + "/" + name
		if moveErr := c.RenameFile(dir, name, inner); moveErr != nil {
			return fmt.Errorf("cannot move the archive into %s: %w", target, moveErr)
		}

		decompressErr := c.DecompressFile(target, name)

		// Put it back either way, so a failed extract does not leave the
		// archive buried in a folder the user did not ask for.
		if moveBackErr := c.RenameFile(dir, inner, name); moveBackErr != nil && decompressErr == nil {
			return fmt.Errorf("extracted into %s, but the archive could not be moved back: %w", target, moveBackErr)
		}
		return decompressErr
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

// archiveBaseName strips one archive extension from a filename.
func archiveBaseName(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

// uniqueFolderName appends a counter until the name is free.
func uniqueFolderName(base string, taken map[string]bool) string {
	if base == "" {
		base = "extracted"
	}
	if !taken[strings.ToLower(base)] {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return base + "-extracted"
}

/* --------------------------------------------------------- console logs */

// commonLogPaths are where the eggs this app gets pointed at keep their log.
// The websocket only replays what wings has buffered — a few dozen lines — so
// anything older has to come from the file itself.
var commonLogPaths = []string{
	"/logs/latest.log",
	"/logs/console.log",
	"/latest.log",
	"/server.log",
	"/console.log",
	"/proxy.log.0",
	"/logs/server.log",
}

// LogTail is the end of a server's log file.
type LogTail struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated"`
	Found     bool   `json:"found"`
}

// GetServerLogTail returns the last maxLines of whichever log file the server
// keeps, so the console can show history the websocket never sends.
func (a *App) GetServerLogTail(maxLines int) (*LogTail, error) {
	if maxLines <= 0 || maxLines > 5000 {
		maxLines = 1000
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	out := &LogTail{}

	err := a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		for _, candidate := range commonLogPaths {
			dir, name, splitErr := splitRemote(candidate)
			if splitErr != nil {
				continue
			}

			files, listErr := c.ListFiles(dir)
			if listErr != nil {
				continue
			}

			var size int64 = -1
			for i := range files {
				if files[i].Name == name && files[i].IsFile {
					size = files[i].Size
					break
				}
			}
			if size < 0 {
				continue
			}
			if size > EditorMaxOpenBytes {
				// A log this big would be most of a save's worth of bandwidth
				// for lines nobody scrolls back to.
				out.Path = candidate
				out.Found = true
				out.Truncated = true
				out.Content = fmt.Sprintf("(%s is %s — too large to load here; open it from the Files tab)",
					candidate, humanBytes(size))
				return nil
			}

			content, readErr := c.GetFileContent(candidate)
			if readErr != nil {
				continue
			}

			lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
			if len(lines) > maxLines {
				lines = lines[len(lines)-maxLines:]
				out.Truncated = true
			}

			out.Path = candidate
			out.Found = true
			out.Lines = len(lines)
			out.Content = strings.Join(lines, "\n")
			return nil
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return out, nil
}

/* ---------------------------------------------------- cross-server copy */

// CopyResult reports where a copied file landed.
type CopyResult struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Replaced bool   `json:"replaced"`
	Conflict bool   `json:"conflict"`
	Reason   string `json:"reason,omitempty"`
}

// CopyFileBetweenServers copies one file from any server to any other,
// including across panels.
//
// The bytes never cross the bridge to the frontend: a drag-and-drop of a jar or
// a world file would come back as mojibake if it went through JSON, so the read
// and the write both happen here.
//
// A file already at the destination is reported rather than replaced, unless
// overwrite says otherwise — and an overwrite files the displaced copy in the
// recycle bin first, the same as every other replacement in the app.
func (a *App) CopyFileBetweenServers(srcServer, srcPath, dstServer, dstDir string, overwrite bool) (*CopyResult, error) {
	cleanSrc, err := normalizeRemotePath(srcPath)
	if err != nil {
		return nil, err
	}
	_, name, err := splitRemote(cleanSrc)
	if err != nil {
		return nil, err
	}
	cleanDstDir, err := normalizeRemotePath(dstDir)
	if err != nil {
		return nil, err
	}

	dstPath := joinRemote(cleanDstDir, name)
	if srcServer == dstServer && cleanSrc == dstPath {
		return nil, errors.New("that file is already there")
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	var payload []byte
	var size int64

	// Read from the source.
	err = a.withServerClient(srcServer, func(c *pterodactyl.Client, _ string) error {
		dir, base, splitErr := splitRemote(cleanSrc)
		if splitErr != nil {
			return splitErr
		}
		files, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot list %s: %w", dir, listErr)
		}
		for i := range files {
			if files[i].Name != base {
				continue
			}
			if !files[i].IsFile {
				return fmt.Errorf("%s is a folder; only files can be dragged across", cleanSrc)
			}
			if files[i].Size > safestore.MaxSingleCapture {
				return fmt.Errorf("%s is %s, too large to copy through the app", cleanSrc, humanBytes(files[i].Size))
			}
			size = files[i].Size
			break
		}
		// Through the download endpoint: /files/contents is the editor's and
		// the panel caps it, so a jar or a zip could never be dragged across.
		content, readErr := c.DownloadFileBytes(cleanSrc, safestore.MaxSingleCapture)
		if readErr != nil {
			return readErr
		}
		payload = content
		if size == 0 {
			size = int64(len(content))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := &CopyResult{Path: dstPath, Size: size}

	err = a.withServerClient(dstServer, func(c *pterodactyl.Client, panel string) error {
		files, listErr := c.ListFiles(cleanDstDir)
		if listErr != nil {
			return fmt.Errorf("cannot list %s: %w", cleanDstDir, listErr)
		}

		for i := range files {
			if files[i].Name != name {
				continue
			}
			if !files[i].IsFile {
				return fmt.Errorf("%s is a folder on the destination; refusing to replace it with a file", dstPath)
			}
			if !overwrite {
				out.Conflict = true
				out.Reason = name + " is already in " + cleanDstDir
				return nil
			}
			if _, captureErr := a.captureRemote(safestore.KindBin, c, panel, serverIDOr(dstServer, c),
				dstPath, "replaced by a copy", files[i].Size); captureErr != nil {
				return fmt.Errorf("refusing to copy: keeping a copy of the file it would replace failed: %w", captureErr)
			}
			out.Replaced = true
			break
		}

		if out.Conflict {
			return nil
		}
		// UploadFile rather than SaveFileContent: the write endpoint is the
		// editor's counterpart and treats the body as text.
		dstDirOfFile, dstName, splitErr := splitRemote(dstPath)
		if splitErr != nil {
			return splitErr
		}
		return c.UploadFile(dstDirOfFile, dstName, bytes.NewReader(payload))
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

/* ------------------------------------------------- download to the disk */

// DownloadOutcome reports where files ended up on this machine.
type DownloadOutcome struct {
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
	Skipped   []string `json:"skipped"`
	Bytes     int64    `json:"bytes"`
	Cancelled bool     `json:"cancelled"`
}

// MaxDownloadBytes caps one downloaded file. The content is read into memory
// on the way through, so this is a memory ceiling rather than a policy.
const MaxDownloadBytes int64 = 256 << 20

// DownloadToDisk saves panel files onto this machine.
//
// One file comes down on its own. Anything else — several files, or a folder —
// is archived on the panel first and arrives as a single archive, which is both
// what people expect from a multi-select and the only way the client API can
// hand over a directory at all.
//
// The transfer goes through /files/download, not /files/contents: the latter is
// the file editor's endpoint and the panel caps it, which is why a zip used to
// come back as FileSizeTooLargeException.
func (a *App) DownloadToDisk(paths []string) (*DownloadOutcome, error) {
	if len(paths) == 0 {
		return nil, errors.New("nothing selected")
	}
	if a.ctx == nil {
		return nil, errors.New("no window to ask from")
	}

	cleaned := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		norm, err := normalizeRemotePath(p)
		if err != nil {
			return nil, err
		}
		if _, _, err := splitRemote(norm); err != nil {
			return nil, err
		}
		if !seen[norm] {
			seen[norm] = true
			cleaned = append(cleaned, norm)
		}
	}
	sort.Strings(cleaned)
	cleaned = collapseNested(cleaned)

	outcome := &DownloadOutcome{Files: []string{}, Skipped: []string{}}

	// Look at what was selected before asking where to put it, so the dialog is
	// not shown for something that cannot be downloaded at all.
	type entry struct {
		path  string
		name  string
		isDir bool
		size  int64
	}
	items := make([]entry, 0, len(cleaned))

	a.fileMu.Lock()
	err := a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		for _, p := range cleaned {
			dir, name, _ := splitRemote(p)
			files, listErr := c.ListFiles(dir)
			if listErr != nil {
				return fmt.Errorf("cannot list %s: %w", dir, listErr)
			}
			var found *pterodactyl.FileInfo
			for i := range files {
				if files[i].Name == name {
					found = &files[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("%s no longer exists — refresh the file list", p)
			}
			items = append(items, entry{
				path:  p,
				name:  name,
				isDir: !found.IsFile && !found.IsSymlink,
				size:  found.Size,
			})
		}
		return nil
	})
	a.fileMu.Unlock()
	if err != nil {
		return nil, err
	}

	single := len(items) == 1 && !items[0].isDir

	// Name the save dialog before doing any work, so cancelling costs nothing.
	suggested := "download.tar.gz"
	if single {
		suggested = items[0].name
	} else if len(items) == 1 {
		suggested = archiveBaseName(items[0].name) + ".tar.gz"
	} else if dir, _, dirErr := splitRemote(items[0].path); dirErr == nil {
		folder := path.Base(dir)
		if folder == "/" || folder == "." {
			folder = "files"
		}
		suggested = folder + ".tar.gz"
	}

	chosen, dlgErr := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save " + suggested,
		DefaultFilename: sanitizeLocalName(suggested),
	})
	if dlgErr != nil {
		return nil, dlgErr
	}
	if chosen == "" {
		outcome.Cancelled = true
		return outcome, nil
	}
	outcome.Directory = filepath.Dir(chosen)

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	err = a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		source := ""
		temporary := false

		if single {
			source = items[0].path
		} else {
			// One archive for the whole selection. CompressFiles takes a single
			// root and names within it, which is what a tree selection always
			// is, and collapseNested has already removed anything doubled up.
			root, _, splitErr := splitRemote(items[0].path)
			if splitErr != nil {
				return splitErr
			}
			names := make([]string, 0, len(items))
			for _, it := range items {
				dir, name, err := splitRemote(it.path)
				if err != nil {
					return err
				}
				if dir != root {
					return fmt.Errorf("everything downloaded together has to be in one folder; %s is not in %s", it.path, root)
				}
				names = append(names, name)
			}

			attrs, compressErr := c.CompressFiles(root, names)
			if compressErr != nil {
				return fmt.Errorf("could not archive the selection: %w", compressErr)
			}
			archive, _ := attrs["name"].(string)
			if archive == "" {
				return errors.New("the panel did not say what it named the archive")
			}
			source = joinRemote(root, archive)
			temporary = true
		}

		written, dlErr := c.DownloadFileTo(source, chosen)

		// The archive was this call's doing, so this call takes it back —
		// whether or not the download worked.
		if temporary {
			dir, name, splitErr := splitRemote(source)
			if splitErr == nil {
				if delErr := c.DeleteFiles(dir, []string{name}); delErr != nil {
					outcome.Skipped = append(outcome.Skipped,
						"left "+source+" on the server: "+delErr.Error())
				}
			}
		}

		if dlErr != nil {
			return dlErr
		}

		outcome.Files = append(outcome.Files, chosen)
		outcome.Bytes = written
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}

// sanitizeLocalName keeps a panel filename usable on a Windows filesystem.
func sanitizeLocalName(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f:
			return '_'
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		}
		return r
	}, name)
	out = strings.Trim(out, " .")
	if out == "" {
		out = "download"
	}
	return out
}

/* ---------------------------------------------------- console window */

// WindowMode tells the frontend which of the two shapes this process is.
// "console" hides everything that is not the console; "main" is the whole app.
func (a *App) WindowMode() map[string]interface{} {
	return map[string]interface{}{
		"mode":     map[bool]string{true: "console", false: "main"}[a.consoleOnly],
		"serverID": a.consoleServerID,
		"panel":    a.consolePanelName,
	}
}

// OpenConsoleWindow launches a second copy of this binary as a console-only
// window for one server.
//
// Wails v2 runs a single window per process, so a console that can be dragged
// to another monitor and resized past the main window's bounds has to be its
// own process. It reads the same config and opens its own websocket, which the
// panel is happy to serve alongside the first.
func (a *App) OpenConsoleWindow(serverID string) error {
	if a.consoleOnly {
		return errors.New("this is already a console window")
	}

	if serverID == "" {
		serverID = a.activeServerID()
	}
	if serverID == "" {
		return errors.New("no server selected")
	}

	panelName := a.serverPanelMap[serverID]
	if panelName == "" {
		panelName = a.config.GetActivePanelName()
	}

	label := serverID
	for _, s := range a.lastKnownServers() {
		if s.ID == serverID && s.Name != "" {
			label = s.Name
			break
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find this application on disk: %w", err)
	}

	cmd := exec.Command(exe, "--console", serverID, "--panel", panelName, "--label", label)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open the console window: %w", err)
	}

	// Nothing waits on it: it is a sibling window, not a subtask, and it
	// outlives whatever the main window does next.
	go func() { _ = cmd.Wait() }()

	if a.ctx != nil {
		runtime.LogInfof(a.ctx, "opened a console window for %s (%s)", label, serverID)
	}
	return nil
}

// lastKnownServers reads the active panel's server list for a display name,
// falling back to nothing rather than failing the launch over a label.
func (a *App) lastKnownServers() []pterodactyl.ServerInfo {
	if a.client == nil {
		return nil
	}
	servers, err := a.client.ListServers()
	if err != nil {
		return nil
	}
	return servers
}

/* ------------------------------------------------------------- searching */

// SearchHit is one file or folder a recursive search turned up.
type SearchHit struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// SearchOutcome is what a walk found, and what it had to leave out.
type SearchOutcome struct {
	Query     string      `json:"query"`
	Root      string      `json:"root"`
	Hits      []SearchHit `json:"hits"`
	Scanned   int         `json:"scanned"`
	Folders   int         `json:"folders"`
	Truncated bool        `json:"truncated"`
	Reason    string      `json:"reason,omitempty"`
}

// Bounds for a recursive search. Each folder is one API round trip, so a deep
// tree is a lot of them; these keep a stray search from becoming a thousand
// requests to somebody's panel.
const (
	searchMaxFolders = 600
	searchMaxHits    = 400
	searchMaxDepth   = 12
	searchDeadline   = 20 * time.Second
)

// SearchFiles walks the tree under root looking for names containing query.
//
// The panel has no search endpoint, so this is a breadth-first walk of the
// listing API — one request per folder. Breadth-first rather than depth-first
// because the thing being looked for is usually near the top, and hitting the
// folder ceiling then means the shallow half was covered rather than one deep
// branch.
func (a *App) SearchFiles(root, query string) (*SearchOutcome, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, errors.New("nothing to search for")
	}

	cleanRoot, err := normalizeRemotePath(root)
	if err != nil {
		return nil, err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	out := &SearchOutcome{Query: query, Root: cleanRoot, Hits: []SearchHit{}}
	started := time.Now()

	err = a.withServerClient("", func(c *pterodactyl.Client, _ string) error {
		type todo struct {
			path  string
			depth int
		}
		queue := []todo{{path: cleanRoot, depth: 0}}

		for len(queue) > 0 {
			if out.Folders >= searchMaxFolders {
				out.Truncated = true
				out.Reason = fmt.Sprintf("stopped after %d folders", searchMaxFolders)
				break
			}
			if len(out.Hits) >= searchMaxHits {
				out.Truncated = true
				out.Reason = fmt.Sprintf("stopped at %d matches", searchMaxHits)
				break
			}
			if time.Since(started) > searchDeadline {
				out.Truncated = true
				out.Reason = "stopped after " + searchDeadline.String()
				break
			}

			current := queue[0]
			queue = queue[1:]

			files, listErr := c.ListFiles(current.path)
			if listErr != nil {
				// A folder that cannot be read is skipped rather than failing
				// the whole search; permissions vary inside a container.
				continue
			}
			out.Folders++

			for i := range files {
				f := files[i]
				out.Scanned++

				full := joinRemote(current.path, f.Name)
				isDir := !f.IsFile && !f.IsSymlink

				if strings.Contains(strings.ToLower(f.Name), needle) && len(out.Hits) < searchMaxHits {
					out.Hits = append(out.Hits, SearchHit{
						Path:  full,
						Name:  f.Name,
						Dir:   current.path,
						Size:  f.Size,
						IsDir: isDir,
					})
				}

				// Symlinks are not followed: a link pointing up the tree turns
				// the walk into a loop.
				if isDir && current.depth < searchMaxDepth {
					queue = append(queue, todo{path: full, depth: current.depth + 1})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

/* -------------------------------------------------------------- uploads */

// UploadItem is one file in a batch. Content is base64.
//
// Not a []byte: Wails marshals call arguments as JSON, and a byte slice
// arrives from JavaScript as an array of numbers — roughly four characters of
// JSON per byte, plus an allocation each. Base64 is one string at 1.33x, which
// is where most of an upload's wall time was going.
type UploadItem struct {
	RelPath string `json:"rel_path"`
	Base64  string `json:"base64"`
}

// UploadOutcome reports a batch, item by item.
type UploadOutcome struct {
	Uploaded  []string `json:"uploaded"`
	Replaced  []string `json:"replaced"`
	Conflicts []string `json:"conflicts"`
	Failed    []string `json:"failed"`
	Bytes     int64    `json:"bytes"`
}

// UploadBatch writes several files under baseDir in one pass.
//
// One call rather than one per file, because each one used to take the file
// lock, list its target directory and make its own round trip — fifty files
// into one folder meant fifty identical listings. Directories are created
// once, each target directory is listed once, and the lock is held once.
//
// keepCopy controls whether a file being replaced is copied to the recycle bin
// first. That copy is a full download of the old file, so turning it off is
// the difference between one request per replacement and two — at the cost of
// the replacement being unrecoverable.
func (a *App) UploadBatch(baseDir string, items []UploadItem, overwrite, keepCopy bool) (*UploadOutcome, error) {
	if len(items) == 0 {
		return nil, errors.New("nothing to upload")
	}

	base, err := normalizeRemotePath(baseDir)
	if err != nil {
		return nil, err
	}

	type prepared struct {
		dir     string
		name    string
		path    string
		content []byte
	}

	ready := make([]prepared, 0, len(items))
	out := &UploadOutcome{
		Uploaded: []string{}, Replaced: []string{}, Conflicts: []string{}, Failed: []string{},
	}

	// Decode before touching the network, so a malformed item fails fast and
	// on its own rather than halfway through a batch.
	for _, item := range items {
		rel := strings.TrimPrefix(strings.ReplaceAll(item.RelPath, "\\", "/"), "/")
		if rel == "" {
			out.Failed = append(out.Failed, item.RelPath+" — no name")
			continue
		}

		full, pathErr := normalizeRemotePath(joinRemote(base, rel))
		if pathErr != nil {
			out.Failed = append(out.Failed, item.RelPath+" — "+pathErr.Error())
			continue
		}
		dir, name, splitErr := splitRemote(full)
		if splitErr != nil {
			out.Failed = append(out.Failed, item.RelPath+" — "+splitErr.Error())
			continue
		}

		content, decodeErr := base64.StdEncoding.DecodeString(item.Base64)
		if decodeErr != nil {
			out.Failed = append(out.Failed, item.RelPath+" — could not be decoded")
			continue
		}

		ready = append(ready, prepared{dir: dir, name: name, path: full, content: content})
	}

	if len(ready) == 0 {
		return out, nil
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	err = a.withServerClient("", func(c *pterodactyl.Client, panel string) error {
		// Every directory the batch needs, shallowest first so each one has a
		// parent by the time it is made. Already-exists is the normal case.
		dirs := map[string]bool{}
		for _, item := range ready {
			dirs[item.dir] = true
		}
		ordered := make([]string, 0, len(dirs))
		for dir := range dirs {
			ordered = append(ordered, dir)
		}
		sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) < len(ordered[j]) })

		for _, dir := range ordered {
			if dir == "/" || dir == base {
				continue
			}
			parent, name, splitErr := splitRemote(dir)
			if splitErr != nil {
				continue
			}
			_ = c.CreateDirectory(parent, name)
		}

		// One listing per directory, reused for every file going into it.
		existing := map[string]map[string]pterodactyl.FileInfo{}
		listOf := func(dir string) map[string]pterodactyl.FileInfo {
			if cached, ok := existing[dir]; ok {
				return cached
			}
			byName := map[string]pterodactyl.FileInfo{}
			if files, listErr := c.ListFiles(dir); listErr == nil {
				for i := range files {
					byName[files[i].Name] = files[i]
				}
			}
			existing[dir] = byName
			return byName
		}

		for _, item := range ready {
			here := listOf(item.dir)

			if found, clash := here[item.name]; clash {
				if !overwrite {
					out.Conflicts = append(out.Conflicts, item.path)
					continue
				}
				if !found.IsFile {
					out.Failed = append(out.Failed, item.path+" — a folder is already there")
					continue
				}
				if keepCopy {
					if _, captureErr := a.captureRemote(safestore.KindBin, c, panel, c.GetServerID(),
						item.path, "replaced by an upload", found.Size); captureErr != nil {
						out.Failed = append(out.Failed, item.path+" — keeping a copy failed: "+captureErr.Error())
						continue
					}
				}
				out.Replaced = append(out.Replaced, item.path)
			} else {
				out.Uploaded = append(out.Uploaded, item.path)
			}

			if uploadErr := c.UploadFile(item.dir, item.name, bytes.NewReader(item.content)); uploadErr != nil {
				// Take it back out of whichever list it was optimistically added to.
				out.Uploaded = withoutLast(out.Uploaded, item.path)
				out.Replaced = withoutLast(out.Replaced, item.path)
				out.Failed = append(out.Failed, item.path+" — "+uploadErr.Error())
				continue
			}

			out.Bytes += int64(len(item.content))
			// The directory now holds it, so a second file with the same name
			// later in the batch is seen as the clash it is.
			here[item.name] = pterodactyl.FileInfo{Name: item.name, IsFile: true, Size: int64(len(item.content))}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// withoutLast removes the last occurrence of value, for undoing an optimistic append.
func withoutLast(list []string, value string) []string {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i] == value {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}
