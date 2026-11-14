package runner

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// masker replaces secret values in log lines with ***.
type masker struct {
	mu     sync.RWMutex
	values []string // longest first
}

func (m *masker) add(v string) {
	for _, part := range strings.Split(strings.ReplaceAll(v, "\r\n", "\n"), "\n") {
		part = strings.TrimSpace(part)
		if len(part) < 3 { // masking 1-2 character values would mangle every log
			continue
		}
		m.mu.Lock()
		dup := false
		for _, x := range m.values {
			if x == part {
				dup = true
				break
			}
		}
		if !dup {
			m.values = append(m.values, part)
			sort.Slice(m.values, func(i, k int) bool { return len(m.values[i]) > len(m.values[k]) })
		}
		m.mu.Unlock()
	}
}

func (m *masker) mask(s string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, v := range m.values {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

// logStream buffers log lines and uploads them in order, at least once a
// second. Uploads are retried; the server deduplicates by line number.
type logStream struct {
	client *Client
	job    int64
	mask   *masker
	// onCancel is called when the server says the job should stop.
	onCancel func()

	mu        sync.Mutex
	pending   []runnerapi.LogLine
	first     int64 // number of pending[0]
	truncated bool
	kick      chan struct{}
	done      chan struct{}
	closed    bool
	gone      bool // the server no longer accepts this job
	wg        sync.WaitGroup
}

func newLogStream(c *Client, job int64, m *masker, onCancel func()) *logStream {
	ls := &logStream{client: c, job: job, mask: m, onCancel: onCancel, first: 1, kick: make(chan struct{}, 1), done: make(chan struct{})}
	ls.wg.Add(1)
	go ls.loop()
	return ls
}

// Line adds a line for step (masking secrets and handling ::add-mask::).
func (ls *logStream) Line(step int, text string) {
	if v, ok := strings.CutPrefix(strings.TrimSpace(text), "::add-mask::"); ok {
		ls.mask.add(v)
		text = "::add-mask::***"
	}
	text = ls.mask.mask(text)
	ls.mu.Lock()
	if ls.truncated || ls.closed {
		ls.mu.Unlock()
		return
	}
	ls.pending = append(ls.pending, runnerapi.LogLine{Step: step, T: time.Now().UTC(), Text: text})
	n := len(ls.pending)
	ls.mu.Unlock()
	if n >= 1000 {
		ls.poke()
	}
}

func (ls *logStream) poke() {
	select {
	case ls.kick <- struct{}{}:
	default:
	}
}

func (ls *logStream) loop() {
	defer ls.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ls.done:
			ls.flush(true)
			return
		case <-t.C:
		case <-ls.kick:
		}
		ls.flush(false)
	}
}

// flush uploads pending lines in batches.
func (ls *logStream) flush(bool) {
	for {
		ls.mu.Lock()
		if len(ls.pending) == 0 || ls.gone {
			ls.mu.Unlock()
			return
		}
		batch := ls.pending
		if len(batch) > 2000 {
			batch = batch[:2000]
		}
		first := ls.first
		ls.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var resp *runnerapi.LogResponse
		err := retry(ctx, func() error {
			var err error
			resp, err = ls.client.Logs(ctx, ls.job, runnerapi.LogRequest{First: first, Lines: batch})
			return err
		})
		cancel()
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 409 && ae.Details != nil {
			// The server is missing earlier lines (a lost upload): continue
			// numbering from where it is.
			if next, ok := ae.Details["next"].(float64); ok && int64(next) < first {
				ls.mu.Lock()
				ls.first = int64(next)
				ls.mu.Unlock()
				continue
			}
		}
		if errors.As(err, &ae) && ae.Gone() {
			ls.mu.Lock()
			ls.gone = true
			ls.mu.Unlock()
			if ls.onCancel != nil {
				ls.onCancel()
			}
			return
		}
		if err != nil {
			return // keep the lines; the next tick (or Close) tries again
		}
		ls.mu.Lock()
		ls.pending = ls.pending[len(batch):]
		ls.first += int64(len(batch))
		if resp != nil && resp.Truncated {
			ls.truncated = true
			ls.pending = nil
		}
		ls.mu.Unlock()
		if resp != nil && resp.Cancel && ls.onCancel != nil {
			ls.onCancel()
		}
	}
}

// Close flushes everything and stops the uploader.
func (ls *logStream) Close() {
	ls.mu.Lock()
	if ls.closed {
		ls.mu.Unlock()
		return
	}
	ls.closed = true
	ls.mu.Unlock()
	close(ls.done)
	ls.wg.Wait()
}

// lineWriter turns a byte stream into log lines. A carriage return without
// a newline (progress bars) keeps only the text after it, like a terminal.
type lineWriter struct {
	step int
	out  *logStream
	buf  []byte
	mu   sync.Mutex
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 64<<10 { // a very long line without a newline
		w.emit(string(w.buf))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

func (w *lineWriter) emit(line string) {
	line = strings.TrimSuffix(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	w.out.Line(w.step, line)
}

// Flush emits a trailing partial line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = w.buf[:0]
	}
}
