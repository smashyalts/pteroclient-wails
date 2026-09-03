package pterodactyl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// Client represents a Pterodactyl API client
type Client struct {
	client   *resty.Client
	baseURL  string
	apiKey   string
	serverID string
	isAdmin  bool // Track if this is an admin API key
}

// FileAttributes represents the attributes of a file or directory
type FileAttributes struct {
	Name        string    `json:"name"`
	Mode        string    `json:"mode"`
	ModeBits    string    `json:"mode_bits"`
	Size        int64     `json:"size"`
	IsFile      bool      `json:"is_file"`
	IsDirectory bool      `json:"is_directory"`
	IsSymlink   bool      `json:"is_symlink"`
	MimeType    string    `json:"mimetype"`
	CreatedAt   time.Time `json:"created_at"`
	ModifiedAt  time.Time `json:"modified_at"`
}

// FileObject represents a file object in the API response
type FileObject struct {
	Object     string         `json:"object"`
	Attributes FileAttributes `json:"attributes"`
}

// FileInfo represents a file or directory (simplified for internal use)
type FileInfo struct {
	Name       string
	Mode       string
	ModeBits   string
	Size       int64
	IsFile     bool
	IsSymlink  bool
	MimeType   string
	CreatedAt  time.Time
	ModifiedAt time.Time
}

// ListFilesResponse represents the response from the list files endpoint
type ListFilesResponse struct {
	Object string       `json:"object"`
	Data   []FileObject `json:"data"`
}

// FileContent represents file content response
type FileContent struct {
	Content string `json:"content"`
}

// RenameRequest represents a file rename request
type RenameRequest struct {
	Root  string       `json:"root"`
	Files []RenameFile `json:"files"`
}

// RenameFile represents a single file rename operation
type RenameFile struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DeleteRequest represents a file deletion request
type DeleteRequest struct {
	Root  string   `json:"root"`
	Files []string `json:"files"`
}

// ServerAttributes represents server attributes from the API
type ServerAttributes struct {
	UUID        string `json:"uuid"`
	Identifier  string `json:"identifier"` // Admin API uses identifier
	Name        string `json:"name"`
	Description string `json:"description"`
	IsOwner     bool   `json:"is_owner"`
	Status      string `json:"status,omitempty"`
}

// ServerObject represents a server object in the API response
type ServerObject struct {
	Object     string           `json:"object"`
	Attributes ServerAttributes `json:"attributes"`
}

// ListServersResponse represents the response from list servers endpoint
type ListServersResponse struct {
	Object string         `json:"object"`
	Data   []ServerObject `json:"data"`
}

