package pteroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pteroclient-wails/pkg/download"
)

// Backup is the part of a backup record a download needs.
type Backup struct {
	UUID         string    `json:"uuid"`
	Name         string    `json:"name"`
	Bytes        int64     `json:"bytes"`
	Checksum     string    `json:"checksum"`
	ChecksumType string    `json:"checksum_type"`
	IsSuccessful bool      `json:"is_successful"`
	IsLocked     bool      `json:"is_locked"`
	CompletedAt  time.Time `json:"completed_at"`
}

// Disk driver names, as the panel records them on a backup.
const (
	// DiskWings is a backup sitting on the node's own disk. The panel signs a
	// JWT for the node's /download/backup route.
	DiskWings = "wings"

	// DiskS3 is a backup in a bucket. The panel signs an S3 GetObject URL,
	// which is a different animal entirely: object stores honour Range, so
	// the archive can be pulled in parallel.
	DiskS3 = "s3"
)

// Transfer describes how a particular backup can be fetched, which depends
// entirely on which driver the host configured.
//
// The panel's DownloadLinkService branches on the backup's disk. For a wings
// backup it signs a node JWT valid for fifteen minutes, and wings rejects the
// second request carrying it. For an S3 backup it presigns a GetObject URL
// valid for five minutes, which any number of connections may use at once.
//
// That difference is the whole performance story. Stock wings serves a backup
// with a plain write of the file to the response and no Accept-Ranges, so a
// local backup is one stream and nothing can change that. An S3-backed one
// splits across as many connections as the bucket will take.
type Transfer struct {
	// Parallel says whether ranged, multi-connection download is expected to
	// work. It is a prediction from the disk driver, not a promise; the
	// downloader probes the host and falls back on its own.
	Parallel bool

	// ReusableURL says one signed URL may serve several requests.
	ReusableURL bool

	// URLLifetime is how long a signed URL lasts.
	URLLifetime time.Duration

	// Note explains the above in a sentence, for a UI or a tool result.
	Note string
}

// ClassifySignedURL works out how a backup can be fetched by looking at the
// URL the panel just signed.
//
// This is the only way to know on a stock panel with a client API key. The
// client API's backup transformer returns uuid, name, bytes, checksum, the
// two flags and the timestamps — and no disk field, so nothing in the record
// says where the archive lives. The signed URL does: an S3 presigned link
// carries the signature in its query string, while a wings link is the node's
// own /download/backup route with a JWT.
func ClassifySignedURL(signed string) Transfer {
	lowered := strings.ToLower(signed)

	// SigV4 presigned requests carry X-Amz-Signature; the older SigV2 style
	// uses AWSAccessKeyId. Both appear depending on what the host points the
	// S3 adapter at, which is often not Amazon at all but R2, B2 or MinIO.
	if strings.Contains(lowered, "x-amz-signature") ||
		strings.Contains(lowered, "x-amz-algorithm") ||
		strings.Contains(lowered, "awsaccesskeyid") {
		return TransferFor(DiskS3)
	}
	if strings.Contains(lowered, "/download/backup") {
		return TransferFor(DiskWings)
	}

	// Something else is in front — a CDN, a reverse proxy, a fork. Assume the
	// cautious shape: one connection, no sharing of the URL. The downloader
	// probes for range support regardless, so a host that does support it is
	// still used properly; only the URL is not reused.
	return TransferFor("")
}

// TransferFor reports how a backup on the given disk can be fetched.
//
// The disk is only known when something other than a client key read the
// record, so most callers want ClassifySignedURL instead.
func TransferFor(disk string) Transfer {
	switch strings.ToLower(strings.TrimSpace(disk)) {
	case DiskS3:
		return Transfer{
			Parallel:    true,
			ReusableURL: true,
			URLLifetime: 5 * time.Minute,
			Note: "stored in a bucket: the panel presigns an S3 URL, which honours range " +
				"requests, so this downloads over several connections at once",
		}
	default:
		return Transfer{
			Parallel:    false,
			ReusableURL: false,
			URLLifetime: 15 * time.Minute,
			Note: "stored on the node: wings writes the whole archive to one response and " +
				"ignores range requests, so this is a single stream and cannot be resumed",
		}
	}
}

// BackupRecord is a backup plus the disk it lives on.
type BackupRecord struct {
	Backup
	Disk string `json:"disk"`
}

