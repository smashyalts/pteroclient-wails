package main

// SFTP transfers.
//
// The client API is one HTTP request per file with a size cap on both ends, so
// a plugins folder is hundreds of round trips and a world folder is not
// possible at all. SFTP is one connection carrying several transfers at once,
// which is what makes a folder upload finish in seconds rather than minutes.
//
// It does not replace the API. Browsing, editing and deleting still go through
// the safety layer in filesafe.go; this is only the fast path for moving whole
// files in bulk, and it obeys the same rules — an upload that would replace
// something still takes a copy first, and nothing is overwritten without being
// asked.
//
// Credentials are the one thing this needs that the rest of the app does not.
// Pterodactyl authenticates SFTP against the panel account's own password, not
// the API key, so there is nothing to reuse: the password is asked for, held in
// memory for as long as the connection lasts, and never written to disk.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/pterodactyl"
	"pteroclient-wails/pkg/safestore"
	"pteroclient-wails/pkg/sftpx"
)

// Bounds. A folder transfer is bulk work, but it is still somebody's server on
// the other end and somebody's disk on this one.
const (
	sftpMaxEntries  = 20000
	sftpIdleTimeout = 15 * time.Minute
)

// SFTPStatus is what the transfer panel shows about the connection.
type SFTPStatus struct {
	Connected bool   `json:"connected"`
	Host      string `json:"host"`
	User      string `json:"user"`
	ServerID  string `json:"server_id"`
	Streams   int    `json:"streams"`
	// Fingerprint is the host key in use, so it can be shown rather than only
	// checked.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// SFTPPrompt is what the app needs from the user before it can connect.
type SFTPPrompt struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	ServerID string `json:"server_id"`
	// Known is true when this host has been connected to before, so the UI can
	// skip the fingerprint step.
	Known bool `json:"known"`
}

// sftpLink is the live connection and what it belongs to.
type sftpLink struct {
	session     *sftpx.Session
	serverID    string
	fingerprint string
	lastUsed    time.Time
}

// GetSFTPPrompt collects everything the connect dialog needs to fill itself in.
func (a *App) GetSFTPPrompt(serverID string) (*SFTPPrompt, error) {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	out := &SFTPPrompt{}
	err := a.withServerClient(serverID, func(c *pterodactyl.Client, _ string) error {
		details, detailErr := c.GetSFTPDetails()
		if detailErr != nil {
			return detailErr
		}
		out.Host = details.Host
		out.Port = details.Port
		out.Username = details.Username
		out.ServerID = details.ServerID
		return nil
	})
	if err != nil {
		return nil, err
	}

	if a.sftpHosts != nil {
		key := fmt.Sprintf("%s:%d", out.Host, out.Port)
		_, out.Known = a.sftpHosts.Fingerprints()[key]
	}
	return out, nil
}

// SFTPConnect opens the connection.
//
// The first attempt against an unseen host fails on purpose, carrying the
// fingerprint: the UI shows it, and calls again with acceptFingerprint set once
// the user has agreed to it. Trusting silently would make this interceptable by
// anyone between here and the node, which for a file transfer tool is the whole
// ball game.
func (a *App) SFTPConnect(serverID, username, password, acceptFingerprint string, streams int) (*SFTPStatus, error) {
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("the SFTP username is required")
	}
	if password == "" {
		return nil, errors.New("the SFTP password is required — it is your panel account's password")
	}

	prompt, err := a.GetSFTPPrompt(serverID)
	if err != nil {
		return nil, err
	}

	// Whatever was open belongs to a different request now.
	a.SFTPDisconnect()

	session, err := sftpx.Dial(sftpx.Config{
		Host:              prompt.Host,
		Port:              prompt.Port,
		User:              username,
		Password:          password,
		Streams:           streams,
		AcceptFingerprint: acceptFingerprint,
	}, a.sftpHosts)
	if err != nil {
		return nil, err
	}

	fingerprint := ""
	if a.sftpHosts != nil {
		fingerprint = a.sftpHosts.Fingerprints()[fmt.Sprintf("%s:%d", prompt.Host, prompt.Port)]
	}

	a.sftpMu.Lock()
	a.sftpLink = &sftpLink{
		session:     session,
		serverID:    prompt.ServerID,
		fingerprint: fingerprint,
		lastUsed:    time.Now(),
	}
	a.sftpMu.Unlock()

	return a.SFTPStatus(), nil
}

