// Package sftpx moves files over a panel's SFTP service, several at a time.
//
// The client API is one HTTP request per file and caps what it will accept, so
// a folder of a few hundred plugins is a few hundred round trips. SFTP is one
// connection with many channels on it: this opens a handful of subsystems over
// a single SSH connection and keeps all of them busy, which is most of where
// the speed comes from.
//
// Nothing here decides what to transfer or what to overwrite. It is handed a
// list of jobs and reports what happened to each, so the safety rules stay in
// one place — with the rest of them — rather than being reimplemented for a
// second transport.
package sftpx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Defaults. Streams is per transfer, not per file: pkg/sftp already pipelines
// requests within one file, and the win here is overlapping the many small
// files a plugins folder is made of.
const (
	// Eight rather than six: one connection can only hold 2 MB in flight
	// (see multipartMin), so on a fast line the number of connections is the
	// throughput.
	DefaultStreams = 8
	MaxStreams     = 16

	// Requests in flight per file handle. SFTP is request/response, so a
	// sequential transfer costs one round trip per packet: at 32 KB and 50 ms
	// that is about 640 KB/s regardless of bandwidth. Sixty-four outstanding
	// requests is what turns that into line speed.
	requestsPerFile = 64

	// The SFTP packet size. pkg/sftp refuses anything over 32 KB outright —
	// MaxPacketChecked returns an error above it, which made NewClient fail
	// and took every connection attempt with it. Throughput comes from having
	// many packets in flight, not from larger ones.
	chunkSize = 32768

	dialTimeout = 20 * time.Second
)

// preferredCiphers puts AES-GCM first.
//
// Go offers ChaCha20-Poly1305 ahead of AES by default, and the server picks the
// first thing on the client's list that it supports. On anything with AES-NI —
// every desktop this runs on — AES-GCM is several times faster, and on a fast
// link the cipher is what the transfer is waiting for. The rest of Go's list
// follows, so a server without AES still negotiates.
var preferredCiphers = []string{
	"aes128-gcm@openssh.com",
	"aes256-gcm@openssh.com",
	"chacha20-poly1305@openssh.com",
	"aes128-ctr",
	"aes192-ctr",
	"aes256-ctr",
}

// Config is everything needed to open a connection.
type Config struct {
	Host string
	Port int
	User string
	// Password is the panel account's, which is what Pterodactyl's SFTP
	// authenticates against. It is never written anywhere by this package.
	Password string
	Streams  int

	// AcceptFingerprint is a host key the user has just been shown and agreed
	// to. Empty on the first attempt, which is what produces the prompt.
	AcceptFingerprint string
}