// GetBackup reads one backup's record.
func (c *Client) GetBackup(ctx context.Context, server, backup string) (*BackupRecord, error) {
	result, err := c.Send(ctx, Request{
		Method: http.MethodGet,
		Path:   "/servers/" + server + "/backups/" + backup,
	})
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 {
		return nil, describeFailure(result)
	}

	var envelope struct {
		Attributes BackupRecord `json:"attributes"`
	}
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the backup record: %w", err)
	}
	if envelope.Attributes.UUID == "" {
		envelope.Attributes.UUID = backup
	}
	return &envelope.Attributes, nil
}

// ListBackups reads a server's backups.
func (c *Client) ListBackups(ctx context.Context, server string) ([]BackupRecord, error) {
	result, err := c.Send(ctx, Request{
		Method: http.MethodGet,
		Path:   "/servers/" + server + "/backups",
	})
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 {
		return nil, describeFailure(result)
	}

	var envelope struct {
		Data []struct {
			Attributes BackupRecord `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the backup list: %w", err)
	}

	out := make([]BackupRecord, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		out = append(out, row.Attributes)
	}
	return out, nil
}

// BackupDownloadURL mints a signed URL for a backup.
//
// Each call is a fresh signature. For a wings backup that is mandatory, since
// the node spends the token on first use; for S3 it is merely wasteful, which
// is why the downloader caches one and only comes back when it is near
// expiry.
func (c *Client) BackupDownloadURL(ctx context.Context, server, backup string) (string, error) {
	result, err := c.Send(ctx, Request{
		Method: http.MethodGet,
		Path:   "/servers/" + server + "/backups/" + backup + "/download",
	})
	if err != nil {
		return "", err
	}
	if result.Status < 200 || result.Status > 299 {
		return "", describeFailure(result)
	}

	var envelope struct {
		Attributes struct {
			URL string `json:"url"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		return "", fmt.Errorf("could not read the signed URL: %w", err)
	}
	if envelope.Attributes.URL == "" {
		return "", fmt.Errorf("the panel returned no download URL for backup %s", backup)
	}
	return envelope.Attributes.URL, nil
}

// BackupJob builds a download job for a backup, filled in from its record so
// the transfer knows the size to expect, the checksum to verify against, and
// whether the host will let it use more than one connection.
func (c *Client) BackupJob(ctx context.Context, server, backup, dest string) (download.Job, Transfer, error) {
	record, err := c.GetBackup(ctx, server, backup)
	if err != nil {
		return download.Job{}, Transfer{}, err
	}
	if !record.IsSuccessful {
		return download.Job{}, Transfer{}, fmt.Errorf(
			"backup %s did not complete successfully; there is nothing to download", backup)
	}

	// Predicted from the disk when the record happens to carry one, which a
	// client key's record does not. The real answer arrives with the first
	// signed URL, through Classify below.
	transfer := TransferFor(record.Disk)

	return download.Job{
		Name:         backupLabel(record),
		Dest:         dest,
		Size:         record.Bytes,
		Checksum:     record.Checksum,
		ChecksumType: record.ChecksumType,
		Reusable:     transfer.ReusableURL,
		URLLifetime:  transfer.URLLifetime,
		Mint: func(ctx context.Context) (string, error) {
			return c.BackupDownloadURL(ctx, server, backup)
		},
		Classify: func(signed string) (bool, time.Duration) {
			found := ClassifySignedURL(signed)
			return found.ReusableURL, found.URLLifetime
		},
	}, transfer, nil
}

// backupLabel names a backup for display, falling back to its UUID.
func backupLabel(record *BackupRecord) string {
	if name := strings.TrimSpace(record.Name); name != "" {
		return name
	}
	return record.UUID
}

// SuggestedFilename is what the panel's own UI would call the downloaded
// archive. Backups are always tar.gz.
func SuggestedFilename(record *BackupRecord) string {
	base := strings.TrimSpace(record.Name)
	if base == "" {
		base = record.UUID
	}
	// Keep it to something every filesystem accepts.
	base = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '-'
		}
		return r
	}, base)
	base = strings.TrimSpace(base)
	if base == "" {
		base = "backup"
	}
	if !strings.HasSuffix(strings.ToLower(base), ".tar.gz") {
		base += ".tar.gz"
	}
	return base
}