// ServerInfo represents simplified server information
// ID is always the short server identifier (identifier) used by client endpoints
// UUID is provided for reference when needed
type ServerInfo struct {
	ID          string `json:"id"`
	UUID        string `json:"uuid,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsOwner     bool   `json:"is_owner"`
	Status      string `json:"status,omitempty"`
}

// NewClient creates a new Pterodactyl API client
func NewClient(baseURL, apiKey, serverID string) *Client {
	client := resty.New()
	client.SetTimeout(30 * time.Second)
	client.SetHeader("Authorization", "Bearer "+apiKey)
	client.SetHeader("Accept", "application/json")
	client.SetHeader("Content-Type", "application/json")

	c := &Client{
		client:   client,
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		apiKey:   apiKey,
		serverID: serverID,
	}

	// Auto-detect if this is an admin API key
	c.detectAPIType()

	return c
}

// detectAPIType attempts to detect if this is an admin or client API key
func (c *Client) detectAPIType() {
	// Try admin API first - check if we can list all servers
	adminEndpoint := fmt.Sprintf("%s/api/application/servers", c.baseURL)
	resp, err := c.client.R().
		SetQueryParam("per_page", "1").
		Get(adminEndpoint)

	if err == nil && resp.StatusCode() == http.StatusOK {
		c.isAdmin = true
		return
	}

	// Not admin, must be client API
	c.isAdmin = false
}

// SetServerID changes the active server ID
func (c *Client) SetServerID(serverID string) {
	c.serverID = serverID
}

// Close closes all HTTP connections
func (c *Client) Close() {
	// This forces the resty client to close all connections
	if c.client != nil {
		c.client.GetClient().CloseIdleConnections()
	}
}

// GetServerID returns the current server ID
func (c *Client) GetServerID() string {
	return c.serverID
}

// GetBaseURL returns the base URL for debugging
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// IsAdmin returns whether this client is using admin API
func (c *Client) IsAdmin() bool {
	return c.isAdmin
}

// ListServers lists all servers the user has access to
func (c *Client) ListServers() ([]ServerInfo, error) {
	if c.isAdmin {
		return c.listServersAdmin()
	}
	return c.listServersClient()
}

// listServersClient lists servers using client API
func (c *Client) listServersClient() ([]ServerInfo, error) {
	endpoint := fmt.Sprintf("%s/api/client", c.baseURL)

	resp, err := c.client.R().
		SetResult(&ListServersResponse{}).
		Get(endpoint)

	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	result := resp.Result().(*ListServersResponse)

	// Convert ServerObjects to ServerInfo
	servers := make([]ServerInfo, len(result.Data))
	for i, obj := range result.Data {
		servers[i] = ServerInfo{
			ID:          obj.Attributes.UUID, // Client API returns UUID as the identifier
			UUID:        obj.Attributes.UUID,
			Name:        obj.Attributes.Name,
			Description: obj.Attributes.Description,
			IsOwner:     obj.Attributes.IsOwner,
			Status:      obj.Attributes.Status,
		}
	}

	return servers, nil
}

// listServersAdmin lists servers using admin API
func (c *Client) listServersAdmin() ([]ServerInfo, error) {
	endpoint := fmt.Sprintf("%s/api/application/servers", c.baseURL)

	// Admin API requires pagination parameters
	resp, err := c.client.R().
		SetQueryParam("per_page", "100").
		Get(endpoint)

	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	// Parse admin API response
	var adminResp struct {
		Data []struct {
			Attributes struct {
				Identifier  string `json:"identifier"`
				UUID        string `json:"uuid"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp.Body(), &adminResp); err != nil {
		return nil, fmt.Errorf("failed to parse admin response: %w", err)
	}

	// Convert to ServerInfo
	servers := make([]ServerInfo, len(adminResp.Data))
	for i, obj := range adminResp.Data {
		servers[i] = ServerInfo{
			ID:          obj.Attributes.Identifier, // Admin API uses identifier
			UUID:        obj.Attributes.UUID,       // Store UUID separately for file operations
			Name:        obj.Attributes.Name,
			Description: obj.Attributes.Description,
			IsOwner:     true, // Admins own all servers
		}
	}

	return servers, nil
}

// ListFiles lists files in a directory
func (c *Client) ListFiles(path string) ([]FileInfo, error) {
	// Note: Admin API keys can also access client endpoints
	encodedPath := url.QueryEscape(path)
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/list?directory=%s", c.baseURL, c.serverID, encodedPath)

	resp, err := c.client.R().
		SetResult(&ListFilesResponse{}).
		Get(endpoint)

	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	result := resp.Result().(*ListFilesResponse)

	// Convert FileObjects to FileInfo
	files := make([]FileInfo, len(result.Data))
	for i, obj := range result.Data {
		files[i] = FileInfo{
			Name:       obj.Attributes.Name,
			Mode:       obj.Attributes.Mode,
			ModeBits:   obj.Attributes.ModeBits,
			Size:       obj.Attributes.Size,
			IsFile:     obj.Attributes.IsFile,
			IsSymlink:  obj.Attributes.IsSymlink,
			MimeType:   obj.Attributes.MimeType,
			CreatedAt:  obj.Attributes.CreatedAt,
			ModifiedAt: obj.Attributes.ModifiedAt,
		}
	}

	return files, nil
}

// GetFileContent retrieves the content of a file
func (c *Client) GetFileContent(path string) (string, error) {
	// Note: Admin API keys can also access client endpoints
	encodedPath := url.QueryEscape(path)
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/contents?file=%s", c.baseURL, c.serverID, encodedPath)

	resp, err := c.client.R().
		Get(endpoint)

	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return string(resp.Body()), nil
}

