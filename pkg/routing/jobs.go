package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	SendOrEditProgress(ctx context.Context, channelName, chatID, messageID, content string) (string, error)
}

type handlerResolverFunc func(target LoopTarget) (JobRunner, error)

type JobClass string

const (
	JobClassWrite            JobClass = "write"
	JobClassExternalReadOnly JobClass = "external_readonly"
)

type JobRecord struct {
	ID              string   `json:"id"`
	ShortID         string   `json:"short_id"`
	Channel         string   `json:"channel"`
	ChatID          string   `json:"chat_id"`
	Workspace       string   `json:"workspace"`
	RuntimeKey      string   `json:"runtime_key"`
	TargetKey       string   `json:"target_key"`
	Class           JobClass `json:"class"`
	State           JobState `json:"state"`
	Phase           string   `json:"phase,omitempty"`
	Detail          string   `json:"detail,omitempty"`
	StatusMessageID string   `json:"status_message_id,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	AskSummary      string   `json:"ask_summary,omitempty"`
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

type targetActiveJobs struct {
	write            *activeJob
	externalReadOnly *activeJob
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
	case JobClassExternalReadOnly:
		s.externalReadOnly = job
	default:
		s.write = job
	}
}

func (s *targetActiveJobs) clear(class JobClass) {
	switch class {
	case JobClassExternalReadOnly:
		s.externalReadOnly = nil
	default:
		s.write = nil
	}
}

func (s *targetActiveJobs) jobForClass(class JobClass) *activeJob {
	switch class {
	case JobClassExternalReadOnly:
		return s.externalReadOnly
	default:
		return s.write
	}
}

func (s *targetActiveJobs) empty() bool {
	return s == nil || (s.write == nil && s.externalReadOnly == nil)
}

func (s *targetActiveJobs) isFull() bool {
	return s != nil && s.write != nil && s.externalReadOnly != nil
}

func (s *targetActiveJobs) snapshot() []*activeJob {
	if s == nil {
		return nil
	}
	out := make([]*activeJob, 0, 2)
	if s.write != nil {
		out = append(out, s.write)
	}
	if s.externalReadOnly != nil {
		out = append(out, s.externalReadOnly)
	}
	return out
}

type JobManager struct {
	bus                      *bus.MessageBus
	progress                 ProgressMessenger
	resolve                  handlerResolverFunc
	resolveExternal          handlerResolverFunc
	progressInterval         time.Duration
	store                    *jobStore
	semaphore                chan struct{}
	allowReadOnlyDuringWrite bool

	mu      sync.Mutex
	active  map[string]*targetActiveJobs
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
		bus:                      msgBus,
		progress:                 progress,
		resolve:                  resolve,
		progressInterval:         progressEvery,
		store:                    store,
		semaphore:                make(chan struct{}, maxConcurrent),
		allowReadOnlyDuringWrite: cfg.AllowReadOnlyDuringWrite,
		active:                   map[string]*targetActiveJobs{},
	}
	if err := jm.interruptStaleActiveJobs(); err != nil {
		return nil, err
	}
	return jm, nil
}

func (jm *JobManager) SetExternalReadOnlyResolver(resolve handlerResolverFunc) {
	jm.mu.Lock()
	jm.resolveExternal = resolve
	jm.mu.Unlock()
}

func (jm *JobManager) ShouldHandleChannel(channel string) bool {
	return strings.EqualFold(strings.TrimSpace(channel), "discord")
}

func (jm *JobManager) resolveForClass(class JobClass, target LoopTarget) (JobRunner, error) {
	if class == JobClassExternalReadOnly {
		jm.mu.Lock()
		resolve := jm.resolveExternal
		jm.mu.Unlock()
		if resolve == nil {
			return nil, fmt.Errorf("external readonly job runner is not configured")
		}
		return resolve(target)
	}
	return jm.resolve(target)
}

func (jm *JobManager) Submit(ctx context.Context, target LoopTarget, msg bus.InboundMessage) error {
	target = target.normalized()
	targetKey := target.key()
	control := parseJobControl(msg)
	jobClass := classifyJobClass(msg)

	jm.mu.Lock()
	activeSet := jm.active[targetKey]
	activeJobs := activeSet.snapshot()
	if control.Kind != jobControlNone && control.Directed {
		jm.mu.Unlock()
		return jm.handleControl(ctx, msg, activeJobs, control)
	}
	if activeSet == nil {
		activeSet = &targetActiveJobs{}
		jm.active[targetKey] = activeSet
	}

	if activeSet.jobForClass(jobClass) != nil || (!jm.allowReadOnlyDuringWrite && jobClass == JobClassExternalReadOnly && activeSet.write != nil) || activeSet.isFull() {
		jm.mu.Unlock()
		return jm.sendBusy(ctx, msg, snapshotActiveJobs(activeJobs), jobClass)
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
		AskSummary: summarizeJobAsk(msg.Content),
		StartedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	active := &activeJob{record: record, cancel: cancel}
	activeSet.set(jobClass, active)
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

func (jm *JobManager) handleControl(ctx context.Context, msg bus.InboundMessage, active []*activeJob, control parsedJobControl) error {
	if len(active) == 0 {
		text := "sciClaw is not currently running a background job for this workspace."
		if control.Kind == jobControlCancel {
			text = "sciClaw is not currently running a background job that can be cancelled."
		}
		return jm.sendText(ctx, msg.Channel, msg.ChatID, text)
	}

	selected, err := selectActiveJob(active, control.TargetID, msg.Metadata["reply_message_id"])
	if err != nil {
		if control.TargetID == "" && len(active) > 1 && control.Kind == jobControlStatus {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, formatActiveJobList(snapshotActiveJobs(active)))
		}
		if control.TargetID == "" && len(active) > 1 && control.Kind == jobControlCancel {
			return jm.sendText(ctx, msg.Channel, msg.ChatID, formatCancelRequiresJobID(snapshotActiveJobs(active)))
		}
		return jm.sendText(ctx, msg.Channel, msg.ChatID, err.Error())
	}

	record := selected.snapshot()
	switch control.Kind {
	case jobControlStatus:
		return jm.sendText(ctx, msg.Channel, msg.ChatID, formatActiveJobStatus(record, false))
	case jobControlCancel:
		selected.cancel()
		return jm.sendText(ctx, msg.Channel, msg.ChatID, fmt.Sprintf("sciClaw is stopping job %s.\n\nAsk: %s", record.ShortID, fallbackAskSummary(record.AskSummary)))
	default:
		return nil
	}
}

func (jm *JobManager) runJob(ctx context.Context, target LoopTarget, msg bus.InboundMessage, active *activeJob) {
	targetKey := target.key()
	jobClass := active.snapshot().Class
	progress := jm.newProgressReporter(ctx, active)
	defer progress.finish()
	defer func() {
		jm.mu.Lock()
		if activeSet := jm.active[targetKey]; activeSet != nil {
			activeSet.clear(jobClass)
			if activeSet.empty() {
				delete(jm.active, targetKey)
			}
		}
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

func (jm *JobManager) nextJobID() (string, string) {
	n := atomic.AddUint64(&jm.counter, 1)
	return fmt.Sprintf("job-%d-%d", time.Now().UnixMilli(), n), "J" + strings.ToUpper(strconv.FormatUint(n, 36))
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

func classifyJobClass(msg bus.InboundMessage) JobClass {
	text := strings.ToLower(compactJobLine(discordMentionPattern.ReplaceAllString(msg.Content, "")))
	if text == "" {
		return JobClassWrite
	}
	for _, marker := range []string{
		".pdf", ".docx", ".pptx", ".xlsx", "./", "../", "/users/", "/home/", "\\",
		"workspace", "folder", "directory", "file", "files", "save", "write", "edit",
		"apply", "patch", "rename", "move", "copy", "delete", "remove", "run ", "exec",
		"shell", "terminal", "command", "docx", "pdf", "quarto", "pandoc",
	} {
		if strings.Contains(text, marker) {
			return JobClassWrite
		}
	}
	hasExternalCue := false
	for _, cue := range []string{
		"search", "look up", "look for", "find", "collect", "gather", "compare",
		"what is", "what are", "which", "who", "when", "where", "images", "image",
		"photos", "pictures", "species", "related probes", "probe", "research",
	} {
		if strings.Contains(text, cue) {
			hasExternalCue = true
			break
		}
	}
	if hasExternalCue {
		return JobClassExternalReadOnly
	}
	return JobClassWrite
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
	return fmt.Sprintf("**%s** · **%s** · %s", record.ShortID, humanizePhase(record.Phase), formatDiscordRelativeTimestamp(record.StartedAt))
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
	return fmt.Sprintf("Reply `%s %s` · `%s %s`", "status", record.ShortID, "cancel", record.ShortID)
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

func selectActiveJob(active []*activeJob, targetID, replyMessageID string) (*activeJob, error) {
	if len(active) == 0 {
		return nil, fmt.Errorf("sciClaw is not currently running a background job for this workspace.")
	}
	if targetID != "" {
		for _, job := range active {
			record := job.snapshot()
			if strings.EqualFold(record.ShortID, targetID) || strings.EqualFold(record.ID, targetID) {
				return job, nil
			}
		}
		return nil, fmt.Errorf("No active sciClaw job matches %s.", targetID)
	}
	if strings.TrimSpace(replyMessageID) != "" {
		for _, job := range active {
			if job.snapshot().StatusMessageID == strings.TrimSpace(replyMessageID) {
				return job, nil
			}
		}
	}
	if len(active) == 1 {
		return active[0], nil
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
	lines := []string{"**Multiple sciClaw jobs are active here**", ""}
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("- **%s** · %s · %s", record.ShortID, humanizePhase(record.Phase), formatDiscordRelativeTimestamp(record.StartedAt)))
		lines = append(lines, fmt.Sprintf("  %s", fallbackAskSummary(record.AskSummary)))
	}
	lines = append(lines, "", "Reply `status <job-id>` for details.")
	return strings.Join(lines, "\n")
}

func formatCancelRequiresJobID(records []JobRecord) string {
	lines := []string{"**Multiple sciClaw jobs are active here**", "Reply `cancel <job-id>`.", ""}
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("- **%s** · %s", record.ShortID, fallbackAskSummary(record.AskSummary)))
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
	case JobClassExternalReadOnly:
		lines = append(lines, "", "Another external research job will wait for an open slot.")
	default:
		lines = append(lines, "", "Another write-capable job will wait until this one finishes.")
	}
	lines = append(lines, "Reply `status <job-id>` or `cancel <job-id>` if you need the running job.")
	return strings.Join(lines, "\n")
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
