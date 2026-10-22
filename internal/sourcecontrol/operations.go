package sourcecontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

const operationRetention = 5 * time.Minute

type operationRecord struct {
	operation Operation
	cancel    context.CancelFunc
	finished  time.Time
}

func cloneSnapshot(snap *Snapshot) *Snapshot {
	if snap == nil {
		return nil
	}
	out := *snap
	out.Entries = append([]Entry{}, snap.Entries...)
	out.Remotes = append([]Remote{}, snap.Remotes...)
	return &out
}
func cloneOperation(op Operation) *Operation {
	op.AfterSnapshot = cloneSnapshot(op.AfterSnapshot)
	return &op
}
func (s *Service) purgeOperationsLocked() {
	now := time.Now()
	for id, r := range s.operations {
		if !r.finished.IsZero() && now.Sub(r.finished) >= operationRetention {
			delete(s.operations, id)
		}
	}
}
func (s *Service) eventLocked(r *operationRecord, stream, chunk string) OperationEvent {
	s.sequence++
	r.operation.Sequence = s.sequence
	return OperationEvent{OperationID: r.operation.ID, Repository: r.operation.Repository, Sequence: s.sequence, State: r.operation.State, Stream: stream, Chunk: chunk}
}
func (s *Service) publish(event OperationEvent) {
	if s.emit != nil {
		s.emit(event)
	}
}
func (s *Service) GetOperation(id string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeOperationsLocked()
	if r := s.operations[id]; r != nil {
		return Result{Code: "ok", Operation: cloneOperation(r.operation)}
	}
	return issue("operation_unknown", errors.New("Operation is no longer available"))
}
func (s *Service) CancelOperation(id string) Result {
	s.mu.Lock()
	s.purgeOperationsLocked()
	r := s.operations[id]
	if r == nil {
		s.mu.Unlock()
		return issue("operation_unknown", errors.New("Operation is no longer available"))
	}
	var event OperationEvent
	if r.finished.IsZero() && r.operation.State != "cancelling" {
		r.operation.State = "cancelling"
		r.cancel()
		event = s.eventLocked(r, "", "")
	}
	result := Result{Code: "ok", Operation: cloneOperation(r.operation)}
	s.mu.Unlock()
	if event.OperationID != "" {
		s.publish(event)
	}
	return result
}
func (s *Service) Busy() BusyState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeOperationsLocked()
	result := BusyState{OperationIDs: []string{}}
	for _, id := range s.active {
		result.OperationIDs = append(result.OperationIDs, id)
	}
	sort.Strings(result.OperationIDs)
	result.Busy = len(result.OperationIDs) > 0
	return result
}
func commonKey(a *authority) string { return digest(a.commonID.CanonicalPath, a.commonID.PhysicalID) }
func (s *Service) snapshotBusy(a *authority, snap *Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap.OperationID = s.active[commonKey(a)]
	snap.Busy = snap.OperationID != ""
}

