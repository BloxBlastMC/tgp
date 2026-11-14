package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
)

// Logs are stored in chunks, one blob per runner upload, indexed by
// job_log_chunks. A chunk holds one line per log line:
//
//	<step>\t<unix nanoseconds>\t<text>\n
const maxLogLineBytes = 64 << 10

func logChunkKey(jobID int64, seq int) string { return fmt.Sprintf("ci-logs/%d/%06d", jobID, seq) }
func logPrefix(jobID int64) string            { return fmt.Sprintf("ci-logs/%d", jobID) }

func cleanLogText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLogLineBytes {
		cut := maxLogLineBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + " … (line truncated)"
	}
	return strings.ToValidUTF8(s, "�")
}

// appendLogs stores lines from the runner. Lines the server already has
// (from a retried request) are skipped; a gap is a conflict.
func (s *Server) appendLogs(ctx context.Context, j *model.Job, req *runnerapi.LogRequest) (*runnerapi.LogResponse, error) {
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	resp := &runnerapi.LogResponse{Cancel: j.CancelRequested}
	next := j.LogLines + 1
	if j.LogTruncated {
		resp.Truncated, resp.Next = true, req.First+int64(len(req.Lines))
		return resp, nil
	}
	if req.First > next {
		return nil, apierr.Conflict("log lines out of order: expected line %d, got %d", next, req.First).WithDetails(map[string]any{"next": next})
	}
	lines := req.Lines
	if skip := next - req.First; skip > 0 {
		if skip >= int64(len(lines)) {
			resp.Next = next
			return resp, nil
		}
		lines = lines[skip:]
	}
	if len(lines) == 0 {
		resp.Next = next
		return resp, nil
	}
	maxBytes := int64(cs.MaxLogMB) << 20
	var buf bytes.Buffer
	count := 0
	truncated := false
	for _, l := range lines {
		text := cleanLogText(l.Text)
		line := strconv.Itoa(l.Step) + "\t" + strconv.FormatInt(l.T.UnixNano(), 10) + "\t" + text + "\n"
		if maxBytes > 0 && j.LogBytes+int64(buf.Len()+len(line)) > maxBytes {
			note := fmt.Sprintf("%d\t%d\t##[warning] The log reached the %d MB limit; further output is dropped.\n", l.Step, l.T.UnixNano(), cs.MaxLogMB)
			buf.WriteString(note)
			count++
			truncated = true
			break
		}
		buf.WriteString(line)
		count++
	}
	var chunks int
	if err := s.db.NewSelect().Model((*model.JobLogChunk)(nil)).ColumnExpr("COUNT(*)").Where("job_id = ?", j.ID).Scan(ctx, &chunks); err != nil {
		return nil, err
	}
	seq := chunks + 1
	size := int64(buf.Len())
	if err := s.store.Put(ctx, logChunkKey(j.ID, seq), &buf); err != nil {
		return nil, err
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*model.Job)(nil)).
			Set("log_lines = log_lines + ?, log_bytes = log_bytes + ?, log_truncated = ?", count, size, truncated).
			Where("id = ? AND log_lines = ?", j.ID, j.LogLines).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return apierr.Conflict("concurrent log upload; retry")
		}
		_, err = tx.NewInsert().Model(&model.JobLogChunk{JobID: j.ID, Seq: seq, FirstLine: next, LineCount: count, Bytes: size, CreatedAt: s.now()}).Exec(ctx)
		return err
	})
	if err != nil {
		_ = s.store.Delete(ctx, logChunkKey(j.ID, seq))
		return nil, err
	}
	j.LogLines += int64(count)
	j.LogBytes += size
	j.LogTruncated = truncated
	resp.Next = req.First + int64(len(req.Lines))
	resp.Truncated = truncated
	s.ci.logs.notify()
	return resp, nil
}

// logSource is the job whose stored logs belong to j (a reused job in a
// re-run shows the original's logs).
func logSource(j *model.Job) int64 {
	if j.ReusedFromID != nil {
		return *j.ReusedFromID
	}
	return j.ID
}

