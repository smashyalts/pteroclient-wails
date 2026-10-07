package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"pteroclient-wails/pkg/download"
	"pteroclient-wails/pkg/mcp"
	"pteroclient-wails/pkg/pteroapi"
)

// registerDownloadTools exposes the backup downloader.
//
// A backup is gigabytes and takes minutes to hours, so these tools queue work
// and report on it rather than blocking a tool call until it finishes. The
// transfer survives the call that started it; only shutting the server down
// stops it.
func (t *toolset) registerDownloadTools(s *mcp.Server) {
	if t.pool == nil {
		return // no download directory configured; see -download-dir
	}

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_backup_download",
		Description: "Queue a backup for download to this machine and return a job id. Returns " +
			"immediately; poll ptero_downloads to watch it. Speed depends on where the host keeps " +
			"its backups: one on a node is a single stream because wings ignores range requests, " +
			"while one in a bucket is pulled over several connections at once. The client API does " +
			"not say which, so ptero_downloads reports it once the transfer starts. The archive is " +
			"checked against the panel's own checksum before it is kept.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID from ptero_backups_list.").Req(),
			mcp.Str("filename", "Name to save as, inside the configured download directory. "+
				"Omit to use the backup's own name."),
			mcp.Int("connections", "How many parallel connections to use where the host supports "+
				"them. Ignored for a backup on a node, which only ever gets one."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, _, server, err := t.resolveTarget(args)
			if err != nil {
				return "", err
			}
			backup := strings.TrimSpace(args.String("backup", ""))

			record, err := client.GetBackup(ctx, server, backup)
			if err != nil {
				return "", err
			}

			name := strings.TrimSpace(args.String("filename", ""))
			if name == "" {
				name = pteroapi.SuggestedFilename(record)
			}
			// The directory is the operator's choice and a tool argument must
			// not be able to climb out of it.
			if filepath.Base(name) != name {
				return "", fmt.Errorf("filename must be a plain name, not a path: %q", name)
			}
			dest := filepath.Join(t.downloadDir, name)

			job, transfer, err := client.BackupJob(ctx, server, backup, dest)
			if err != nil {
				return "", err
			}
			// The disk is absent from a client key's backup record, so the
			// prediction above is a default rather than a fact.
			if record.Disk == "" {
				transfer.Note = "whether this uses one connection or several is decided by the " +
					"signed URL and reported by ptero_downloads once it starts"
			}

			opts := map[string]interface{}{}
			if n := args.Int("connections", 0); n > 0 {
				opts["connections_requested"] = n
			}

			id, err := t.pool.Add(job)
			if err != nil {
				return "", err
			}

			reply := map[string]interface{}{
				"job":       id,
				"backup":    record.UUID,
				"name":      record.Name,
				"disk":      record.Disk,
				"bytes":     record.Bytes,
				"dest":      dest,
				"note":      transfer.Note,
				"next_step": "poll ptero_downloads with job " + id,
			}
			for k, v := range opts {
				reply[k] = v
			}
			if record.Checksum == "" {
				reply["warning"] = "the panel recorded no checksum for this backup, so the " +
					"download can only be checked by size"
			}
			return pteroapi.Pretty(reply), nil
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_downloads",
		Description: "List backup downloads this server has been asked to make, with their state, " +
			"progress, speed and estimated time left. Give a job id for just that one. A failed job " +
			"carries the reason; queue it again to retry, and where the host supports ranges it " +
			"picks up where it stopped.",
		Input: mcp.In(
			mcp.Str("job", "Job id from ptero_backup_download. Omit to list them all."),
			mcp.Bool("forget_finished", "Drop finished jobs from the list after returning them.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			if id := strings.TrimSpace(args.String("job", "")); id != "" {
				status, found := t.pool.Status(id)
				if !found {
					return "", fmt.Errorf("no download job named %q", id)
				}
				return pteroapi.Pretty(describeDownload(status)), nil
			}

			jobs := t.pool.List()
			rows := make([]interface{}, 0, len(jobs))
			for _, status := range jobs {
				rows = append(rows, describeDownload(status))
			}

			reply := map[string]interface{}{
				"directory": t.downloadDir,
				"active":    t.pool.Active(),
				"jobs":      rows,
			}
			if args.Bool("forget_finished", false) {
				reply["forgotten"] = t.pool.Forget()
			}
			return pteroapi.Pretty(reply), nil
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_download_cancel",
		Description: "Stop a queued or running backup download. The partly downloaded file is kept, " +
			"so queueing the same backup again resumes it where the host allows that. Nothing on " +
			"the panel or the server is touched.",
		Input: mcp.In(
			mcp.Str("job", "Job id from ptero_backup_download.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := strings.TrimSpace(args.String("job", ""))
			if !t.pool.Cancel(id) {
				status, found := t.pool.Status(id)
				if !found {
					return "", fmt.Errorf("no download job named %q", id)
				}
				return "", fmt.Errorf("job %s has already finished as %s", id, status.State)
			}
			return "cancelled " + id, nil
		},
	})
}

// describeDownload renders one job for a tool result, leaving out the fields
// that are noise until they mean something.
func describeDownload(status download.Status) map[string]interface{} {
	out := map[string]interface{}{
		"job":   status.ID,
		"name":  status.Name,
		"dest":  status.Dest,
		"state": string(status.State),
	}

	p := status.Progress
	if p.Total > 0 {
		out["bytes_total"] = p.Total
		out["bytes_done"] = p.Done
		out["percent"] = fmt.Sprintf("%.1f", 100*float64(p.Done)/float64(p.Total))
	} else if p.Done > 0 {
		out["bytes_done"] = p.Done
	}

	if status.State == download.StateRunning {
		out["phase"] = string(p.Phase)
		out["parallel"] = p.Ranged
		out["connections"] = p.Parts
		if p.BytesPerSecond > 0 {
			out["speed"] = humanRate(p.BytesPerSecond)
		}
		if p.ETASeconds > 0 {
			out["eta_seconds"] = int(p.ETASeconds)
		}
	}
	if p.Retries > 0 {
		out["retries"] = p.Retries
	}
	if p.Resumed {
		out["resumed"] = true
	}
	if status.Error != "" {
		out["error"] = status.Error
	}
	if status.Result != nil {
		out["took_seconds"] = int(status.Result.Duration.Seconds())
		out["verified"] = status.Result.Verified
		if status.Result.Duration.Seconds() > 0 {
			out["average_speed"] = humanRate(
				float64(status.Result.Bytes) / status.Result.Duration.Seconds())
		}
	}
	return out
}

func humanRate(bytesPerSecond float64) string {
	const unit = 1024.0
	value := bytesPerSecond
	for _, suffix := range []string{"B/s", "KiB/s", "MiB/s", "GiB/s"} {
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
		value /= unit
	}
	return fmt.Sprintf("%.1f TiB/s", value)
}