func (c Config) address() string {
	port := c.Port
	if port == 0 {
		port = 2022
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

// conn is one SSH connection and the SFTP subsystem on it.
type conn struct {
	ssh  *ssh.Client
	sftp *sftp.Client
}

// Session is a pool of connections.
//
// Connections, not channels on one connection: channels share a single TCP
// stream and therefore a single congestion window, so on a long or lossy path
// they all wait together. Separate connections are what let a large file be
// split across several streams.
type Session struct {
	cfg   Config
	conns []*conn
	pool  chan *conn

	closeOnce sync.Once
}

// Dial opens the connection and its streams.
func Dial(cfg Config, hosts *KnownHosts) (*Session, error) {
	if cfg.Host == "" {
		return nil, errors.New("no SFTP host")
	}
	if cfg.User == "" {
		return nil, errors.New("no SFTP username")
	}
	streams := cfg.Streams
	if streams <= 0 {
		streams = DefaultStreams
	}
	if streams > MaxStreams {
		streams = MaxStreams
	}

	check := ssh.InsecureIgnoreHostKey()
	if hosts != nil {
		check = hosts.callback(cfg.AcceptFingerprint)
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.Password(cfg.Password)},
		HostKeyCallback: check,
		Timeout:         dialTimeout,
	}
	clientCfg.Ciphers = preferredCiphers

	s := &Session{cfg: cfg, pool: make(chan *conn, streams)}

	// The first one on its own: its failure is the one worth reporting, and it
	// is what settles the host key before the rest go out together.
	first, err := s.open(clientCfg)
	if err != nil {
		// The host key errors carry the fingerprint the caller has to show, so
		// they are handed back as they are rather than wrapped into a string.
		var unknown *ErrHostKeyUnknown
		var changed *ErrHostKeyChanged
		if errors.As(err, &unknown) {
			return nil, unknown
		}
		if errors.As(err, &changed) {
			return nil, changed
		}
		return nil, err
	}
	s.conns = append(s.conns, first)
	s.pool <- first

	// The rest in parallel: a handshake is a couple of round trips, and six of
	// them one after another is a visible wait before anything moves.
	var wg sync.WaitGroup
	extra := make([]*conn, streams-1)
	for i := 1; i < streams; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			// The host key is known by now, so this cannot prompt.
			cfgCopy := *clientCfg
			if made, openErr := s.open(&cfgCopy); openErr == nil {
				extra[slot] = made
			}
		}(i - 1)
	}
	wg.Wait()

	for _, made := range extra {
		if made == nil {
			// One that would not open is not a reason to refuse: fewer
			// connections is slower, not broken.
			continue
		}
		s.conns = append(s.conns, made)
		s.pool <- made
	}

	return s, nil
}

// open makes one connection and starts SFTP on it.
func (s *Session) open(clientCfg *ssh.ClientConfig) (*conn, error) {
	client, err := ssh.Dial("tcp", s.cfg.address(), clientCfg)
	if err != nil {
		var unknown *ErrHostKeyUnknown
		var changed *ErrHostKeyChanged
		if errors.As(err, &unknown) || errors.As(err, &changed) {
			return nil, err
		}
		return nil, fmt.Errorf("could not connect to %s: %w", s.cfg.address(), err)
	}

	sub, subErr := sftp.NewClient(client,
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
		sftp.UseFstat(true),
		sftp.MaxConcurrentRequestsPerFile(requestsPerFile),
		sftp.MaxPacketChecked(chunkSize),
	)
	if subErr != nil {
		client.Close()
		return nil, fmt.Errorf("could not start SFTP on %s: %w", s.cfg.address(), subErr)
	}
	return &conn{ssh: client, sftp: sub}, nil
}

// Streams is how many transfers can be in flight at once.
func (s *Session) Streams() int { return len(s.conns) }

// Host is what was connected to, for display.
func (s *Session) Host() string { return s.cfg.address() }

// User is the account in use, for display.
func (s *Session) User() string { return s.cfg.User }

// Close shuts every connection.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		for _, c := range s.conns {
			if closeErr := c.sftp.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if closeErr := c.ssh.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
	})
	return err
}

// borrow takes a connection, waiting for one if all are busy.
func (s *Session) borrow(ctx context.Context) (*conn, error) {
	select {
	case c := <-s.pool:
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Session) give(c *conn) { s.pool <- c }

/* --------------------------------------------------------------- listing */

// Entry is one thing on the far side.
type Entry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// List reads one directory.
func (s *Session) List(ctx context.Context, dir string) ([]Entry, error) {
	sub, err := s.borrow(ctx)
	if err != nil {
		return nil, err
	}
	defer s.give(sub)

	items, err := sub.sftp.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot list %s: %w", dir, err)
	}

	out := make([]Entry, 0, len(items))
	for _, item := range items {
		out = append(out, Entry{
			Path:  path.Join(dir, item.Name()),
			Name:  item.Name(),
			Size:  item.Size(),
			IsDir: item.IsDir(),
		})
	}
	return out, nil
}

