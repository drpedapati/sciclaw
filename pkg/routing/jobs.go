package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type JobState string

const (
	JobStateQueued      JobState = "queued"
	JobStateRunning     JobState = "running"
	JobStateDone        JobState = "done"
	JobStateFailed      JobState = "failed"
	JobStateCancelled   JobState = "cancelled"
	JobStateInterrupted JobState = "interrupted"
)

type ProgressMessenger interface {
	SendOrEditProgress(ctx context.Context, channelName, chatID, messageID string, msg bus.OutboundMessage) (string, error)
}

type handlerResolverFunc func(target LoopTarget) (JobRunner, error)

type JobClass string

const (
	JobClassWrite JobClass = "write"
	JobClassBTW   JobClass = "btw"
	// JobClassExternalReadOnly is a legacy alias for pre-/btw scheduler
	// naming. New code should use JobClassBTW.
	JobClassExternalReadOnly JobClass = JobClassBTW
)

type JobRecord struct {
	ID              string             `json:"id"`
	ShortID         string             `json:"short_id"`
	Channel         string             `json:"channel"`
	ChatID          string             `json:"chat_id"`
	Workspace       string             `json:"workspace"`
	RuntimeKey      string             `json:"runtime_key"`
	TargetKey       string             `json:"target_key"`
	Class           JobClass           `json:"class"`
	State           JobState           `json:"state"`
	Phase           string             `json:"phase,omitempty"`
	Detail          string             `json:"detail,omitempty"`
	StatusMessageID string             `json:"status_message_id,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	AskSummary      string             `json:"ask_summary,omitempty"`
	LastError       string             `json:"last_error,omitempty"`
	Runtime         RuntimeProfile     `json:"runtime,omitempty"`
	Message         bus.InboundMessage `json:"message,omitempty"`
	StartedAt       int64              `json:"started_at"`
	UpdatedAt       int64              `json:"updated_at"`
}

type jobStore struct {
	path string
	mu   sync.Mutex
	jobs map[string]JobRecord
}

type activeJob struct {
	mu     sync.RWMutex
	record JobRecord
	cancel context.CancelFunc
	ctx    context.Context
	target LoopTarget
	msg    bus.InboundMessage
}

type targetActiveJobs struct {
	write        *activeJob
	btw          *activeJob
	queuedWrites []*activeJob
}

func (j *activeJob) snapshot() JobRecord {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.record
}

func (j *activeJob) update(mutator func(*JobRecord)) JobRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	mutator(&j.record)
	return j.record
}

func (s *targetActiveJobs) set(class JobClass, job *activeJob) {
	switch class {
	case JobClassBTW:
		s.btw = job
	default:
		s.write = job
	}
}

func (s *targetActiveJobs) clear(class JobClass) {
	switch class {
	case JobClassBTW:
		s.btw = nil
	default:
		s.write = nil
	}
}

func (s *targetActiveJobs) jobForClass(class JobClass) *activeJob {
	switch class {
	case JobClassBTW:
		return s.btw
	default:
		return s.write
	}
}

func (s *targetActiveJobs) empty() bool {
	return s == nil || (s.write == nil && s.btw == nil && len(s.queuedWrites) == 0)
}

func (s *targetActiveJobs) isFull() bool {
	return s != nil && s.write != nil && s.btw != nil
}

func (s *targetActiveJobs) snapshot() []*activeJob {
	if s == nil {
		return nil
	}
	out := make([]*activeJob, 0, 2)
	if s.write != nil {
		out = append(out, s.write)
	}
	if s.btw != nil {
		out = append(out, s.btw)
	}
	return out
}

func (s *targetActiveJobs) snapshotAll() []*activeJob {
	out := s.snapshot()
	if s == nil || len(s.queuedWrites) == 0 {
		return out
	}
	out = append(out, s.queuedWrites...)
	return out
}

func (s *targetActiveJobs) enqueueWrite(job *activeJob) int {
	if s == nil || job == nil {
		return 0
	}
	s.queuedWrites = append(s.queuedWrites, job)
	return len(s.queuedWrites)
}

func (s *targetActiveJobs) popQueuedWrite() *activeJob {
	if s == nil || len(s.queuedWrites) == 0 {
		return nil
	}
	job := s.queuedWrites[0]
	s.queuedWrites = s.queuedWrites[1:]
	return job
}

func (s *targetActiveJobs) removeQueuedWrite(jobID string) *activeJob {
	if s == nil || strings.TrimSpace(jobID) == "" {
		return nil
	}
	for i, job := range s.queuedWrites {
		if job == nil {
			continue
		}
		record := job.snapshot()
		if record.ID != jobID {
			continue
		}
		s.queuedWrites = append(s.queuedWrites[:i], s.queuedWrites[i+1:]...)
		return job
	}
	return nil
}

func (s *targetActiveJobs) moveQueuedWriteToFront(jobID string) *activeJob {
	if s == nil || strings.TrimSpace(jobID) == "" || len(s.queuedWrites) == 0 {
		return nil
	}
	for i, job := range s.queuedWrites {
		if job == nil {
			continue
		}
		record := job.snapshot()
		if record.ID != jobID {
			continue
		}
		if i == 0 {
			return job
		}
		s.queuedWrites = append([]*activeJob{job}, append(s.queuedWrites[:i], s.queuedWrites[i+1:]...)...)
		return job
	}
	return nil
}

type JobManager struct {
	bus                 *bus.MessageBus
	progress            ProgressMessenger
	resolve             handlerResolverFunc
	resolveSideLane     handlerResolverFunc
	progressInterval    time.Duration
	store               *jobStore
	semaphore           chan struct{}
	allowBTWDuringWrite bool

	mu      sync.Mutex
	active  map[string]*targetActiveJobs
	counter uint64
}

type parsedJobRequest struct {
	Class   JobClass
	Message bus.InboundMessage
}

func formatShortJobRef(n uint64) string {
	const width = 5
	ref := strings.ToUpper(strconv.FormatUint(n, 36))
	if len(ref) >= width {
		return ref[len(ref)-width:]
	}
	return strings.Repeat("0", width-len(ref)) + ref
}

func NewJobManager(storePath string, cfg config.JobsConfig, msgBus *bus.MessageBus, progress ProgressMessenger, resolve handlerResolverFunc) (*JobManager, error) {
	if strings.TrimSpace(storePath) == "" {
		return nil, fmt.Errorf("job store path is required")
	}
	if resolve == nil {
		return nil, fmt.Errorf("job handler resolver is required")
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	progressEvery := time.Duration(cfg.ProgressUpdateSeconds) * time.Second
	if progressEvery <= 0 {
		progressEvery = 5 * time.Second
	}
	store, err := newJobStore(storePath)
	if err != nil {
		return nil, err
	}
	jm := &JobManager{
		bus:                 msgBus,
		progress:            progress,
		resolve:             resolve,
		progressInterval:    progressEvery,
		store:               store,
		semaphore:           make(chan struct{}, maxConcurrent),
		allowBTWDuringWrite: cfg.AllowBTWDuringWrite || cfg.AllowReadOnlyDuringWrite,
		active:              map[string]*targetActiveJobs{},
	}
	if err := jm.restoreCounterFromStore(); err != nil {
		return nil, err
	}
	if err := jm.interruptStaleActiveJobs(); err != nil {
		return nil, err
	}
	if err := jm.rehydrateQueuedJobs(); err != nil {
		return nil, err
	}
	return jm, nil
}

func (jm *JobManager) SetSideLaneResolver(resolve handlerResolverFunc) {
	jm.mu.Lock()
	jm.resolveSideLane = resolve
	jm.mu.Unlock()
}

// SetExternalReadOnlyResolver is a legacy alias for pre-/btw scheduler
// naming. New call sites should use SetSideLaneResolver.
func (jm *JobManager) SetExternalReadOnlyResolver(resolve handlerResolverFunc) {
	jm.SetSideLaneResolver(resolve)
}

func (jm *JobManager) ShouldHandleChannel(channel string) bool {
	return strings.EqualFold(strings.TrimSpace(channel), "discord")
}

func (jm *JobManager) resolveForClass(class JobClass, target LoopTarget) (JobRunner, error) {
	if class == JobClassBTW {
		jm.mu.Lock()
		resolve := jm.resolveSideLane
		jm.mu.Unlock()
		if resolve == nil {
			return nil, fmt.Errorf("/btw job runner is not configured")
		}
		return resolve(target)
	}
	return jm.resolve(target)
}

func (jm *JobManager) Submit(ctx context.Context, target LoopTarget, msg bus.InboundMessage) error {
	target = target.normalized()
	targetKey := target.key()
	control := parseJobControl(msg)
	request := classifyJobRequest(msg)
	jobClass := request.Class

	jm.mu.Lock()
	activeSet := jm.active[targetKey]
	knownJobs := activeSet.snapshotAll()
	if control.Kind != jobControlNone && (control.Directed || replyTargetsTrackedJob(knownJobs, msg.Metadata["reply_message_id"])) {
		jm.mu.Unlock()
		return jm.handleControl(ctx, msg, targetKey, knownJobs, control)
	}
	if activeSet == nil {
		activeSet = &targetActiveJobs{}
		jm.active[targetKey] = activeSet
	}
	var recoveredWrite *activeJob
	if jobClass == JobClassWrite && activeSet.write == nil && len(activeSet.queuedWrites) > 0 {
		recoveredWrite = activeSet.popQueuedWrite()
		activeSet.write = recoveredWrite
	}

	jobID, shortID := jm.nextJobID()
	jobCtx, cancel := context.WithCancel(ctx)
	record := JobRecord{
		ID:         jobID,
		ShortID:    shortID,
		Channel:    msg.Channel,
		ChatID:     msg.ChatID,
		Workspace:  target.Workspace,
		RuntimeKey: target.Runtime.Key(),
		TargetKey:  targetKey,
		Class:      jobClass,
		State:      JobStateQueued,
		Phase:      "queued",
		Detail:     "Queued",
		AskSummary: summarizeJobAsk(request.Message.Content),
		Runtime:    target.Runtime,
		Message:    request.Message,
		StartedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	active := &activeJob{
		record: record,
		cancel: cancel,
		ctx:    jobCtx,
		target: target,
		msg:    request.Message,
	}
	var (
		startImmediately bool
		queuedPosition   int
		activeWrite      JobRecord
	)
	switch jobClass {
	case JobClassWrite:
		if activeSet.write != nil {
			activeWrite = activeSet.write.snapshot()
			queuedPosition = activeSet.enqueueWrite(active)
			record.Detail = fmt.Sprintf("Queued in main lane · behind %s · position %d", activeWrite.ShortID, queuedPosition)
			record.Summary = record.Detail
			active.record = record
		} else {
			activeSet.set(jobClass, active)
			startImmediately = true
		}
	case JobClassBTW:
		if activeSet.jobForClass(jobClass) != nil || (!jm.allowBTWDuringWrite && activeSet.write != nil) || activeSet.isFull() {
			jm.mu.Unlock()
			return jm.sendBusy(ctx, msg, snapshotActiveJobs(knownJobs), jobClass)
		}
		activeSet.set(jobClass, active)
		startImmediately = true
	default:
		activeSet.set(jobClass, active)
		startImmediately = true
	}
	jm.mu.Unlock()

	if recoveredWrite != nil {
		go jm.runJob(recoveredWrite.ctx, recoveredWrite.target, recoveredWrite.msg, recoveredWrite)
	}

	if !startImmediately {
		if err := jm.store.Save(record); err != nil {
			return jm.rollbackQueuedSubmission(targetKey, active, fmt.Errorf("persist queued job: %w", err))
		}
		if err := jm.sendQueuedStatus(ctx, active, activeWrite, queuedPosition); err != nil {
			return jm.rollbackQueuedSubmission(targetKey, active, fmt.Errorf("send queued status: %w", err))
		}
		return nil
	}
	if err := jm.store.Save(record); err != nil {
		logger.WarnCF("jobs", "Failed to persist queued job", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
	}
	go jm.runJob(jobCtx, target, request.Message, active)
	return nil
}

func replyTargetsTrackedJob(jobs []*activeJob, replyMessageID string) bool {
	replyMessageID = strings.TrimSpace(replyMessageID)
	if replyMessageID == "" {
		return false
	}
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if strings.TrimSpace(job.snapshot().StatusMessageID) == replyMessageID {
			return true
		}
	}
	return false
}

func (jm *JobManager) rollbackQueuedSubmission(targetKey string, active *activeJob, cause error) error {
	if active == nil {
		return cause
	}
	record := active.snapshot()
	if active.cancel != nil {
		active.cancel()
	}

	jm.mu.Lock()
	var updatedQueued []JobRecord
	if activeSet := jm.active[targetKey]; activeSet != nil {
		activeSet.removeQueuedWrite(record.ID)
		updatedQueued = jm.reindexQueuedWritesLocked(activeSet)
		if activeSet.empty() {
			delete(jm.active, targetKey)
		}
	}
	jm.mu.Unlock()

	record = active.update(func(record *JobRecord) {
		record.State = JobStateFailed
		record.Phase = "failed"
		record.Detail = "Failed to queue background job"
		record.Summary = record.Detail
		record.LastError = utils.Truncate(compactJobLine(cause.Error()), 180)
		record.UpdatedAt = time.Now().UnixMilli()
	})
	if err := jm.store.Save(record); err != nil {
		logger.WarnCF("jobs", "Failed to persist queued rollback state", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
	}
	for _, queued := range updatedQueued {
		if err := jm.store.Save(queued); err != nil {
			logger.WarnCF("jobs", "Failed to persist reindexed queue after rollback", map[string]interface{}{"job_id": queued.ID, "error": err.Error()})
		}
	}
	record = jm.syncProgressRecord(context.Background(), record)
	_ = jm.syncProgressRecords(context.Background(), updatedQueued...)
	return cause
}

func (jm *JobManager) interruptStaleActiveJobs() error {
	records, err := jm.store.All()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.State != JobStateRunning {
			continue
		}
		record.State = JobStateInterrupted
		record.Phase = "interrupted"
		record.Detail = "Gateway restarted before this job finished"
		record.LastError = "interrupted on restart"
		record.UpdatedAt = time.Now().UnixMilli()
		if err := jm.store.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func (jm *JobManager) rehydrateQueuedJobs() error {
	records, err := jm.store.All()
	if err != nil {
		return err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].StartedAt == records[j].StartedAt {
			return records[i].ID < records[j].ID
		}
		return records[i].StartedAt < records[j].StartedAt
	})

	for _, record := range records {
		if record.State != JobStateQueued {
			continue
		}
		target, msg, err := rehydrateQueuedJob(record)
		if err != nil {
			record.State = JobStateInterrupted
			record.Phase = "interrupted"
			record.Detail = "Queued job could not be resumed after restart"
			record.Summary = record.Detail
			record.LastError = utils.Truncate(compactJobLine(err.Error()), 180)
			record.UpdatedAt = time.Now().UnixMilli()
			if saveErr := jm.store.Save(record); saveErr != nil {
				return saveErr
			}
			continue
		}

		jobCtx, cancel := context.WithCancel(context.Background())
		active := &activeJob{
			record: record,
			cancel: cancel,
			ctx:    jobCtx,
			target: target,
			msg:    msg,
		}

		targetKey := target.key()
		jm.mu.Lock()
		activeSet := jm.active[targetKey]
		if activeSet == nil {
			activeSet = &targetActiveJobs{}
			jm.active[targetKey] = activeSet
		}
		activeSet.enqueueWrite(active)
		jm.mu.Unlock()
	}
	var updatedQueued []JobRecord
	jm.mu.Lock()
	for _, activeSet := range jm.active {
		if activeSet == nil {
			continue
		}
		updatedQueued = append(updatedQueued, jm.reindexQueuedWritesLocked(activeSet)...)
	}
	jm.mu.Unlock()
	for _, record := range updatedQueued {
		if err := jm.store.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func rehydrateQueuedJob(record JobRecord) (LoopTarget, bus.InboundMessage, error) {
	target, err := targetFromJobRecord(record)
	if err != nil {
		return LoopTarget{}, bus.InboundMessage{}, err
	}
	msg := record.Message
	if strings.TrimSpace(msg.Channel) == "" || strings.TrimSpace(msg.ChatID) == "" || strings.TrimSpace(msg.SessionKey) == "" {
		return LoopTarget{}, bus.InboundMessage{}, fmt.Errorf("queued job payload missing original inbound context")
	}
	return target, msg, nil
}

func targetFromJobRecord(record JobRecord) (LoopTarget, error) {
	workspace := filepath.Clean(strings.TrimSpace(record.Workspace))
	if workspace == "" {
		return LoopTarget{}, fmt.Errorf("queued job missing workspace")
	}

	runtime := record.Runtime.normalized()
	if runtime == (RuntimeProfile{}) {
		runtime = parseRuntimeProfileKey(record.RuntimeKey)
	}

	return LoopTarget{Workspace: workspace, Runtime: runtime}.normalized(), nil
}

func parseRuntimeProfileKey(key string) RuntimeProfile {
	key = strings.TrimSpace(key)
	if key == "" {
		return RuntimeProfile{Mode: config.ModeCloud}
	}
	parts := strings.Split(key, "|")
	if strings.EqualFold(parts[0], config.ModePhi) {
		rt := RuntimeProfile{Mode: config.ModePhi}
		if len(parts) > 1 {
			rt.LocalBackend = parts[1]
		}
		if len(parts) > 2 {
			rt.LocalModel = parts[2]
		}
		if len(parts) > 3 {
			rt.LocalPreset = parts[3]
		}
		return rt.normalized()
	}
	return RuntimeProfile{Mode: parts[0]}.normalized()
}

func (jm *JobManager) handleControl(ctx context.Context, msg bus.InboundMessage, targetKey string, jobs []*activeJob, control parsedJobControl) error {
	if len(jobs) == 0 {
		text := "sciClaw is not currently running a background job for this workspace."
		if control.Kind == jobControlCancel {
			text = "sciClaw is not currently running a background job that can be cancelled."
		}
		return jm.sendText(ctx, msg.Channel, msg.ChatID, text)
	}

	selected, err := selectTrackedJob(jobs, control.TargetID, msg.Metadata["reply_message_id"])
	if err != nil {
		if control.TargetID == "" && len(jobs) > 1 && control.Kind == jobControlStatus {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, formatActiveJobList(snapshotActiveJobs(jobs)))
		}
		if control.TargetID == "" && len(jobs) > 1 && control.Kind == jobControlCancel {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, formatCancelRequiresJobID(snapshotActiveJobs(jobs)))
		}
		return jm.sendText(ctx, msg.Channel, msg.ChatID, err.Error())
	}

	record := selected.snapshot()
	switch control.Kind {
	case jobControlStatus:
		return jm.sendText(ctx, msg.Channel, msg.ChatID, formatActiveJobStatus(record, true))
	case jobControlCancel:
		if record.State == JobStateQueued {
			cancelled, cancelErr := jm.cancelQueuedJob(targetKey, record.ID)
			if cancelErr != nil {
				return jm.sendText(ctx, msg.Channel, msg.ChatID, cancelErr.Error())
			}
			return jm.sendText(ctx, msg.Channel, msg.ChatID, fmt.Sprintf("sciClaw removed queued job %s from the main queue.\n\nAsk: %s", cancelled.ShortID, fallbackAskSummary(cancelled.AskSummary)))
		}
		selected.cancel()
		return jm.sendText(ctx, msg.Channel, msg.ChatID, fmt.Sprintf("sciClaw is stopping job %s.\n\nAsk: %s", record.ShortID, fallbackAskSummary(record.AskSummary)))
	case jobControlForce:
		if record.State != JobStateQueued {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, fmt.Sprintf("Job %s is already running.", record.ShortID))
		}
		forced, forceErr := jm.forceQueuedJob(targetKey, record.ID)
		if forceErr != nil {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, forceErr.Error())
		}
		return jm.sendText(ctx, msg.Channel, msg.ChatID, fmt.Sprintf("sciClaw moved job %s to the front of the main queue.\n\nAsk: %s", forced.ShortID, fallbackAskSummary(forced.AskSummary)))
	default:
		return nil
	}
}

func (jm *JobManager) cancelQueuedJob(targetKey, jobID string) (JobRecord, error) {
	jm.mu.Lock()
	activeSet := jm.active[targetKey]
	if activeSet == nil {
		jm.mu.Unlock()
		return JobRecord{}, fmt.Errorf("No queued sciClaw job matches %s.", jobID)
	}
	job := activeSet.removeQueuedWrite(jobID)
	if job == nil && activeSet.write != nil {
		writeRecord := activeSet.write.snapshot()
		if writeRecord.ID == jobID && writeRecord.State == JobStateQueued {
			job = activeSet.write
			activeSet.write = nil
		}
	}
	if job == nil {
		jm.mu.Unlock()
		return JobRecord{}, fmt.Errorf("No queued sciClaw job matches %s.", jobID)
	}
	record := job.update(func(record *JobRecord) {
		record.State = JobStateCancelled
		record.Phase = "cancelled"
		record.Detail = "Removed from main queue"
		record.Summary = record.Detail
		record.UpdatedAt = time.Now().UnixMilli()
		record.LastError = ""
	})
	updatedQueued := jm.reindexQueuedWritesLocked(activeSet)
	if activeSet.empty() {
		delete(jm.active, targetKey)
	}
	jm.mu.Unlock()

	if err := jm.store.Save(record); err != nil {
		return JobRecord{}, err
	}
	for _, queued := range updatedQueued {
		if err := jm.store.Save(queued); err != nil {
			return JobRecord{}, err
		}
	}
	record = jm.syncProgressRecord(context.Background(), record)
	updatedQueued = jm.syncProgressRecords(context.Background(), updatedQueued...)
	if job.cancel != nil {
		job.cancel()
	}
	return record, nil
}

func (jm *JobManager) forceQueuedJob(targetKey, jobID string) (JobRecord, error) {
	jm.mu.Lock()
	activeSet := jm.active[targetKey]
	if activeSet == nil {
		jm.mu.Unlock()
		return JobRecord{}, fmt.Errorf("No queued sciClaw job matches %s.", jobID)
	}
	var updatedQueued []JobRecord
	if activeSet.write != nil {
		writeRecord := activeSet.write.snapshot()
		if writeRecord.ID == jobID && writeRecord.State == JobStateQueued {
			record := writeRecord
			updatedQueued = jm.reindexQueuedWritesLocked(activeSet)
			jm.mu.Unlock()
			for _, queued := range updatedQueued {
				if err := jm.store.Save(queued); err != nil {
					return JobRecord{}, err
				}
			}
			_ = jm.syncProgressRecords(context.Background(), updatedQueued...)
			return record, nil
		}
	}
	job := activeSet.moveQueuedWriteToFront(jobID)
	if job == nil {
		jm.mu.Unlock()
		return JobRecord{}, fmt.Errorf("No queued sciClaw job matches %s.", jobID)
	}
	updatedQueued = jm.reindexQueuedWritesLocked(activeSet)
	record := job.snapshot()
	jm.mu.Unlock()

	if err := jm.store.Save(record); err != nil {
		return JobRecord{}, err
	}
	for _, queued := range updatedQueued {
		if err := jm.store.Save(queued); err != nil {
			return JobRecord{}, err
		}
	}
	_ = jm.syncProgressRecords(context.Background(), updatedQueued...)
	return record, nil
}

func (jm *JobManager) reindexQueuedWritesLocked(activeSet *targetActiveJobs) []JobRecord {
	if activeSet == nil || len(activeSet.queuedWrites) == 0 {
		return nil
	}
	leadShortID := ""
	if activeSet.write != nil {
		leadShortID = activeSet.write.snapshot().ShortID
	}
	updated := make([]JobRecord, 0, len(activeSet.queuedWrites))
	for idx, job := range activeSet.queuedWrites {
		if job == nil {
			continue
		}
		position := idx + 1
		detail := fmt.Sprintf("Queued in main lane · position %d", position)
		switch {
		case position == 1 && leadShortID != "":
			detail = fmt.Sprintf("Queued in main lane · next up after %s", leadShortID)
		case position == 1:
			detail = "Queued in main lane · next up"
		case leadShortID != "":
			detail = fmt.Sprintf("Queued in main lane · behind %s · position %d", leadShortID, position)
		}
		updated = append(updated, job.update(func(record *JobRecord) {
			record.State = JobStateQueued
			record.Phase = "queued"
			record.Detail = detail
			record.Summary = detail
			record.UpdatedAt = time.Now().UnixMilli()
		}))
	}
	return updated
}

func (jm *JobManager) syncProgressRecord(ctx context.Context, record JobRecord) JobRecord {
	if jm.progress == nil || strings.TrimSpace(record.StatusMessageID) == "" {
		return record
	}
	message := formatProgressOutbound(record)
	message.Channel = record.Channel
	message.ChatID = record.ChatID
	messageID, err := jm.progress.SendOrEditProgress(ctx, record.Channel, record.ChatID, record.StatusMessageID, message)
	if err != nil {
		logger.WarnCF("jobs", "Failed to sync progress record", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
		return record
	}
	if trimmed := strings.TrimSpace(messageID); trimmed != "" && trimmed != strings.TrimSpace(record.StatusMessageID) {
		record.StatusMessageID = trimmed
		if err := jm.store.Save(record); err != nil {
			logger.WarnCF("jobs", "Failed to persist synced progress record", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
		}
	}
	return record
}

func (jm *JobManager) syncProgressRecords(ctx context.Context, records ...JobRecord) []JobRecord {
	updated := make([]JobRecord, 0, len(records))
	for _, record := range records {
		updated = append(updated, jm.syncProgressRecord(ctx, record))
	}
	return updated
}

func (jm *JobManager) runJob(ctx context.Context, target LoopTarget, msg bus.InboundMessage, active *activeJob) {
	targetKey := target.key()
	jobClass := active.snapshot().Class
	progress := jm.newProgressReporter(ctx, active)
	defer progress.finish()
	defer func() {
		var nextWrite *activeJob
		jm.mu.Lock()
		if activeSet := jm.active[targetKey]; activeSet != nil {
			activeSet.clear(jobClass)
			if jobClass == JobClassWrite {
				nextWrite = activeSet.popQueuedWrite()
				if nextWrite != nil {
					activeSet.write = nextWrite
				}
			}
			if activeSet.empty() {
				delete(jm.active, targetKey)
			}
		}
		jm.mu.Unlock()
		if nextWrite != nil {
			go jm.runJob(nextWrite.ctx, nextWrite.target, nextWrite.msg, nextWrite)
		}
	}()

	progress.update(JobStateQueued, "queued", "Queued")

	select {
	case jm.semaphore <- struct{}{}:
		defer func() { <-jm.semaphore }()
	case <-ctx.Done():
		progress.complete(JobStateCancelled, "cancelled", "Cancelled before start", ctx.Err())
		return
	}

	progress.update(JobStateRunning, "preparing_context", "Preparing context")

	handler, err := jm.resolveForClass(jobClass, target)
	if err != nil {
		progress.complete(JobStateFailed, "failed", "Failed to start", err)
		return
	}

	_, runErr := handler.RunJob(ctx, msg, func(phase, detail string) {
		progress.throttledUpdate(phase, detail)
	})

	switch {
	case ctx.Err() != nil:
		progress.complete(JobStateCancelled, "cancelled", "Cancelled", ctx.Err())
	case runErr != nil:
		progress.complete(JobStateFailed, "failed", "Job failed", runErr)
	default:
		progress.complete(JobStateDone, "done", "Done. Reply posted below.", nil)
	}
}

func (jm *JobManager) sendText(ctx context.Context, channel, chatID, content string) error {
	if jm.bus == nil {
		return nil
	}
	return jm.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: content,
	})
}

func (jm *JobManager) sendQueuedStatus(ctx context.Context, active *activeJob, running JobRecord, position int) error {
	record := active.snapshot()
	if strings.TrimSpace(record.Detail) == "" && position > 0 {
		record.Detail = fmt.Sprintf("Queued in main lane · behind %s · position %d", running.ShortID, position)
	}
	message := formatProgressOutbound(record)
	message.Channel = record.Channel
	message.ChatID = record.ChatID
	if jm.progress != nil {
		messageID, err := jm.progress.SendOrEditProgress(ctx, record.Channel, record.ChatID, record.StatusMessageID, message)
		if err != nil {
			return err
		}
		if strings.TrimSpace(messageID) != "" && strings.TrimSpace(messageID) != record.StatusMessageID {
			record = active.update(func(existing *JobRecord) {
				existing.StatusMessageID = strings.TrimSpace(messageID)
			})
			if err := jm.store.Save(record); err != nil {
				return err
			}
		}
		return nil
	}
	if jm.bus == nil {
		return nil
	}
	return jm.bus.PublishOutbound(ctx, message)
}

func (jm *JobManager) nextJobID() (string, string) {
	n := atomic.AddUint64(&jm.counter, 1)
	return fmt.Sprintf("job-%d-%d", time.Now().UnixMilli(), n), formatShortJobRef(n)
}

func (jm *JobManager) restoreCounterFromStore() error {
	records, err := jm.store.All()
	if err != nil {
		return err
	}
	var maxCounter uint64
	for _, record := range records {
		if counter, ok := parseJobCounter(record); ok && counter > maxCounter {
			maxCounter = counter
		}
	}
	atomic.StoreUint64(&jm.counter, maxCounter)
	return nil
}

func parseJobCounter(record JobRecord) (uint64, bool) {
	if id := strings.TrimSpace(record.ID); id != "" {
		if idx := strings.LastIndex(id, "-"); idx >= 0 && idx < len(id)-1 {
			if counter, err := strconv.ParseUint(id[idx+1:], 10, 64); err == nil {
				return counter, true
			}
		}
	}
	if shortID := strings.TrimSpace(record.ShortID); shortID != "" {
		if counter, err := strconv.ParseUint(shortID, 36, 64); err == nil {
			return counter, true
		}
	}
	return 0, false
}

type progressReporter struct {
	manager   *JobManager
	ctx       context.Context
	active    *activeJob
	recordMu  sync.Mutex
	lastPhase string
	lastSent  time.Time
	done      chan struct{}
}

func (jm *JobManager) newProgressReporter(ctx context.Context, active *activeJob) *progressReporter {
	reporter := &progressReporter{
		manager: jm,
		ctx:     ctx,
		active:  active,
		done:    make(chan struct{}),
	}
	if jm.progressInterval > 0 {
		go reporter.runHeartbeat()
	}
	return reporter
}

func (p *progressReporter) finish() {
	close(p.done)
}

func (p *progressReporter) runHeartbeat() {
	ticker := time.NewTicker(p.manager.progressInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.heartbeat()
		}
	}
}

func (p *progressReporter) heartbeat() {
	p.recordMu.Lock()
	defer p.recordMu.Unlock()
	if p.active == nil {
		return
	}
	record := p.active.snapshot()
	if record.State != JobStateQueued && record.State != JobStateRunning {
		return
	}
	if !p.lastSent.IsZero() && time.Since(p.lastSent) < p.manager.progressInterval {
		return
	}
	p.sendLocked(record)
}

func (p *progressReporter) throttledUpdate(phase, detail string) {
	p.recordMu.Lock()
	defer p.recordMu.Unlock()
	now := time.Now()
	if phase == p.lastPhase && !p.lastSent.IsZero() && now.Sub(p.lastSent) < p.manager.progressInterval {
		return
	}
	p.updateLocked(JobStateRunning, phase, detail)
}

func (p *progressReporter) update(state JobState, phase, detail string) {
	p.recordMu.Lock()
	defer p.recordMu.Unlock()
	p.updateLocked(state, phase, detail)
}

func (p *progressReporter) updateLocked(state JobState, phase, detail string) {
	if p.active == nil {
		return
	}
	record := p.active.update(func(record *JobRecord) {
		record.State = state
		record.Phase = strings.TrimSpace(phase)
		record.Detail = strings.TrimSpace(detail)
		record.Summary = record.Detail
		record.UpdatedAt = time.Now().UnixMilli()
		if record.Phase == "" {
			record.Phase = string(state)
		}
	})
	p.lastPhase = record.Phase

	if err := p.manager.store.Save(record); err != nil {
		logger.WarnCF("jobs", "Failed to persist job progress", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
	}
	p.sendLocked(record)
}

func (p *progressReporter) sendLocked(record JobRecord) {
	if p.manager.progress == nil {
		return
	}

	message := formatProgressOutbound(record)
	message.Channel = record.Channel
	message.ChatID = record.ChatID
	messageID, err := p.manager.progress.SendOrEditProgress(p.ctx, record.Channel, record.ChatID, record.StatusMessageID, message)
	if err != nil {
		logger.WarnCF("jobs", "Failed to send progress update", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
		return
	}
	if strings.TrimSpace(messageID) != "" && strings.TrimSpace(messageID) != record.StatusMessageID {
		record = p.active.update(func(existing *JobRecord) {
			existing.StatusMessageID = strings.TrimSpace(messageID)
		})
		if err := p.manager.store.Save(record); err != nil {
			logger.WarnCF("jobs", "Failed to persist progress message id", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
		}
	}
	p.lastSent = time.Now()
}

func (p *progressReporter) complete(state JobState, phase, detail string, err error) {
	p.recordMu.Lock()
	defer p.recordMu.Unlock()
	if p.active == nil {
		return
	}
	record := p.active.update(func(record *JobRecord) {
		record.State = state
		record.Phase = strings.TrimSpace(phase)
		record.Detail = strings.TrimSpace(detail)
		record.Summary = record.Detail
		record.UpdatedAt = time.Now().UnixMilli()
		record.LastError = ""
		if err != nil {
			record.LastError = utils.Truncate(compactJobLine(err.Error()), 180)
		}
	})
	if saveErr := p.manager.store.Save(record); saveErr != nil {
		logger.WarnCF("jobs", "Failed to persist final job state", map[string]interface{}{"job_id": record.ID, "error": saveErr.Error()})
	}
	p.sendLocked(record)
}

type jobControl int

const (
	jobControlNone jobControl = iota
	jobControlStatus
	jobControlCancel
	jobControlForce
)

type parsedJobControl struct {
	Kind     jobControl
	TargetID string
	Directed bool
}

var discordMentionPattern = regexp.MustCompile(`<@!?\d+>`)

func parseJobControl(msg bus.InboundMessage) parsedJobControl {
	content := compactJobLine(discordMentionPattern.ReplaceAllString(strings.TrimSpace(msg.Content), ""))
	fields := strings.Fields(strings.ToLower(content))
	if len(fields) == 0 {
		return parsedJobControl{}
	}

	parseTarget := func(idx int) string {
		if len(fields) <= idx {
			return ""
		}
		return strings.ToUpper(strings.TrimSpace(fields[idx]))
	}

	control := parsedJobControl{
		Directed: isControlDirected(msg),
	}
	switch fields[0] {
	case "status", "progress":
		control.Kind = jobControlStatus
		control.TargetID = parseTarget(1)
	case "job":
		if len(fields) >= 2 && fields[1] == "status" {
			control.Kind = jobControlStatus
			control.TargetID = parseTarget(2)
		}
	case "cancel", "stop":
		control.Kind = jobControlCancel
		control.TargetID = parseTarget(1)
		if control.TargetID == "JOB" {
			control.TargetID = parseTarget(2)
		}
	case "force":
		control.Kind = jobControlForce
		control.TargetID = parseTarget(1)
		if control.TargetID == "JOB" {
			control.TargetID = parseTarget(2)
		}
	}
	return control
}

func isControlDirected(msg bus.InboundMessage) bool {
	meta := msg.Metadata
	if strings.EqualFold(strings.TrimSpace(meta["is_dm"]), "true") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(meta["has_direct_mention"]), "true") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(meta["reply_to_bot"]), "true")
}

func classifyJobRequest(msg bus.InboundMessage) parsedJobRequest {
	out := parsedJobRequest{
		Class:   JobClassWrite,
		Message: msg,
	}
	content := compactJobLine(discordMentionPattern.ReplaceAllString(strings.TrimSpace(msg.Content), ""))
	if content == "" {
		return out
	}
	lower := strings.ToLower(content)
	if lower == "/btw" || strings.HasPrefix(lower, "/btw ") {
		cleaned := strings.TrimSpace(content[len("/btw"):])
		out.Class = JobClassBTW
		out.Message.Content = cleaned
	}
	return out
}

func fallbackAskSummary(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "Working request"
	}
	return strings.TrimSpace(summary)
}

func formatDiscordRelativeTimestamp(startedAtMillis int64) string {
	if startedAtMillis <= 0 {
		return "just now"
	}
	return fmt.Sprintf("<t:%d:R>", startedAtMillis/1000)
}

func formatJobCardHeader(record JobRecord) string {
	return fmt.Sprintf("**%s** · **%s** · %s · %s", record.ShortID, humanizePhase(record.Phase), jobLaneLabel(record.Class), formatDiscordRelativeTimestamp(record.StartedAt))
}

func formatJobCardDetail(record JobRecord) string {
	detail := strings.TrimSpace(record.Detail)
	if detail == "" || strings.EqualFold(detail, humanizePhase(record.Phase)) {
		return ""
	}
	return "_" + utils.Truncate(detail, 120) + "_"
}

func formatJobCardAsk(record JobRecord) string {
	return "> " + fallbackAskSummary(record.AskSummary)
}

func formatJobControlHint(record JobRecord) string {
	if record.State == JobStateQueued {
		return "Reply `status` · `force` to move next · `cancel` to drop from queue"
	}
	if record.Class == JobClassBTW {
		return "Reply `status` · `cancel` to clear /btw"
	}
	return "Reply `status` · `cancel` to stop it"
}

func summarizeJobAsk(content string) string {
	trimmed := compactJobLine(discordMentionPattern.ReplaceAllString(content, ""))
	if trimmed == "" {
		return "Working request"
	}
	if idx := strings.IndexAny(trimmed, ".!?"); idx >= 0 && idx < 120 {
		trimmed = strings.TrimSpace(trimmed[:idx+1])
	}
	return utils.Truncate(trimmed, 100)
}

func snapshotActiveJobs(active []*activeJob) []JobRecord {
	records := make([]JobRecord, 0, len(active))
	for _, job := range active {
		if job == nil {
			continue
		}
		records = append(records, job.snapshot())
	}
	return records
}

func selectTrackedJob(jobs []*activeJob, targetID, replyMessageID string) (*activeJob, error) {
	if len(jobs) == 0 {
		return nil, fmt.Errorf("sciClaw is not currently running a background job for this workspace.")
	}
	if targetID != "" {
		for _, job := range jobs {
			record := job.snapshot()
			if strings.EqualFold(record.ShortID, targetID) || strings.EqualFold(record.ID, targetID) {
				return job, nil
			}
		}
		return nil, fmt.Errorf("No sciClaw job matches %s.", targetID)
	}
	if strings.TrimSpace(replyMessageID) != "" {
		for _, job := range jobs {
			if job.snapshot().StatusMessageID == strings.TrimSpace(replyMessageID) {
				return job, nil
			}
		}
	}
	if len(jobs) == 1 {
		return jobs[0], nil
	}
	return nil, fmt.Errorf("Multiple sciClaw jobs are active here.")
}

func formatProgressMessage(record JobRecord) string {
	lines := []string{formatJobCardHeader(record), formatJobCardAsk(record)}
	if detail := formatJobCardDetail(record); detail != "" {
		lines = append(lines, detail)
	}
	switch record.State {
	case JobStateQueued, JobStateRunning:
		lines = append(lines, "", formatJobControlHint(record))
	case JobStateDone:
		lines = append(lines, "", "Done. Reply below.")
	case JobStateCancelled:
		lines = append(lines, "", "This job was cancelled.")
	case JobStateFailed, JobStateInterrupted:
		if strings.TrimSpace(record.LastError) != "" {
			lines = append(lines, "", "Last error: "+strings.TrimSpace(record.LastError))
		}
	}
	return strings.Join(lines, "\n")
}

func formatProgressOutbound(record JobRecord) bus.OutboundMessage {
	return bus.OutboundMessage{
		Content: formatProgressMessage(record),
		Embeds:  []bus.OutboundEmbed{formatProgressEmbed(record)},
	}
}

func formatProgressEmbed(record JobRecord) bus.OutboundEmbed {
	embed := bus.OutboundEmbed{
		Title:         fmt.Sprintf("sciClaw · %s", record.ShortID),
		Description:   formatJobCardAsk(record),
		Color:         jobStateColor(record.State),
		TimestampUnix: record.UpdatedAt / 1000,
	}

	embed.Fields = append(embed.Fields,
		bus.OutboundEmbedField{Name: "Lane", Value: jobLaneLabel(record.Class), Inline: true},
		bus.OutboundEmbedField{Name: "Status", Value: humanizePhase(record.Phase), Inline: true},
		bus.OutboundEmbedField{Name: "Started", Value: formatDiscordRelativeTimestamp(record.StartedAt), Inline: true},
	)

	if detail := strings.TrimSpace(record.Detail); detail != "" && !strings.EqualFold(detail, humanizePhase(record.Phase)) {
		embed.Fields = append(embed.Fields, bus.OutboundEmbedField{
			Name:   "Detail",
			Value:  utils.Truncate(detail, 240),
			Inline: false,
		})
	}

	switch record.State {
	case JobStateQueued, JobStateRunning:
		embed.Footer = strings.TrimPrefix(formatJobControlHint(record), "Controls: ")
	case JobStateDone:
		embed.Footer = "Done. Reply below."
	case JobStateCancelled:
		embed.Footer = "Cancelled."
	case JobStateFailed, JobStateInterrupted:
		if strings.TrimSpace(record.LastError) != "" {
			embed.Fields = append(embed.Fields, bus.OutboundEmbedField{
				Name:   "Last error",
				Value:  utils.Truncate(strings.TrimSpace(record.LastError), 240),
				Inline: false,
			})
		}
		embed.Footer = strings.ToUpper(string(record.State[:1])) + string(record.State[1:]) + "."
	}

	return embed
}

func jobStateColor(state JobState) int {
	switch state {
	case JobStateQueued:
		return 0x3B82F6
	case JobStateRunning:
		return 0xF59E0B
	case JobStateDone:
		return 0x10B981
	case JobStateCancelled:
		return 0x6B7280
	case JobStateFailed, JobStateInterrupted:
		return 0xEF4444
	default:
		return 0x64748B
	}
}

func formatActiveJobStatus(record JobRecord, includeControlHint bool) string {
	lines := []string{formatJobCardHeader(record), formatJobCardAsk(record)}
	if detail := formatJobCardDetail(record); detail != "" {
		lines = append(lines, detail)
	}
	if includeControlHint {
		lines = append(lines, "", formatJobControlHint(record))
	}
	return strings.Join(lines, "\n")
}

func formatActiveJobList(records []JobRecord) string {
	lines := []string{"**sciClaw jobs in this workspace**", ""}
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("- **%s** · %s · %s · %s", record.ShortID, jobLaneLabel(record.Class), humanizePhase(record.Phase), formatDiscordRelativeTimestamp(record.StartedAt)))
		lines = append(lines, fmt.Sprintf("  %s", fallbackAskSummary(record.AskSummary)))
		if detail := formatJobCardDetail(record); detail != "" {
			lines = append(lines, fmt.Sprintf("  %s", detail))
		}
	}
	lines = append(lines, "", "Reply on a job card with `status` or `cancel`, or direct `status <job-id>` at sciClaw.")
	if hasQueuedMainLane(records) {
		lines = append(lines, "Queued main-lane jobs can also use `force <job-id>` when directed at sciClaw.")
	}
	return strings.Join(lines, "\n")
}

func formatCancelRequiresJobID(records []JobRecord) string {
	lines := []string{"**sciClaw jobs in this workspace**", "Reply `cancel` on a job card, or direct `cancel <job-id>` at sciClaw.", ""}
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("- **%s** · %s · %s", record.ShortID, jobLaneLabel(record.Class), fallbackAskSummary(record.AskSummary)))
	}
	if hasQueuedMainLane(records) {
		lines = append(lines, "", "Queued main-lane jobs can also use directed `force <job-id>` to move next.")
	}
	return strings.Join(lines, "\n")
}

func formatBusyMessage(records []JobRecord, requestedClass JobClass) string {
	if len(records) == 0 {
		return "sciClaw is already busy with this workspace."
	}
	lines := []string{formatActiveJobStatus(records[0], false)}
	if len(records) > 1 {
		lines = []string{formatActiveJobList(records)}
	}
	switch requestedClass {
	case JobClassBTW:
		lines = append(lines, "", "The /btw lane is already in use in this workspace.")
		lines = append(lines, "Reply on that job card with `status` or `cancel`, or direct `status <job-id>` at sciClaw.")
		lines = append(lines, "If this can wait, resend it without `/btw` to place it in the main queue.")
	default:
		lines = append(lines, "", "Main-lane work is queued in this workspace.")
		if hasQueuedMainLane(records) {
			lines = append(lines, "Queued main-lane jobs can use directed `force <job-id>` to move next.")
		}
		lines = append(lines, "Reply on a job card with `status` or `cancel`, or direct `status <job-id>` at sciClaw.")
	}
	return strings.Join(lines, "\n")
}

func jobLaneLabel(class JobClass) string {
	switch class {
	case JobClassBTW:
		return "/btw lane"
	default:
		return "main lane"
	}
}

func hasQueuedMainLane(records []JobRecord) bool {
	for _, record := range records {
		if record.Class == JobClassWrite && record.State == JobStateQueued {
			return true
		}
	}
	return false
}

func (jm *JobManager) sendBusy(ctx context.Context, msg bus.InboundMessage, records []JobRecord, requestedClass JobClass) error {
	return jm.sendText(ctx, msg.Channel, msg.ChatID, formatBusyMessage(records, requestedClass))
}

func humanizePhase(phase string) string {
	switch strings.TrimSpace(phase) {
	case "queued":
		return "Queued"
	case "preparing_context":
		return "Preparing context"
	case "thinking":
		return "Thinking"
	case "using_tools":
		return "Using tools"
	case "saving":
		return "Saving"
	case "replying":
		return "Replying"
	case "cancelled":
		return "Cancelled"
	case "failed":
		return "Failed"
	case "done":
		return "Done"
	case "interrupted":
		return "Interrupted"
	default:
		if phase == "" {
			return "Working"
		}
		normalized := strings.ReplaceAll(phase, "_", " ")
		return strings.ToUpper(normalized[:1]) + normalized[1:]
	}
}

func compactJobLine(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func newJobStore(path string) (*jobStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	s := &jobStore{
		path: path,
		jobs: map[string]JobRecord{},
	}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *jobStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var payload struct {
		Jobs []JobRecord `json:"jobs"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	for _, job := range payload.Jobs {
		s.jobs[job.ID] = job
	}
	return nil
}

func (s *jobStore) Save(record JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[record.ID] = record
	return s.writeLocked()
}

func (s *jobStore) All() ([]JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobRecord, 0, len(s.jobs))
	for _, record := range s.jobs {
		out = append(out, record)
	}
	return out, nil
}

func (s *jobStore) writeLocked() error {
	payload := struct {
		Jobs []JobRecord `json:"jobs"`
	}{Jobs: make([]JobRecord, 0, len(s.jobs))}
	for _, record := range s.jobs {
		payload.Jobs = append(payload.Jobs, record)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