// SFTPStatus reports the connection, for the panel header.
func (a *App) SFTPStatus() *SFTPStatus {
	a.sftpMu.Lock()
	link := a.sftpLink
	a.sftpMu.Unlock()

	if link == nil {
		return &SFTPStatus{}
	}
	return &SFTPStatus{
		Connected:   true,
		Host:        link.session.Host(),
		User:        link.session.User(),
		ServerID:    link.serverID,
		Streams:     link.session.Streams(),
		Fingerprint: link.fingerprint,
	}
}

// SFTPDisconnect closes the connection and forgets the password with it.
func (a *App) SFTPDisconnect() {
	a.sftpMu.Lock()
	link := a.sftpLink
	a.sftpLink = nil
	if a.sftpCancel != nil {
		a.sftpCancel()
		a.sftpCancel = nil
	}
	a.sftpMu.Unlock()

	if link != nil {
		_ = link.session.Close()
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "sftp-status", a.SFTPStatus())
	}
}

// SFTPForgetHost drops a remembered host key, so the next connection asks
// again. For a node that really was rebuilt.
func (a *App) SFTPForgetHost(host string) error {
	if a.sftpHosts == nil {
		return errors.New("no host store")
	}
	return a.sftpHosts.Forget(host)
}

// SFTPKnownHosts lists what has been trusted.
func (a *App) SFTPKnownHosts() map[string]string {
	if a.sftpHosts == nil {
		return map[string]string{}
	}
	return a.sftpHosts.Fingerprints()
}

// live returns the session, or says why there is not one.
func (a *App) live() (*sftpx.Session, string, error) {
	a.sftpMu.Lock()
	defer a.sftpMu.Unlock()
	if a.sftpLink == nil {
		return nil, "", errors.New("not connected over SFTP")
	}
	a.sftpLink.lastUsed = time.Now()
	return a.sftpLink.session, a.sftpLink.serverID, nil
}

/* ------------------------------------------------------------- transfers */

// SFTPTransfer is what a finished transfer reports.
type SFTPTransfer struct {
	Moved     int      `json:"moved"`
	Failed    []string `json:"failed"`
	Skipped   []string `json:"skipped"`
	Replaced  []string `json:"replaced"`
	Conflicts []string `json:"conflicts"`
	Bytes     int64    `json:"bytes"`
	Seconds   float64  `json:"seconds"`
	Cancelled bool     `json:"cancelled"`
	Truncated bool     `json:"truncated"`
}

// SFTPCancel stops the transfer in progress.
func (a *App) SFTPCancel() {
	a.sftpMu.Lock()
	cancel := a.sftpCancel
	a.sftpMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// transferContext hands out the context this transfer runs under, replacing
// whatever the last one used.
func (a *App) transferContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	a.sftpMu.Lock()
	if a.sftpCancel != nil {
		a.sftpCancel()
	}
	a.sftpCancel = cancel
	a.sftpMu.Unlock()

	return ctx, func() {
		cancel()
		a.sftpMu.Lock()
		if a.sftpCancel != nil {
			a.sftpCancel = nil
		}
		a.sftpMu.Unlock()
	}
}

// progress forwards the engine's counters to the window, unchanged.
func (a *App) progress(kind string) func(sftpx.Progress) {
	return func(p sftpx.Progress) {
		if a.ctx == nil {
			return
		}
		runtime.EventsEmit(a.ctx, "sftp-progress", map[string]interface{}{
			"kind":        kind,
			"done":        p.Done,
			"total":       p.Total,
			"bytes":       p.Bytes,
			"total_bytes": p.TotalBytes,
			"current":     p.Current,
		})
	}
}