// Discovery admits a concrete physical repository; the worker owns its retained
// handles and revalidates the immutable request immediately before dispatch.
func (s *Service) startOperation(ctx context.Context, ref RepositoryRef, kind string, timeout time.Duration, work func(context.Context, *authority, func(string, string)) (CommandResult, Result)) Result {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	a, problem := s.discover(ctx, ref.Context, &ref)
	if problem.Code != "" {
		return problem
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		a.close()
		return issue("unavailable", err)
	}
	id := hex.EncodeToString(token)
	key := commonKey(a)
	s.mu.Lock()
	s.purgeOperationsLocked()
	if s.closed || s.shutdownReserved || s.lifetime.Err() != nil {
		s.mu.Unlock()
		a.close()
		return issue("shutting_down", errors.New("Source control is closing"))
	}
	if s.active[key] != "" {
		s.mu.Unlock()
		a.close()
		busy := s.Busy()
		return Result{Code: "busy", Message: "Repository has an active operation", Busy: &busy}
	}
	jobctx, jobcancel := context.WithTimeout(s.lifetime, timeout)
	record := &operationRecord{operation: Operation{ID: id, Repository: a.ref, Kind: kind, State: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, cancel: jobcancel}
	s.operations[id] = record
	s.active[key] = id
	s.jobs.Add(1)
	event := s.eventLocked(record, "", "")
	result := Result{Code: "ok", Operation: cloneOperation(record.operation)}
	s.mu.Unlock()
	s.publish(event)
	go func() {
		defer s.jobs.Done()
		defer jobcancel()
		native, problem := work(jobctx, a, func(stream, chunk string) { s.operationOutput(id, stream, chunk) })
		// A cancelled command can have changed the index. Recovery has its own
		// deadline, and keeps the common-directory reservation until it drains.
		recoveryctx, recoverycancel := context.WithTimeout(s.lifetime, queryTimeout)
		after := s.status(recoveryctx, a)
		recoverycancel()
		a.close()
		s.mu.Lock()
		op := &record.operation
		op.HasExitCode, op.ExitCode = native.HasExitCode, native.ExitCode
		op.OutputTruncated = op.OutputTruncated || native.ConsoleTruncated
		op.State = "succeeded"
		op.ResultCode = "ok"
		if problem.Code != "" {
			op.State = "failed"
			op.ResultCode = problem.Code
			op.Message = problem.Message
		}
		if native.Cancelled || errors.Is(jobctx.Err(), context.Canceled) {
			op.State = "cancelled"
			op.ResultCode = "git_error"
		}
		if problem.Operation != nil {
			op.Remote, op.RemoteBranch = problem.Operation.Remote, problem.Operation.RemoteBranch
			if kind == "push" && (native.Cancelled || native.TimedOut || jobctx.Err() != nil) {
				op.RemoteUncertain = true
				op.State, op.ResultCode = "uncertain", "git_error"
			}
		}
		if after.Code == "ok" {
			if problem.Snapshot != nil {
				op.MutationObserved = after.Snapshot.IndexRevision != problem.Snapshot.IndexRevision || after.Snapshot.HeadOID != problem.Snapshot.HeadOID
			}
			if kind == "commit" && problem.Snapshot != nil && after.Snapshot.HeadOID != problem.Snapshot.HeadOID {
				op.CommitOID = after.Snapshot.HeadOID
			}
			op.AfterSnapshot = cloneSnapshot(after.Snapshot)
			op.AfterSnapshot.Busy = false
			op.AfterSnapshot.OperationID = ""
		} else if op.State == "succeeded" {
			op.State = "failed"
			op.ResultCode = after.Code
			op.Message = after.Message
		}
		record.finished = time.Now().UTC()
		op.FinishedAt = record.finished.Format(time.RFC3339Nano)
		delete(s.active, key)
		event := s.eventLocked(record, "", "")
		s.mu.Unlock()
		s.publish(event)
	}()
	return result
}
func (s *Service) operationOutput(id, stream, chunk string) {
	s.mu.Lock()
	r := s.operations[id]
	chunk = redact(chunk)
	remaining := consoleLimit - len(r.operation.Output)
	if len(chunk) > remaining {
		chunk = chunk[:remaining]
		r.operation.OutputTruncated = true
	}
	if chunk == "" {
		s.mu.Unlock()
		return
	}
	r.operation.Output += chunk
	event := s.eventLocked(r, stream, chunk)
	s.mu.Unlock()
	s.publish(event)
}

// Admission and shutdown reservation share mu, including in-flight discovery.
func (s *Service) TryBeginShutdown() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.shutdownReserved {
		return nil, errors.New("Source control is closing")
	}
	if len(s.active) != 0 {
		return nil, errors.New("Source control operation is still running")
	}
	s.shutdownReserved = true
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.closed {
				s.shutdownReserved = false
			}
		})
	}, nil
}

// Close belongs only to true application shutdown, never panel/tray changes.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.stop()
	s.mu.Unlock()
	s.jobs.Wait()
}
