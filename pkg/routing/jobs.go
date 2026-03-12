package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	SendOrEditProgress(ctx context.Context, channelName, chatID, messageID, content string) (string, error)
}

type handlerResolverFunc func(target LoopTarget) (JobRunner, error)

type JobRecord struct {
	ID              string   `json:"id"`
	Channel         string   `json:"channel"`
	ChatID          string   `json:"chat_id"`
	Workspace       string   `json:"workspace"`
	RuntimeKey      string   `json:"runtime_key"`
	TargetKey       string   `json:"target_key"`
	State           JobState `json:"state"`
	Phase           string   `json:"phase,omitempty"`
	Detail          string   `json:"detail,omitempty"`
	StatusMessageID string   `json:"status_message_id,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
	StartedAt       int64    `json:"started_at"`
	UpdatedAt       int64    `json:"updated_at"`
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

type JobManager struct {
	bus              *bus.MessageBus
	progress         ProgressMessenger
	resolve          handlerResolverFunc
	progressInterval time.Duration
	store            *jobStore
	semaphore        chan struct{}

	mu      sync.Mutex
	active  map[string]*activeJob
	counter uint64
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
		bus:              msgBus,
		progress:         progress,
		resolve:          resolve,
		progressInterval: progressEvery,
		store:            store,
		semaphore:        make(chan struct{}, maxConcurrent),
		active:           map[string]*activeJob{},
	}
	if err := jm.interruptStaleActiveJobs(); err != nil {
		return nil, err
	}
	return jm, nil
}

func (jm *JobManager) ShouldHandleChannel(channel string) bool {
	return strings.EqualFold(strings.TrimSpace(channel), "discord")
}

func (jm *JobManager) Submit(ctx context.Context, target LoopTarget, msg bus.InboundMessage) error {
	target = target.normalized()
	targetKey := target.key()

	control := parseJobControl(msg.Content)

	jm.mu.Lock()
	active := jm.active[targetKey]
	if active != nil {
		jm.mu.Unlock()
		return jm.handleControlOrBusy(ctx, msg, active, control)
	}
	if control != jobControlNone {
		jm.mu.Unlock()
		return jm.sendNoActiveJobMessage(ctx, msg, control)
	}

	jobID := jm.nextJobID()
	jobCtx, cancel := context.WithCancel(ctx)
	record := JobRecord{
		ID:         jobID,
		Channel:    msg.Channel,
		ChatID:     msg.ChatID,
		Workspace:  target.Workspace,
		RuntimeKey: target.Runtime.Key(),
		TargetKey:  targetKey,
		State:      JobStateQueued,
		Phase:      "queued",
		Detail:     "Queued",
		StartedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	active = &activeJob{record: record, cancel: cancel}
	jm.active[targetKey] = active
	jm.mu.Unlock()

	if err := jm.store.Save(record); err != nil {
		logger.WarnCF("jobs", "Failed to persist queued job", map[string]interface{}{"job_id": record.ID, "error": err.Error()})
	}

	go jm.runJob(jobCtx, target, msg, active)
	return nil
}

func (jm *JobManager) interruptStaleActiveJobs() error {
	records, err := jm.store.All()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.State != JobStateQueued && record.State != JobStateRunning {
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

func (jm *JobManager) handleControlOrBusy(ctx context.Context, msg bus.InboundMessage, active *activeJob, control jobControl) error {
	record := active.snapshot()
	switch control {
	case jobControlStatus:
		return jm.sendText(ctx, msg.Channel, msg.ChatID, formatActiveJobStatus(record, false))
	case jobControlCancel:
		active.cancel()
		return jm.sendText(ctx, msg.Channel, msg.ChatID, "sciClaw is stopping the background job for this workspace.")
	default:
		return jm.sendText(ctx, msg.Channel, msg.ChatID, formatBusyMessage(record))
	}
}

func (jm *JobManager) sendNoActiveJobMessage(ctx context.Context, msg bus.InboundMessage, control jobControl) error {
	text := "sciClaw is not currently running a background job for this workspace."
	if control == jobControlCancel {
		text = "sciClaw is not currently running a background job that can be cancelled."
	}
	return jm.sendText(ctx, msg.Channel, msg.ChatID, text)
}

func (jm *JobManager) runJob(ctx context.Context, target LoopTarget, msg bus.InboundMessage, active *activeJob) {
	targetKey := target.key()
	progress := jm.newProgressReporter(ctx, active)
	defer progress.finish()
	defer func() {
		jm.mu.Lock()
		delete(jm.active, targetKey)
		jm.mu.Unlock()
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

	handler, err := jm.resolve(target)
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

func (jm *JobManager) nextJobID() string {
	n := atomic.AddUint64(&jm.counter, 1)
	return fmt.Sprintf("job-%d-%d", time.Now().UnixMilli(), n)
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

	content := formatProgressMessage(record)
	messageID, err := p.manager.progress.SendOrEditProgress(p.ctx, record.Channel, record.ChatID, record.StatusMessageID, content)
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
)

func parseJobControl(content string) jobControl {
	trimmed := strings.ToLower(strings.TrimSpace(content))
	switch trimmed {
	case "status", "progress", "job status":
		return jobControlStatus
	case "cancel", "stop", "cancel job", "stop job":
		return jobControlCancel
	default:
		return jobControlNone
	}
}

func formatProgressMessage(record JobRecord) string {
	lines := []string{
		"sciClaw is working in the background.",
		"",
		fmt.Sprintf("Status: %s", humanizePhase(record.Phase)),
	}
	if strings.TrimSpace(record.Detail) != "" && !strings.EqualFold(strings.TrimSpace(record.Detail), humanizePhase(record.Phase)) {
		lines = append(lines, fmt.Sprintf("Detail: %s", strings.TrimSpace(record.Detail)))
	}
	lines = append(lines, fmt.Sprintf("Started: %s ago", time.Since(time.UnixMilli(record.StartedAt)).Round(time.Second)))
	switch record.State {
	case JobStateQueued, JobStateRunning:
		lines = append(lines, "", "Reply with `status` to check progress or `cancel` to stop this job.")
	case JobStateDone:
		lines = append(lines, "", "Done. The full reply is below.")
	case JobStateCancelled:
		lines = append(lines, "", "This job was cancelled.")
	case JobStateFailed, JobStateInterrupted:
		if strings.TrimSpace(record.LastError) != "" {
			lines = append(lines, "", "Last error: "+strings.TrimSpace(record.LastError))
		}
	}
	return strings.Join(lines, "\n")
}

func formatActiveJobStatus(record JobRecord, includeControlHint bool) string {
	text := fmt.Sprintf("sciClaw is already working in the background.\n\nStatus: %s", humanizePhase(record.Phase))
	if strings.TrimSpace(record.Detail) != "" {
		text += "\nDetail: " + strings.TrimSpace(record.Detail)
	}
	text += "\nStarted: " + time.Since(time.UnixMilli(record.StartedAt)).Round(time.Second).String() + " ago"
	if includeControlHint {
		text += "\n\nReply with `status` to check progress or `cancel` to stop this job."
	}
	return text
}

func formatBusyMessage(record JobRecord) string {
	return formatActiveJobStatus(record, false) + "\n\nTo avoid mixing partial work into a second request, sciClaw is not starting another job in this workspace yet. Reply with `status` or `cancel`, then send the next request after this finishes."
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