// SaveFileContent saves content to a file
func (c *Client) SaveFileContent(path, content string) error {
	encodedPath := url.QueryEscape(path)
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/write?file=%s", c.baseURL, c.serverID, encodedPath)

	resp, err := c.client.R().
		SetBody(content).
		SetHeader("Content-Type", "text/plain").
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// CreateDirectory creates a new directory
func (c *Client) CreateDirectory(path, name string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/create-folder", c.baseURL, c.serverID)

	body := map[string]string{
		"root": path,
		"name": name,
	}

	resp, err := c.client.R().
		SetBody(body).
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// RenameFile renames a file or directory
func (c *Client) RenameFile(root, oldName, newName string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/rename", c.baseURL, c.serverID)

	req := RenameRequest{
		Root: root,
		Files: []RenameFile{
			{
				From: oldName,
				To:   newName,
			},
		},
	}

	resp, err := c.client.R().
		SetBody(req).
		Put(endpoint)

	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// DeleteFiles deletes one or more files
func (c *Client) DeleteFiles(root string, files []string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/delete", c.baseURL, c.serverID)

	req := DeleteRequest{
		Root:  root,
		Files: files,
	}

	resp, err := c.client.R().
		SetBody(req).
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to delete files: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// DownloadFile gets a download URL for a file
func (c *Client) GetDownloadURL(path string) (string, error) {
	encodedPath := url.QueryEscape(path)
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/download?file=%s", c.baseURL, c.serverID, encodedPath)

	resp, err := c.client.R().
		Get(endpoint)

	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Attributes struct {
			URL string `json:"url"`
		} `json:"attributes"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Attributes.URL, nil
}

// UploadFile uploads a file to the server
func (c *Client) UploadFile(path string, filename string, content io.Reader) error {
	// First, get upload URL
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/upload", c.baseURL, c.serverID)

	resp, err := c.client.R().
		Get(endpoint)

	if err != nil {
		return fmt.Errorf("failed to get upload URL: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var uploadInfo struct {
		Attributes struct {
			URL string `json:"url"`
		} `json:"attributes"`
	}

	if err := json.Unmarshal(resp.Body(), &uploadInfo); err != nil {
		return fmt.Errorf("failed to parse upload info: %w", err)
	}

	// Now upload the file
	uploadClient := resty.New()
	uploadResp, err := uploadClient.R().
		SetFileReader("files", filename, content).
		Post(uploadInfo.Attributes.URL + "&directory=" + url.QueryEscape(path))

	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	if uploadResp.StatusCode() != http.StatusOK && uploadResp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("upload returned status %d: %s", uploadResp.StatusCode(), uploadResp.String())
	}

	return nil
}

// TestConnection tests if the API connection is working
func (c *Client) TestConnection() error {
	var endpoint string

	if c.isAdmin {
		// For admin API, test with servers endpoint
		endpoint = fmt.Sprintf("%s/api/application/servers/%s", c.baseURL, c.serverID)
	} else {
		// For client API, use the standard endpoint
		endpoint = fmt.Sprintf("%s/api/client/servers/%s", c.baseURL, c.serverID)
	}

	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// SendConsoleCommand sends a command to the server console
func (c *Client) SendConsoleCommand(command string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/command", c.baseURL, c.serverID)

	payload := map[string]string{
		"command": command,
	}

	resp, err := c.client.R().
		SetBody(payload).
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to send command: status %d", resp.StatusCode())
	}

	return nil
}

// GetServerState gets the current server power state
func (c *Client) GetServerState() (string, error) {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/resources", c.baseURL, c.serverID)

	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to get server state: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	var result struct {
		Attributes struct {
			CurrentState string `json:"current_state"`
		} `json:"attributes"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", err
	}

	return result.Attributes.CurrentState, nil
}

// SetPowerState sets the server power state (start, stop, restart, kill)
func (c *Client) SetPowerState(state string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/power", c.baseURL, c.serverID)

	payload := map[string]string{
		"signal": state,
	}

	resp, err := c.client.R().
		SetBody(payload).
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to set power state: %w", err)
	}

	if resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to set power state: status %d", resp.StatusCode())
	}

	return nil
}

// WebSocketCredentials represents the credentials for connecting to the console WebSocket
type WebSocketCredentials struct {
	Token  string `json:"token"`
	Socket string `json:"socket"`
}

// GetWebSocketCredentials retrieves WebSocket credentials for console connection
func (c *Client) GetWebSocketCredentials() (*WebSocketCredentials, error) {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/websocket", c.baseURL, c.serverID)

	resp, err := c.client.R().
		Get(endpoint)

	if err != nil {
		return nil, fmt.Errorf("failed to get WebSocket credentials: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get WebSocket credentials: status %d", resp.StatusCode())
	}

	var response struct {
		Data WebSocketCredentials `json:"data"`
	}

	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to parse WebSocket credentials: %w", err)
	}

	return &response.Data, nil
}