// WalkDir collects every file under dir, so a folder download knows what it is
// asking for before it starts.
//
// maxEntries stops a mistake — a symlinked loop, or somebody's node_modules —
// from becoming an unbounded walk.
func (s *Session) WalkDir(ctx context.Context, dir string, maxEntries int) ([]Entry, bool, error) {
	sub, err := s.borrow(ctx)
	if err != nil {
		return nil, false, err
	}
	defer s.give(sub)

	out := []Entry{}
	truncated := false
	queue := []string{dir}

	for len(queue) > 0 {
		if ctx.Err() != nil {
			return out, truncated, ctx.Err()
		}
		if len(out) >= maxEntries {
			truncated = true
			break
		}

		current := queue[0]
		queue = queue[1:]

		items, listErr := sub.sftp.ReadDir(current)
		if listErr != nil {
			// A folder that cannot be read is skipped rather than failing the
			// whole walk; permissions vary inside a container.
			continue
		}

		for _, item := range items {
			full := path.Join(current, item.Name())
			// Symlinks are not followed: one pointing up the tree turns this
			// into a loop, and its target is not what was selected.
			if item.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if item.IsDir() {
				queue = append(queue, full)
				out = append(out, Entry{Path: full, Name: item.Name(), IsDir: true})
				continue
			}
			out = append(out, Entry{Path: full, Name: item.Name(), Size: item.Size()})
		}
	}

	// Shallowest first, so a caller creating directories has a parent before
	// it needs one.
	sort.SliceStable(out, func(i, j int) bool {
		di := strings.Count(strings.Trim(out[i].Path, "/"), "/")
		dj := strings.Count(strings.Trim(out[j].Path, "/"), "/")
		if di != dj {
			return di < dj
		}
		return out[i].Path < out[j].Path
	})
	return out, truncated, nil
}

// Stat reports one path, or whether it is there at all.
func (s *Session) Stat(ctx context.Context, remote string) (Entry, bool, error) {
	sub, err := s.borrow(ctx)
	if err != nil {
		return Entry{}, false, err
	}
	defer s.give(sub)

	info, statErr := sub.sftp.Stat(remote)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return Entry{}, false, nil
		}
		return Entry{}, false, statErr
	}
	return Entry{
		Path:  remote,
		Name:  path.Base(remote),
		Size:  info.Size(),
		IsDir: info.IsDir(),
	}, true, nil
}

// MkdirAll creates a directory and its parents.
func (s *Session) MkdirAll(ctx context.Context, dir string) error {
	sub, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(sub)
	return sub.sftp.MkdirAll(dir)
}

/* -------------------------------------------------------------- transfer */

// Job is one file to move. Local is a path on this machine, Remote on the
// panel; direction decides which is the source.
type Job struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	Size   int64  `json:"size"`
	// Replace says something is already at Remote. Set by the caller, which
	// has listed the target directory anyway to decide about overwriting —
	// so it costs nothing here and saves two round trips per new file.
	Replace bool `json:"replace"`
}

// FileResult is what became of one job.
type FileResult struct {
	Job   Job    `json:"job"`
	Bytes int64  `json:"bytes"`
	Error string `json:"error,omitempty"`
}

// Progress is the running total, reported as bytes move.
type Progress struct {
	Done       int   `json:"done"`
	Total      int   `json:"total"`
	Bytes      int64 `json:"bytes"`
	TotalBytes int64 `json:"total_bytes"`
	// Current is what a stream is working on now, for the line under the bar.
	Current string `json:"current,omitempty"`
}

// Result is the whole transfer.
type Result struct {
	Files     []FileResult `json:"files"`
	Bytes     int64        `json:"bytes"`
	Failed    int          `json:"failed"`
	Cancelled bool         `json:"cancelled"`
	Seconds   float64      `json:"seconds"`
}

// counter adds to both totals as bytes pass: the file's, which the result
// reports, and the transfer's, which the progress bar reads. A single large
// file otherwise sat at zero until it finished.
type counter struct {
	file  *int64
	total *int64
}

