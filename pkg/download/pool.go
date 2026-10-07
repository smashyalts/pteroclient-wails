package download

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// State is where a queued job has got to.
type State string

const (
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Status is a job's entry in the queue, safe to hand to a UI or a tool result.
type Status struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Dest  string `json:"dest"`
	State State  `json:"state"`

	Progress Progress `json:"progress"`
	Result   *Result  `json:"result,omitempty"`
	Error    string   `json:"error,omitempty"`

	QueuedAt  time.Time  `json:"queued_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Pool runs downloads in the background with a bounded number at once.
//
// Two layers of concurrency are at work and they are worth keeping apart. A
// job may split itself across connections when the host allows ranges, which
// is the Parts option. The pool's own limit is how many *files* are in flight.
// Against a stock node, where a single backup is stuck on one stream, the
// pool's limit is the only parallelism available, and pulling four backups at
// once is four times the throughput even though each one is serial.
type Pool struct {
	opts    Options
	limit   int
	onEvent func(Status)

	mu    sync.Mutex
	jobs  map[string]*entry
	order []string
	seq   uint64

	queue  chan string
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

type entry struct {
	status Status
	job    Job
	cancel context.CancelFunc
}

// NewPool starts a pool with the given number of concurrent transfers.
//
// onEvent, when set, is called on every state change and on progress, so a UI
// can render without polling. It is called from the worker goroutines, so it
// must not block for long.
func NewPool(ctx context.Context, concurrent int, opts Options, onEvent func(Status)) *Pool {
	if concurrent < 1 {
		concurrent = 1
	}
	opts.applyDefaults()

	poolCtx, cancel := context.WithCancel(ctx)
	p := &Pool{
		opts:    opts,
		limit:   concurrent,
		onEvent: onEvent,
		jobs:    make(map[string]*entry),
		// Buffered generously: Add should not block a UI thread just because
		// every worker is busy.
		queue:  make(chan string, 1024),
		ctx:    poolCtx,
		cancel: cancel,
	}

	for i := 0; i < concurrent; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// Add queues a job and returns its id.
func (p *Pool) Add(job Job) (string, error) {
	if job.Dest == "" {
		return "", errors.New("no destination given")
	}
	if job.Mint == nil {
		return "", errors.New("no way to mint a download URL")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return "", errors.New("the download queue is shut down")
	}
	// The same destination queued twice would have two writers on one part
	// file, interleaving bytes into something that cannot verify.
	for _, id := range p.order {
		existing := p.jobs[id]
		if existing.job.Dest != job.Dest {
			continue
		}
		if existing.status.State == StateQueued || existing.status.State == StateRunning {
			p.mu.Unlock()
			return existing.status.ID, fmt.Errorf(
				"%s is already %s as job %s", job.Dest, existing.status.State, existing.status.ID)
		}
	}

	p.seq++
	id := fmt.Sprintf("dl-%d", p.seq)
	e := &entry{
		job: job,
		status: Status{
			ID: id, Name: job.Name, Dest: job.Dest,
			State: StateQueued, QueuedAt: time.Now(),
			Progress: Progress{Name: job.Name, Dest: job.Dest, Total: job.Size},
		},
	}
	p.jobs[id] = e
	p.order = append(p.order, id)
	snapshot := e.status
	p.mu.Unlock()

	p.emit(snapshot)

	select {
	case p.queue <- id:
		return id, nil
	default:
		p.setFailed(id, errors.New("the download queue is full"))
		return id, errors.New("the download queue is full")
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case id, ok := <-p.queue:
			if !ok {
				return
			}
			p.run(id)
		}
	}
}

func (p *Pool) run(id string) {
	p.mu.Lock()
	e, found := p.jobs[id]
	if !found || e.status.State != StateQueued {
		// Cancelled while it sat in the queue.
		p.mu.Unlock()
		return
	}

	jobCtx, cancel := context.WithCancel(p.ctx)
	e.cancel = cancel
	now := time.Now()
	e.status.State = StateRunning
	e.status.StartedAt = &now
	job := e.job
	snapshot := e.status
	p.mu.Unlock()

	defer cancel()
	p.emit(snapshot)

	opts := p.opts
	// Thread per-job progress back into the job's own status, so the pool
	// reports one coherent picture rather than the caller having to stitch
	// callbacks to ids.
	opts.Progress = func(pr Progress) {
		p.mu.Lock()
		e.status.Progress = pr
		snap := e.status
		p.mu.Unlock()
		p.emit(snap)
	}

	result, err := Fetch(jobCtx, job, opts)

	ended := time.Now()
	p.mu.Lock()
	e.status.EndedAt = &ended
	switch {
	case err == nil:
		e.status.State = StateDone
		e.status.Result = result
		e.status.Error = ""
		if result != nil {
			e.status.Progress.Phase = PhaseDone
			e.status.Progress.Done = result.Bytes
			if e.status.Progress.Total == 0 {
				e.status.Progress.Total = result.Bytes
			}
		}
	case errors.Is(err, context.Canceled):
		// Distinguish the operator stopping it from the node failing. A
		// cancelled job's part file is kept, so it can be queued again.
		e.status.State = StateCancelled
		e.status.Error = "cancelled"
	default:
		e.status.State = StateFailed
		e.status.Error = err.Error()
	}
	snapshot = e.status
	p.mu.Unlock()

	p.emit(snapshot)
}

func (p *Pool) setFailed(id string, cause error) {
	p.mu.Lock()
	e, found := p.jobs[id]
	if !found {
		p.mu.Unlock()
		return
	}
	now := time.Now()
	e.status.State = StateFailed
	e.status.Error = cause.Error()
	e.status.EndedAt = &now
	snapshot := e.status
	p.mu.Unlock()
	p.emit(snapshot)
}

// Status returns one job.
func (p *Pool) Status(id string) (Status, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, found := p.jobs[id]
	if !found {
		return Status{}, false
	}
	return e.status, true
}

// List returns every job, newest first.
func (p *Pool) List() []Status {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]Status, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, p.jobs[id].status)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].QueuedAt.After(out[j].QueuedAt)
	})
	return out
}

// Cancel stops a job. A running transfer keeps its part file, so queueing the
// same destination again resumes where ranges allow it.
func (p *Pool) Cancel(id string) bool {
	p.mu.Lock()
	e, found := p.jobs[id]
	if !found {
		p.mu.Unlock()
		return false
	}

	switch e.status.State {
	case StateQueued:
		now := time.Now()
		e.status.State = StateCancelled
		e.status.Error = "cancelled before it started"
		e.status.EndedAt = &now
		snapshot := e.status
		p.mu.Unlock()
		p.emit(snapshot)
		return true
	case StateRunning:
		cancel := e.cancel
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return true
	default:
		p.mu.Unlock()
		return false // already finished
	}
}

// Forget drops finished jobs from the listing, so a long-lived process does
// not grow a history without bound.
func (p *Pool) Forget() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	kept := make([]string, 0, len(p.order))
	var dropped int
	for _, id := range p.order {
		switch p.jobs[id].status.State {
		case StateQueued, StateRunning:
			kept = append(kept, id)
		default:
			delete(p.jobs, id)
			dropped++
		}
	}
	p.order = kept
	return dropped
}

// Active reports how many jobs are queued or running.
func (p *Pool) Active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var n int
	for _, id := range p.order {
		if s := p.jobs[id].status.State; s == StateQueued || s == StateRunning {
			n++
		}
	}
	return n
}

// Close stops the workers and waits for the running transfers to notice.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()
}

func (p *Pool) emit(s Status) {
	if p.onEvent != nil {
		p.onEvent(s)
	}
}
