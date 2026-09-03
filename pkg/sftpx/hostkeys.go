package sftpx

// Host key trust.
//
// A panel's SFTP host is not in anybody's known_hosts, and there is no
// certificate authority to fall back on. The honest options are to check
// nothing — which makes a file transfer tool trivially interceptable — or to
// trust the key the first time and notice if it ever changes. This does the
// second, and says so.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
)

// KnownHosts remembers one fingerprint per host:port.
type KnownHosts struct {
	mu    sync.Mutex
	path  string
	known map[string]string
}

// ErrHostKeyChanged is returned when a host answers with a different key than
// the one that was accepted before. It is deliberately not a prompt: a changed
// key is either a rebuilt node or somebody in the middle, and the difference is
// not something this can work out.
type ErrHostKeyChanged struct {
	Host        string
	Fingerprint string
	Expected    string
}

func (e *ErrHostKeyChanged) Error() string {
	return fmt.Sprintf("the SSH host key for %s has changed.\nexpected %s\ngot      %s\n\n"+
		"That is either the node being rebuilt or something sitting between you and it. "+
		"Nothing is transferred until this is resolved.", e.Host, e.Expected, e.Fingerprint)
}

// ErrHostKeyUnknown is returned the first time a host is seen. The caller shows
// the fingerprint and calls Trust if the user accepts it.
type ErrHostKeyUnknown struct {
	Host        string
	Fingerprint string
}

func (e *ErrHostKeyUnknown) Error() string {
	return fmt.Sprintf("%s has not been connected to before (%s)", e.Host, e.Fingerprint)
}

// NewKnownHosts opens the store under the app's own directory.
func NewKnownHosts(root string) (*KnownHosts, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("sftpx: create %s: %w", root, err)
	}
	kh := &KnownHosts{
		path:  filepath.Join(root, "sftp-hosts.json"),
		known: map[string]string{},
	}
	kh.load()
	return kh, nil
}

func (k *KnownHosts) load() {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return
	}
	parsed := map[string]string{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// A file that cannot be read is treated as no file: every host is then
		// unknown and asked about again, which is the safe direction.
		return
	}
	k.known = parsed
}

func (k *KnownHosts) save() error {
	raw, err := json.MarshalIndent(k.known, "", "  ")
	if err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	// Windows will not always rename onto an existing file.
	if err := os.Rename(tmp, k.path); err != nil {
		_ = os.Remove(k.path)
		return os.Rename(tmp, k.path)
	}
	return nil
}

// Fingerprint is the SHA-256 form OpenSSH prints.
func Fingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Trust records a fingerprint the user has accepted.
func (k *KnownHosts) Trust(host, fingerprint string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.known[host] = fingerprint
	return k.save()
}

// Forget drops a host, so the next connection asks again.
func (k *KnownHosts) Forget(host string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.known, host)
	return k.save()
}

// Fingerprints returns what is trusted, for the UI to show.
func (k *KnownHosts) Fingerprints() map[string]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make(map[string]string, len(k.known))
	for host, fp := range k.known {
		out[host] = fp
	}
	return out
}

// callback builds the ssh.HostKeyCallback for one connection.
//
// accept is what the user has already agreed to for this attempt, if anything:
// the first connection fails with ErrHostKeyUnknown carrying the fingerprint,
// and the caller retries with that fingerprint once it has been shown and
// accepted. Nothing is trusted without a person seeing it.
func (k *KnownHosts) callback(accept string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := Fingerprint(key)

		k.mu.Lock()
		want, seen := k.known[hostname]
		k.mu.Unlock()

		if seen {
			if want == got {
				return nil
			}
			return &ErrHostKeyChanged{Host: hostname, Fingerprint: got, Expected: want}
		}

		if accept != "" && accept == got {
			k.mu.Lock()
			k.known[hostname] = got
			err := k.save()
			k.mu.Unlock()
			return err
		}

		return &ErrHostKeyUnknown{Host: hostname, Fingerprint: got}
	}
}