// CompressFiles archives files and directories inside root into a new
// tar.gz that the panel names and places in root. Returns the archive's
// attributes, whose "name" is the generated filename.
func (c *Client) CompressFiles(root string, files []string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/compress", c.baseURL, c.serverID)

	resp, err := c.client.R().
		SetBody(map[string]interface{}{
			"root":  root,
			"files": files,
		}).
		Post(endpoint)

	if err != nil {
		return nil, fmt.Errorf("failed to compress: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() > 299 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var parsed struct {
		Attributes map[string]interface{} `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse compress response: %w", err)
	}
	return parsed.Attributes, nil
}

// DecompressFile extracts an archive into root. file is the archive's name
// relative to root.
//
// Extraction is the one file operation whose effect cannot be predicted from
// the outside: the archive decides what it writes, so a file it happens to
// contain replaces the one already there with no chance to copy it first. The
// caller is expected to have said so.
func (c *Client) DecompressFile(root, file string) error {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s/files/decompress", c.baseURL, c.serverID)

	resp, err := c.client.R().
		SetBody(map[string]string{
			"root": root,
			"file": file,
		}).
		Post(endpoint)

	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() > 299 {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}
	return nil
}

// downloadHTTP fetches signed node URLs. Separate from the API client because
// those transfers are whole files rather than JSON: no auth header (the URL
// carries its own token) and no 30 second ceiling.
var downloadHTTP = &http.Client{Timeout: 30 * time.Minute}

// openDownload resolves a file to its signed URL and starts the transfer.
//
// /files/contents is the file *editor* endpoint and the panel caps it — a zip
// or a jar comes back as FileSizeTooLargeException. /files/download has no such
// cap; it hands out a short-lived URL pointing at the node, which is what the
// panel's own download button uses.
func (c *Client) openDownload(remotePath string) (*http.Response, error) {
	signed, err := c.GetDownloadURL(remotePath)
	if err != nil {
		return nil, err
	}

	resp, err := downloadHTTP.Get(signed)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", remotePath, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("download returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// DownloadFileTo streams a file straight onto the local disk. Nothing is held
// in memory, so the size of the file is not the size of the process.
func (c *Client) DownloadFileTo(remotePath, localPath string) (int64, error) {
	resp, err := c.openDownload(remotePath)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return 0, fmt.Errorf("cannot write %s: %w", localPath, err)
	}

	written, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()

	if copyErr != nil {
		// A partial file is worse than none: it looks like a download that
		// worked until something tries to open it.
		os.Remove(localPath)
		return 0, fmt.Errorf("download of %s was interrupted: %w", remotePath, copyErr)
	}
	if closeErr != nil {
		os.Remove(localPath)
		return 0, closeErr
	}
	return written, nil
}

// DownloadFileBytes reads a file into memory through the download endpoint,
// refusing anything over limit. Used where the bytes have to be handed on —
// a copy to another server, a copy into the local store — rather than saved.
func (c *Client) DownloadFileBytes(remotePath string, limit int64) ([]byte, error) {
	resp, err := c.openDownload(remotePath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if limit > 0 && resp.ContentLength > limit {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte limit", remotePath, resp.ContentLength, limit)
	}

	// One byte past the limit, so a body with no Content-Length is caught too
	// rather than read forever.
	reader := io.Reader(resp.Body)
	if limit > 0 {
		reader = io.LimitReader(resp.Body, limit+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", remotePath, err)
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over the %d byte limit", remotePath, limit)
	}
	return data, nil
}

/* ------------------------------------------------------------------ SFTP */

// SFTPDetails is where a server's SFTP service lives, and who to log in as.
//
// Pterodactyl authenticates SFTP against the panel account rather than the API
// key, and the username is the account's name joined to the server's short id.
// The password is the account's own, which is why nothing here asks the API for
// it — the API cannot give it, and the app has to be told.
type SFTPDetails struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	ServerID string `json:"server_id"`
}

// GetSFTPDetails reads the SFTP endpoint from the server record and builds the
// username the panel expects.
func (c *Client) GetSFTPDetails() (*SFTPDetails, error) {
	endpoint := fmt.Sprintf("%s/api/client/servers/%s", c.baseURL, c.serverID)

	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to read the server record: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Attributes struct {
			Identifier  string `json:"identifier"`
			SFTPDetails struct {
				IP   string `json:"ip"`
				Port int    `json:"port"`
			} `json:"sftp_details"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	details := &SFTPDetails{
		Host:     result.Attributes.SFTPDetails.IP,
		Port:     result.Attributes.SFTPDetails.Port,
		ServerID: result.Attributes.Identifier,
	}
	if details.ServerID == "" {
		details.ServerID = c.serverID
	}
	if details.Host == "" {
		return nil, fmt.Errorf("this panel did not say where its SFTP service is")
	}

	// Best effort: without it the user types the whole username themselves,
	// which is a worse first run but not a broken one.
	if account, accErr := c.GetAccountUsername(); accErr == nil && account != "" {
		details.Username = account + "." + details.ServerID
	}
	return details, nil
}

// GetAccountUsername reads the logged-in account's username, which is the first
// half of the SFTP login.
func (c *Client) GetAccountUsername() (string, error) {
	endpoint := fmt.Sprintf("%s/api/client/account", c.baseURL)

	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to read the account: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	var result struct {
		Attributes struct {
			Username string `json:"username"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", err
	}
	return result.Attributes.Username, nil
}
