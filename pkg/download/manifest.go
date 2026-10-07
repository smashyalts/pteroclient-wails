package download

import (
	"encoding/json"
	"os"
	"sync"
)

// manifest is the record on disk of what a ranged transfer has finished, so a
// later run can skip it.
//
// It only earns its keep where the host honours ranges. Against a stock node
// nothing can be resumed, and the manifest exists then only to say that an
// attempt happened. It is written next to the part file and removed when the
// download succeeds.
type manifest struct {
	mu sync.Mutex

	Name  string `json:"name"`
	Total int64  `json:"total"`

	// Validator is the panel's checksum. If it changes, the file on the node
	// is not the file this manifest describes, and the finished parts are
	// about to be stitched into something that will not verify, so the whole
	// thing starts again.
	Validator string `json:"validator,omitempty"`

	Parts []partState `json:"parts"`
}

type partState struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	// Got is how many bytes of this part are on disk. A part that stopped
	// halfway continues from here rather than from its start.
	Got  int64 `json:"got"`
	Done bool  `json:"done"`
}

// loadManifest reads the manifest beside a job's destination, returning an
// empty one when there is nothing usable there.
func loadManifest(path string, job Job) *manifest {
	fresh := &manifest{Name: job.Name, Validator: job.Checksum}

	data, err := os.ReadFile(path)
	if err != nil {
		return fresh
	}

	var stored manifest
	if err := json.Unmarshal(data, &stored); err != nil {
		return fresh
	}
	// A manifest for a different file, or for a version of it that has since
	// changed, is worse than none: it would claim bytes are present that are
	// not the bytes we want.
	if job.Checksum != "" && stored.Validator != job.Checksum {
		return fresh
	}
	if job.Size > 0 && stored.Total > 0 && stored.Total != job.Size {
		return fresh
	}

	stored.Name = job.Name
	stored.Validator = job.Checksum
	return &stored
}

// adopt reconciles the manifest with a part plan.
//
// The plan depends on the file size and the configured part count, so a run
// that changed either produces different boundaries. Rather than try to
// salvage overlapping ranges, a changed plan discards the old progress: the
// bytes are still on the node, and a wrong offset would corrupt the archive
// silently.
func (m *manifest) adopt(plan []Part, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Total = total

	matches := len(m.Parts) == len(plan)
	if matches {
		for i, p := range plan {
			if m.Parts[i].Start != p.Start || m.Parts[i].End != p.End {
				matches = false
				break
			}
		}
	}
	if matches {
		return
	}

	m.Parts = make([]partState, len(plan))
	for i, p := range plan {
		m.Parts[i] = partState{Start: p.Start, End: p.End}
	}
}

func (m *manifest) isDone(index int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return index < len(m.Parts) && m.Parts[index].Done
}

// progress reports how far into a part the bytes on disk reach.
func (m *manifest) progress(index int) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= len(m.Parts) {
		return 0
	}
	return m.Parts[index].Got
}

func (m *manifest) advance(index int, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < len(m.Parts) {
		m.Parts[index].Got += n
	}
}

func (m *manifest) complete(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < len(m.Parts) {
		m.Parts[index].Done = true
		m.Parts[index].Got = m.Parts[index].End - m.Parts[index].Start + 1
	}
}

// reset forgets a part, for when its bytes turned out not to be trustworthy.
func (m *manifest) reset(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < len(m.Parts) {
		m.Parts[index].Got = 0
		m.Parts[index].Done = false
	}
}

func (m *manifest) doneBytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, p := range m.Parts {
		total += p.Got
	}
	return total
}

func (m *manifest) remaining() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var left int
	for _, p := range m.Parts {
		if !p.Done {
			left++
		}
	}
	return left
}

// save writes the manifest out.
//
// Through a temporary file and a rename, because the process can die at any
// moment and a half-written manifest would be read back as a plan to write
// bytes at the wrong offsets. A failure here is not worth failing the
// download over — it costs a resume, not the file.
func (m *manifest) save(path string) {
	m.mu.Lock()
	data, err := json.Marshal(m)
	m.mu.Unlock()
	if err != nil {
		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