// readLogs returns up to limit lines numbered above after, optionally only
// one step's. next is the cursor for the following call.
func (s *Server) readLogs(ctx context.Context, j *model.Job, after int64, limit int, step *int) (lines []apigen.LogLine, next int64, err error) {
	src := logSource(j)
	var chunks []model.JobLogChunk
	if err := s.db.NewSelect().Model(&chunks).Where("job_id = ? AND first_line + line_count - 1 > ?", src, after).
		Order("seq").Scan(ctx); err != nil {
		return nil, after, err
	}
	lines = []apigen.LogLine{}
	next = after
	for _, c := range chunks {
		rc, err := s.store.Get(ctx, logChunkKey(src, c.Seq))
		if errors.Is(err, storage.ErrNotFound) {
			continue // expired
		}
		if err != nil {
			return nil, after, err
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes*2)
		n := c.FirstLine - 1
		for sc.Scan() {
			n++
			if n <= after {
				continue
			}
			parts := strings.SplitN(sc.Text(), "\t", 3)
			if len(parts) != 3 {
				continue
			}
			st, _ := strconv.Atoi(parts[0])
			ns, _ := strconv.ParseInt(parts[1], 10, 64)
			next = n
			if step != nil && st != *step {
				continue
			}
			lines = append(lines, apigen.LogLine{N: int(n), Step: st, T: time.Unix(0, ns).UTC(), Text: parts[2]})
			if len(lines) >= limit {
				rc.Close()
				return lines, next, nil
			}
		}
		rc.Close()
		if err := sc.Err(); err != nil {
			return nil, after, err
		}
	}
	return lines, next, nil
}

// logsComplete reports whether every line of a finished job has been read.
func logsComplete(j *model.Job, src *model.Job, next int64) bool {
	return j.Status == model.JobCompleted && next >= src.LogLines
}

// expireLogs deletes the logs of jobs that completed before cutoff.
func (s *Server) expireLogs(ctx context.Context, cutoff time.Time) error {
	var jobs []*model.Job
	if err := s.db.NewSelect().Model(&jobs).Column("id").
		Where("completed_at < ? AND NOT logs_deleted AND reused_from_id IS NULL", cutoff).Limit(200).Scan(ctx); err != nil {
		return err
	}
	for _, j := range jobs {
		if err := s.store.DeletePrefix(ctx, logPrefix(j.ID)); err != nil {
			return err
		}
		if _, err := s.db.NewDelete().Model((*model.JobLogChunk)(nil)).Where("job_id = ?", j.ID).Exec(ctx); err != nil {
			return err
		}
		if _, err := s.db.NewUpdate().Model((*model.Job)(nil)).Set("logs_deleted = TRUE").
			Where("id = ? OR reused_from_id = ?", j.ID, j.ID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// streamJobLogs serves GET /api/v1/repos/{repo}/jobs/{id}/logs/stream as
// server-sent events: "line" (a LogLine), "job" (the Job, when its status
// or steps change) and finally "end" (the Job) once it has finished and
// every line was sent.
func (s *Server) streamJobLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.checkAccess(ctx, "GetJobLogs", http.MethodGet); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		apierr.Write(w, apierr.BadRequest("invalid job id"))
		return
	}
	repo, perm, err := s.repoFor(ctx, chi.URLParam(r, "repo"), authz.Read)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	j, err := s.loadJob(ctx, repo, id)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > after {
			after = n
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		apierr.Write(w, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, id int64, v any) {
		b, _ := json.Marshal(v)
		if id > 0 {
			fmt.Fprintf(w, "id: %d\n", id)
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	}
	lastSig := ""
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		wake := s.ci.logs.wait()
		if err := s.db.NewSelect().Model(j).WherePK().Scan(ctx); err != nil {
			return
		}
		src := j
		if j.ReusedFromID != nil {
			src = new(model.Job)
			if err := s.db.NewSelect().Model(src).Where("id = ?", *j.ReusedFromID).Scan(ctx); err != nil {
				return
			}
		}
		for {
			lines, next, err := s.readLogs(ctx, j, after, 2000, nil)
			if err != nil {
				return
			}
			for _, l := range lines {
				send("line", int64(l.N), l)
			}
			after = next
			if len(lines) < 2000 {
				break
			}
		}
		view, err := s.jobView(ctx, repo, perm, j)
		if err != nil {
			return
		}
		sig := mustJSON([]any{view.Status, view.Conclusion, view.Steps, view.WaitReason, view.Runner})
		if logsComplete(j, src, after) {
			send("end", 0, view)
			flusher.Flush()
			return
		}
		if sig != lastSig {
			send("job", 0, view)
			lastSig = sig
		}
		flusher.Flush()
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(2 * time.Second): // other server nodes don't notify us
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
		}
	}
}