func (c counter) Write(p []byte) (int, error) {
	n := int64(len(p))
	atomic.AddInt64(c.file, n)
	atomic.AddInt64(c.total, n)
	return len(p), nil
}

// Upload sends every job, several at a time.
//
// Directories are created before the workers start, so two streams cannot race
// to make the same one and one of them lose.
func (s *Session) Upload(ctx context.Context, jobs []Job, onProgress func(Progress)) Result {
	dirs := map[string]bool{}
	for _, job := range jobs {
		dirs[path.Dir(job.Remote)] = true
	}
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool {
		di := strings.Count(strings.Trim(ordered[i], "/"), "/")
		dj := strings.Count(strings.Trim(ordered[j], "/"), "/")
		if di != dj {
			return di < dj
		}
		return ordered[i] < ordered[j]
	})
	for _, dir := range ordered {
		if dir == "" || dir == "/" || dir == "." {
			continue
		}
		_ = s.MkdirAll(ctx, dir)
	}

	return s.each(ctx, jobs, onProgress, func(job Job, running *int64) (int64, error) {
		return s.sendFile(ctx, job, running)
	})
}

// Download pulls every job, several at a time, splitting the large ones across
// connections the same way an upload does.
func (s *Session) Download(ctx context.Context, jobs []Job, onProgress func(Progress)) Result {
	for _, job := range jobs {
		_ = os.MkdirAll(filepath.Dir(job.Local), 0o755)
	}

	return s.each(ctx, jobs, onProgress, func(job Job, running *int64) (int64, error) {
		return s.fetchFile(ctx, job, running)
	})
}

// each runs one transfer function across the job list, one job per stream.
func (s *Session) each(
	ctx context.Context,
	jobs []Job,
	onProgress func(Progress),
	move func(Job, *int64) (int64, error),
) Result {
	started := time.Now()
	result := Result{Files: make([]FileResult, len(jobs))}

	var total int64
	for _, job := range jobs {
		total += job.Size
	}

	var moved int64
	var done int64
	var current atomic.Value
	current.Store("")

	// Progress is reported on a timer rather than per chunk: a fast transfer
	// would otherwise spend its time marshalling events for a window that
	// repaints sixty times a second at best.
	report := func() {
		if onProgress == nil {
			return
		}
		name, _ := current.Load().(string)
		onProgress(Progress{
			Done:       int(atomic.LoadInt64(&done)),
			Total:      len(jobs),
			Bytes:      atomic.LoadInt64(&moved),
			TotalBytes: total,
			Current:    name,
		})
	}

	ticker := time.NewTicker(120 * time.Millisecond)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				report()
			case <-stop:
				return
			}
		}
	}()

	queue := make(chan int)
	var wg sync.WaitGroup

	workers := s.Streams()
	if workers > len(jobs) {
		workers = len(jobs)
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range queue {
				if ctx.Err() != nil {
					return
				}
				job := jobs[index]
				current.Store(job.Remote)

				bytesHere, moveErr := move(job, &moved)

				atomic.AddInt64(&done, 1)

				entry := FileResult{Job: job, Bytes: bytesHere}
				if moveErr != nil {
					entry.Error = moveErr.Error()
					// Its bytes went into the running total on the way past,
					// and none of them landed. Taking them back keeps the bar
					// honest rather than letting it overshoot.
					atomic.AddInt64(&moved, -bytesHere)
					entry.Bytes = 0
				}
				result.Files[index] = entry
			}
		}()
	}

	for i := range jobs {
		select {
		case queue <- i:
		case <-ctx.Done():
			result.Cancelled = true
		}
		if result.Cancelled {
			break
		}
	}
	close(queue)
	wg.Wait()

	ticker.Stop()
	close(stop)

	for _, file := range result.Files {
		result.Bytes += file.Bytes
		if file.Error != "" {
			result.Failed++
		}
	}
	if ctx.Err() != nil {
		result.Cancelled = true
	}
	result.Seconds = time.Since(started).Seconds()

	report()
	return result
}