// SFTPUpload sends local paths — files or whole folders — into remoteDir.
//
// overwrite and keepCopy mean what they do everywhere else in this app: without
// overwrite an existing name is reported as a conflict and left alone, and with
// keepCopy the file being replaced goes to the recycle bin first. The copy is
// taken over the API rather than SFTP because that is where the store's
// plumbing already is, and it only happens for files actually being replaced.
func (a *App) SFTPUpload(localPaths []string, remoteDir string, overwrite, keepCopy bool) (*SFTPTransfer, error) {
	session, serverID, err := a.live()
	if err != nil {
		return nil, err
	}
	if len(localPaths) == 0 {
		return nil, errors.New("nothing selected")
	}

	base, err := normalizeRemotePath(remoteDir)
	if err != nil {
		return nil, err
	}

	ctx, done := a.transferContext()
	defer done()

	out := &SFTPTransfer{
		Failed: []string{}, Skipped: []string{}, Replaced: []string{}, Conflicts: []string{},
	}

	// Expand folders on this machine first, so the whole job is known before
	// anything is sent and the progress bar means something.
	jobs := []sftpx.Job{}
	for _, local := range localPaths {
		info, statErr := os.Stat(local)
		if statErr != nil {
			out.Failed = append(out.Failed, local+" — "+statErr.Error())
			continue
		}

		if !info.IsDir() {
			jobs = append(jobs, sftpx.Job{
				Local:  local,
				Remote: joinRemote(base, filepath.Base(local)),
				Size:   info.Size(),
			})
			continue
		}

		root := filepath.Clean(local)
		top := filepath.Base(root)
		walkErr := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				out.Failed = append(out.Failed, p+" — "+err.Error())
				return nil
			}
			if fi.IsDir() || !fi.Mode().IsRegular() {
				return nil
			}
			if len(jobs) >= sftpMaxEntries {
				out.Truncated = true
				return filepath.SkipAll
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return nil
			}
			jobs = append(jobs, sftpx.Job{
				Local:  p,
				Remote: joinRemote(base, top+"/"+filepath.ToSlash(rel)),
				Size:   fi.Size(),
			})
			return nil
		})
		if walkErr != nil {
			out.Failed = append(out.Failed, local+" — "+walkErr.Error())
		}
	}

	if len(jobs) == 0 {
		return out, nil
	}

	// What is already there, checked once per target directory rather than
	// once per file.
	existing, err := a.sftpExisting(ctx, session, jobs)
	if err != nil {
		return nil, err
	}

	keep := jobs[:0]
	for _, job := range jobs {
		found, clash := existing[job.Remote]
		if !clash {
			keep = append(keep, job)
			continue
		}
		if found.IsDir {
			out.Failed = append(out.Failed, job.Remote+" — a folder is already there")
			continue
		}
		if !overwrite {
			out.Conflicts = append(out.Conflicts, job.Remote)
			continue
		}
		if keepCopy {
			if err := a.sftpKeepCopy(serverID, job.Remote, found.Size); err != nil {
				out.Failed = append(out.Failed, job.Remote+" — keeping a copy failed: "+err.Error())
				continue
			}
		}
		out.Replaced = append(out.Replaced, job.Remote)
		// Tells the engine to go through a temporary name: there is something
		// here worth not half-overwriting.
		job.Replace = true
		keep = append(keep, job)
	}
	jobs = keep

	if len(jobs) == 0 {
		return out, nil
	}

	result := session.Upload(ctx, jobs, a.progress("upload"))
	a.foldResult(out, result)

	// The folders it wrote into are no longer what the explorer has cached.
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "sftp-finished", "upload")
	}
	return out, nil
}

