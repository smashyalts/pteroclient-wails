package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"pteroclient-wails/pkg/download"
	"pteroclient-wails/pkg/pteroapi"
)

// Backups: listing them, and pulling them down.
//
// The note in panel_tabs.go says there is no Backups binding, because a
// restore overwrites every file on a server and a reinstall wipes them. That
// reasoning does not reach downloading. Reading a backup changes nothing on
// the panel and is the one operation that makes the others survivable, so
// listing and downloading are here while restore and delete stay out.
//
// The transfer goes through pkg/download, which is shared with the MCP server
// so there is one implementation of the awkward parts: the probe for range
// support, the single-use token rule, the retry, the checksum check.
//
// This uses pkg/pteroapi rather than pkg/pterodactyl, which the rest of the
// app uses. The two clients are separate HTTP stacks, and the backup logic
// worth not duplicating — which disk driver implies which download shape — is
// in pteroapi already.

// settingBackupDir remembers the folder chosen in the file dialog.
const settingBackupDir = "backupDownloadDir"

// downloadState is the app's download queue, created on first use because
// most sessions never download a backup.
type downloadState struct {
	once sync.Once
	pool *download.Pool
	dir  string
	mu   sync.Mutex
}

// BackupInfo is one backup as the window shows it.
type BackupInfo struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	Checksum     string `json:"checksum"`
	Disk         string `json:"disk"`
	IsSuccessful bool   `json:"is_successful"`
	IsLocked     bool   `json:"is_locked"`
	CompletedAt  string `json:"completed_at"`

	// TransferNote explains what is and is not knowable before a transfer
	// starts. The client API does not say where a backup is kept — its
	// backup record has no disk field — so whether this one can use several
	// connections is discovered from the signed URL once a download begins,
	// and reported then.
	TransferNote string `json:"transfer_note"`
	Filename     string `json:"filename"`
}

// transferNote says what is known about how a backup will come down.
//
// With a client API key that is: not much, until it starts. The panel's own
// client transformer leaves the disk out of the backup record, so the only
// honest answer before the first signed URL is that it depends on where the
// host keeps its backups.
func transferNote(disk string) string {
	if disk != "" {
		return pteroapi.TransferFor(disk).Note
	}
	return "speed depends on where the host keeps its backups, which the panel does not " +
		"tell a client API key; it is reported once the download starts"
}

// backupClient builds a pteroapi client for the active panel.
func (a *App) backupClient() (*pteroapi.Client, string, error) {
	panel := a.config.GetActivePanel()
	if panel == nil {
		return nil, "", fmt.Errorf("no panel is active")
	}
	if strings.TrimSpace(panel.APIKey) == "" {
		return nil, "", fmt.Errorf("panel %q has no API key", panel.Name)
	}

	serverID := ""
	if a.client != nil {
		serverID = a.client.GetServerID()
	}
	if serverID == "" {
		serverID = panel.ServerID
	}
	if serverID == "" {
		return nil, "", fmt.Errorf("no server selected")
	}

	return pteroapi.New(panel.PanelURL, panel.APIKey), serverID, nil
}

// ListBackups returns the active server's backups.
func (a *App) ListBackups() ([]BackupInfo, error) {
	client, server, err := a.backupClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	records, err := client.ListBackups(ctx, server)
	if err != nil {
		return nil, err
	}

	out := make([]BackupInfo, 0, len(records))
	for i := range records {
		record := records[i]
		completed := ""
		if !record.CompletedAt.IsZero() {
			completed = record.CompletedAt.Format(time.RFC3339)
		}

		out = append(out, BackupInfo{
			UUID:         record.UUID,
			Name:         record.Name,
			Bytes:        record.Bytes,
			Checksum:     record.Checksum,
			Disk:         record.Disk,
			IsSuccessful: record.IsSuccessful,
			IsLocked:     record.IsLocked,
			CompletedAt:  completed,
			TransferNote: transferNote(record.Disk),
			Filename:     pteroapi.SuggestedFilename(&record),
		})
	}
	return out, nil
}

// GetDownloadFolder returns where downloads are saved, defaulting to the
// user's Downloads directory.
func (a *App) GetDownloadFolder() string {
	a.downloads.mu.Lock()
	dir := a.downloads.dir
	a.downloads.mu.Unlock()
	if dir != "" {
		return dir
	}

	if saved := a.config.AppSetting(settingBackupDir, ""); saved != "" {
		return saved
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

// ChooseDownloadFolder asks for a directory and remembers it.
func (a *App) ChooseDownloadFolder() (string, error) {
	picked, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Where should backups be saved?",
		DefaultDirectory: a.GetDownloadFolder(),
	})
	if err != nil {
		return "", err
	}
	if picked == "" {
		return a.GetDownloadFolder(), nil // cancelled
	}

	a.downloads.mu.Lock()
	a.downloads.dir = picked
	a.downloads.mu.Unlock()
	// Straight to the config rather than through App.SetAppSetting, which
	// validates against the settings the window exposes and does not know
	// about this one.
	_ = a.config.SetAppSetting(settingBackupDir, picked)
	return picked, nil
}

// downloadPool starts the queue on first use.
//
// Two backups at a time by default. Against a node-stored backup each one is a
// single stream that the node paces, so running two is the only way to use
// more of the link; running many more just divides the same node's upload
// between them.
func (a *App) downloadPool() *download.Pool {
	a.downloads.once.Do(func() {
		a.downloads.pool = download.NewPool(context.Background(), 2, download.Options{
			Parts: 8,
		}, func(status download.Status) {
			// The window redraws from this rather than polling.
			runtime.EventsEmit(a.ctx, "download-progress", status)
		})
	})
	return a.downloads.pool
}

// StartBackupDownload queues a backup and returns the job id.
func (a *App) StartBackupDownload(backupUUID string) (string, error) {
	client, server, err := a.backupClient()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	record, err := client.GetBackup(ctx, server, backupUUID)
	if err != nil {
		return "", err
	}

	dir := a.GetDownloadFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	dest := filepath.Join(dir, pteroapi.SuggestedFilename(record))

	job, _, err := client.BackupJob(ctx, server, backupUUID, dest)
	if err != nil {
		return "", err
	}
	return a.downloadPool().Add(job)
}

// ListDownloads returns every transfer this session has been asked to make.
func (a *App) ListDownloads() []download.Status {
	return a.downloadPool().List()
}

// CancelDownload stops one. The partial file stays, so starting it again
// resumes where the host allows that.
func (a *App) CancelDownload(id string) bool {
	return a.downloadPool().Cancel(id)
}

// ClearFinishedDownloads drops completed rows from the list.
func (a *App) ClearFinishedDownloads() int {
	return a.downloadPool().Forget()
}

// RevealDownload opens the folder a finished download is in.
func (a *App) RevealDownload(id string) error {
	status, found := a.downloadPool().Status(id)
	if !found {
		return fmt.Errorf("no download named %q", id)
	}
	if status.State != download.StateDone {
		return fmt.Errorf("that download has not finished")
	}
	runtime.BrowserOpenURL(a.ctx, "file://"+filepath.ToSlash(filepath.Dir(status.Dest)))
	return nil
}

// shutdownDownloads stops the queue when the window closes.
//
// Cancelled rather than waited for: a half-finished archive is kept on disk
// and can be resumed, so there is nothing to lose by stopping, and blocking
// the shutdown on a multi-gigabyte transfer would look like a hang.
func (a *App) shutdownDownloads() {
	a.downloads.mu.Lock()
	pool := a.downloads.pool
	a.downloads.mu.Unlock()
	if pool != nil {
		pool.Close()
	}
}