// SFTPDownload pulls remote paths — files or whole folders — into localDir.
func (a *App) SFTPDownload(remotePaths []string, localDir string) (*SFTPTransfer, error) {
	session, _, err := a.live()
	if err != nil {
		return nil, err
	}
	if len(remotePaths) == 0 {
		return nil, errors.New("nothing selected")
	}
	if strings.TrimSpace(localDir) == "" {
		return nil, errors.New("no folder to download into")
	}

	ctx, done := a.transferContext()
	defer done()

	out := &SFTPTransfer{
		Failed: []string{}, Skipped: []string{}, Replaced: []string{}, Conflicts: []string{},
	}

	cleaned := make([]string, 0, len(remotePaths))
	seen := map[string]bool{}
	for _, raw := range remotePaths {
		norm, normErr := normalizeRemotePath(raw)
		if normErr != nil {
			out.Failed = append(out.Failed, raw+" — "+normErr.Error())
			continue
		}
		if !seen[norm] {
			seen[norm] = true
			cleaned = append(cleaned, norm)
		}
	}
	sort.Strings(cleaned)
	cleaned = collapseNested(cleaned)

	jobs := []sftpx.Job{}
	for _, remote := range cleaned {
		entry, ok, statErr := session.Stat(ctx, remote)
		if statErr != nil {
			out.Failed = append(out.Failed, remote+" — "+statErr.Error())
			continue
		}
		if !ok {
			out.Failed = append(out.Failed, remote+" no longer exists — refresh the file list")
			continue
		}

		if !entry.IsDir {
			jobs = append(jobs, sftpx.Job{
				Local:  filepath.Join(localDir, sanitizeLocalName(entry.Name)),
				Remote: remote,
				Size:   entry.Size,
			})
			continue
		}

		items, truncated, walkErr := session.WalkDir(ctx, remote, sftpMaxEntries-len(jobs))
		if walkErr != nil && ctx.Err() == nil {
			out.Failed = append(out.Failed, remote+" — "+walkErr.Error())
			continue
		}
		if truncated {
			out.Truncated = true
		}

		parent := path.Dir(strings.TrimSuffix(remote, "/"))
		for _, item := range items {
			if item.IsDir {
				continue
			}
			rel := strings.TrimPrefix(item.Path, strings.TrimSuffix(parent, "/")+"/")
			jobs = append(jobs, sftpx.Job{
				Local:  filepath.Join(localDir, filepath.FromSlash(safeLocalRel(rel))),
				Remote: item.Path,
				Size:   item.Size,
			})
		}
	}

	if len(jobs) == 0 {
		return out, nil
	}

	result := session.Download(ctx, jobs, a.progress("download"))
	a.foldResult(out, result)
	return out, nil
}

// sftpExisting checks each target directory once and returns what is in them.
func (a *App) sftpExisting(ctx context.Context, session *sftpx.Session, jobs []sftpx.Job) (map[string]sftpx.Entry, error) {
	dirs := map[string]bool{}
	for _, job := range jobs {
		dirs[path.Dir(job.Remote)] = true
	}

	out := map[string]sftpx.Entry{}
	for dir := range dirs {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		items, err := session.List(ctx, dir)
		if err != nil {
			// A directory that is not there yet is not an error: nothing in it
			// can clash.
			continue
		}
		for _, item := range items {
			out[item.Path] = item
		}
	}
	return out, nil
}

// sftpKeepCopy files the current remote bytes in the recycle bin before an
// upload replaces them, the same as the API path does.
func (a *App) sftpKeepCopy(serverID, remotePath string, size int64) error {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient(serverID, func(c *pterodactyl.Client, panel string) error {
		_, err := a.captureRemote(safestore.KindBin, c, panel, serverIDOr(serverID, c),
			remotePath, "replaced by an SFTP upload", size)
		return err
	})
}

// foldResult copies the engine's per-file outcome into the app's shape.
func (a *App) foldResult(out *SFTPTransfer, result sftpx.Result) {
	out.Bytes += result.Bytes
	out.Seconds = result.Seconds
	out.Cancelled = out.Cancelled || result.Cancelled

	for _, file := range result.Files {
		if file.Job.Remote == "" && file.Job.Local == "" {
			// A slot for a job the walk never reached, because the transfer
			// was cancelled before it got there.
			continue
		}
		if file.Error != "" {
			out.Failed = append(out.Failed, file.Job.Remote+" — "+file.Error)
			continue
		}
		out.Moved++
	}
}

// safeLocalRel keeps a remote relative path from escaping the download folder.
//
// The names come from somebody's server, and ".." in one of them is how a
// download writes outside the directory it was pointed at.
func safeLocalRel(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, sanitizeLocalName(part))
	}
	if len(out) == 0 {
		return "download"
	}
	return strings.Join(out, "/")
}

// sftpIdleSweep closes a connection nobody has used for a while, so a password
// is not held in memory all afternoon because a transfer panel was left open.
func (a *App) sftpIdleSweep() {
	for {
		time.Sleep(time.Minute)

		a.sftpMu.Lock()
		link := a.sftpLink
		idle := link != nil && time.Since(link.lastUsed) > sftpIdleTimeout
		a.sftpMu.Unlock()

		if idle {
			a.SFTPDisconnect()
		}
	}
}

/* ----------------------------------------------------------- pick a place */

// PickLocalFolder asks where to put a download.
func (a *App) PickLocalFolder(title string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("no window to ask from")
	}
	if title == "" {
		title = "Choose where to save"
	}
	chosen, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return "", err
	}
	return chosen, nil
}

// PickLocalFiles asks which files to upload.
func (a *App) PickLocalFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, errors.New("no window to ask from")
	}
	chosen, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose files to upload",
	})
	if err != nil {
		return nil, err
	}
	if chosen == nil {
		chosen = []string{}
	}
	return chosen, nil
}

/* ------------------------------------------------------------ local files */

// LocalEntry is one thing in a folder on this machine.
type LocalEntry struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
}

// LocalListing is one folder on this machine, for the local half of the
// transfer view.
type LocalListing struct {
	Path    string       `json:"path"`
	Parent  string       `json:"parent"`
	Entries []LocalEntry `json:"entries"`
	// Roots are the drives on this machine, so the view has somewhere to go
	// when it is at the top.
	Roots []string `json:"roots"`
}

// ListLocal reads a folder on this machine.
//
// An empty path means the user's home directory, which is where a transfer
// view should open rather than at a drive root nobody keeps anything in.
func (a *App) ListLocal(dir string) (*LocalListing, error) {
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = home
	}

	cleaned := filepath.Clean(dir)
	items, err := os.ReadDir(cleaned)
	if err != nil {
		return nil, err
	}

	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		// Already at a drive root; there is no up from here.
		parent = ""
	}

	out := &LocalListing{
		Path:    cleaned,
		Parent:  parent,
		Entries: []LocalEntry{},
		Roots:   localRoots(),
	}

	for _, item := range items {
		info, infoErr := item.Info()
		if infoErr != nil {
			// A file that cannot be stat'd is still worth listing; it just has
			// no size or date.
			out.Entries = append(out.Entries, LocalEntry{
				Path:  filepath.Join(cleaned, item.Name()),
				Name:  item.Name(),
				IsDir: item.IsDir(),
			})
			continue
		}
		out.Entries = append(out.Entries, LocalEntry{
			Path:    filepath.Join(cleaned, item.Name()),
			Name:    item.Name(),
			Size:    info.Size(),
			IsDir:   item.IsDir(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}

	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].IsDir != out.Entries[j].IsDir {
			return out.Entries[i].IsDir
		}
		return strings.ToLower(out.Entries[i].Name) < strings.ToLower(out.Entries[j].Name)
	})
	return out, nil
}

// ListRemote reads a folder over SFTP, for the other half of the view.
func (a *App) ListRemote(dir string) ([]sftpx.Entry, error) {
	session, _, err := a.live()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(dir) == "" {
		dir = "/"
	}
	cleaned, err := normalizeRemotePath(dir)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	items, err := session.List(ctx, cleaned)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

// RevealLocal opens a folder in the system file manager, for when the app is
// not the right tool for what comes next.
func (a *App) RevealLocal(dir string) error {
	if a.ctx == nil {
		return errors.New("no window")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	// Explorer only: this takes a path from the app's own UI, never from a
	// server, and nothing else is passed to it.
	return exec.Command("explorer", filepath.Clean(dir)).Start()
}

// localRoots lists the drives on this machine.
func localRoots() []string {
	roots := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	for letter := 'A'; letter <= 'Z'; letter++ {
		drive := string(letter) + ":\\"
		if _, err := os.Stat(drive); err == nil {
			roots = append(roots, drive)
		}
	}
	return roots
}
