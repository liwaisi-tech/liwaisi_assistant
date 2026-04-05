package persist

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Compile-time interface assertions.
var (
	_ SessionRepository      = (*MemorySessionRepository)(nil)
	_ EventRepository        = (*MemoryEventRepository)(nil)
	_ LedgerRepository       = (*MemoryLedgerRepository)(nil)
	_ FlowRepository         = (*MemoryFlowRepository)(nil)
	_ IntelligenceRepository = (*MemoryIntelligenceRepository)(nil)
	_ HITLRepository         = (*MemoryHITLRepository)(nil)
)

const defaultPageLimit = 100

// ---- MemorySessionRepository ----

// MemorySessionRepository is a thread-safe in-memory SessionRepository.
type MemorySessionRepository struct {
	sessions map[string]*SessionRecord
	mu       sync.RWMutex
}

// NewMemorySessionRepository creates an empty MemorySessionRepository.
func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{sessions: make(map[string]*SessionRecord)}
}

// Reset clears all data (test helper).
func (r *MemorySessionRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = make(map[string]*SessionRecord)
}

// Len returns the number of stored sessions (test helper).
func (r *MemorySessionRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func (r *MemorySessionRepository) Create(ctx context.Context, session *SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[session.ID]; exists {
		return ErrSessionExists
	}
	cp := *session
	if cp.Messages != nil {
		msgs := make([]*MessageRecord, len(cp.Messages))
		for i, m := range cp.Messages {
			mc := *m
			msgs[i] = &mc
		}
		cp.Messages = msgs
	}
	r.sessions[session.ID] = &cp
	return nil
}

func (r *MemorySessionRepository) Get(ctx context.Context, sessionID string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *MemorySessionRepository) GetByUserID(ctx context.Context, userID string) ([]*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*SessionRecord
	for _, s := range r.sessions {
		if s.UserID == userID {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (r *MemorySessionRepository) AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	if s.State == SessionClosed || s.State == SessionExpired {
		return ErrSessionClosed
	}
	mc := *msg
	s.Messages = append(s.Messages, &mc)
	return nil
}

func (r *MemorySessionRepository) UpdateState(ctx context.Context, sessionID string, state SessionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.State = state
	return nil
}

func (r *MemorySessionRepository) Touch(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.LastActivityAt = time.Now()
	return nil
}

func (r *MemorySessionRepository) Close(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.State = SessionClosed
	now := time.Now()
	s.ClosedAt = &now
	return nil
}

func (r *MemorySessionRepository) ListExpired(ctx context.Context, before time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var ids []string
	for _, s := range r.sessions {
		if s.LastActivityAt.Before(before) && s.State == SessionActive {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

func (r *MemorySessionRepository) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}
	delete(r.sessions, sessionID)
	return nil
}

func (r *MemorySessionRepository) ListByUserID(ctx context.Context, userID string, opts *SessionListOpts) (*Page[*SessionListItem], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var items []*SessionListItem
	for _, s := range r.sessions {
		if s.UserID != userID || s.DeletedAt != nil || s.State == SessionExpired {
			continue
		}
		preview := ""
		if len(s.Messages) > 0 {
			last := s.Messages[len(s.Messages)-1]
			preview = last.Content
			if len(preview) > 120 {
				preview = preview[:120]
			}
		}
		items = append(items, &SessionListItem{
			ID:                  s.ID,
			Title:               s.Title,
			State:               s.State,
			LastMessagePreview:  preview,
			LastActivityAt:      s.LastActivityAt,
			CreatedAt:           s.CreatedAt,
			MessageCount:        len(s.Messages),
			ForkedFromSessionID: s.ForkedFromSessionID,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].LastActivityAt.After(items[j].LastActivityAt)
	})

	limit := 50
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}
	if limit > len(items) {
		limit = len(items)
	}

	return &Page[*SessionListItem]{Items: items[:limit], HasMore: len(items) > limit}, nil
}

func (r *MemorySessionRepository) UpdateTitle(ctx context.Context, sessionID string, title string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.Title = title
	return nil
}

func (r *MemorySessionRepository) SoftDelete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	now := time.Now()
	s.DeletedAt = &now
	return nil
}

func (r *MemorySessionRepository) ForkSession(ctx context.Context, newSessionID string, sourceSessionID string, messageIndex int, userID string, channel string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.sessions[sourceSessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Validate message index
	if messageIndex < 0 || messageIndex >= len(src.Messages) {
		return nil, ErrInvalidInput
	}

	// Copy messages up to messageIndex (inclusive)
	now := time.Now()
	copiedMessages := make([]*MessageRecord, messageIndex+1)
	for i := 0; i <= messageIndex; i++ {
		mc := *src.Messages[i]
		mc.SessionID = newSessionID
		copiedMessages[i] = &mc
	}

	forked := &SessionRecord{
		ID:                  newSessionID,
		UserID:              userID,
		Channel:             channel,
		State:               SessionActive,
		CreatedAt:           now,
		LastActivityAt:      now,
		Messages:            copiedMessages,
		Title:               "Fork of: " + src.Title,
		ForkedFromSessionID: sourceSessionID,
		ForkMessageCount:    messageIndex + 1,
	}
	r.sessions[newSessionID] = forked
	return forked, nil
}

// ---- MemoryEventRepository ----

// MemoryEventRepository is a thread-safe in-memory EventRepository (append-only).
type MemoryEventRepository struct {
	events []*EventRecord
	mu     sync.RWMutex
}

// NewMemoryEventRepository creates an empty MemoryEventRepository.
func NewMemoryEventRepository() *MemoryEventRepository {
	return &MemoryEventRepository{}
}

// Reset clears all data (test helper).
func (r *MemoryEventRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

// Len returns the number of stored events (test helper).
func (r *MemoryEventRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.events)
}

func (r *MemoryEventRepository) Append(ctx context.Context, events ...*EventRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	for _, e := range events {
		if e == nil {
			return ErrEventAppendFailed
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range events {
		cp := *e
		r.events = append(r.events, &cp)
	}
	return nil
}

func (r *MemoryEventRepository) QueryBySession(ctx context.Context, sessionID string, opts *EventQueryOpts) (*Page[*EventRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []*EventRecord
	for _, e := range r.events {
		if e.SessionID == sessionID {
			filtered = append(filtered, e)
		}
	}
	return paginate(filtered, opts), nil
}

func (r *MemoryEventRepository) QueryByCPN(ctx context.Context, cpnID string, opts *EventQueryOpts) (*Page[*EventRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []*EventRecord
	for _, e := range r.events {
		if e.CPNID == cpnID {
			filtered = append(filtered, e)
		}
	}
	return paginate(filtered, opts), nil
}

func (r *MemoryEventRepository) QueryByType(ctx context.Context, eventType string, from, to time.Time, opts *EventQueryOpts) (*Page[*EventRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []*EventRecord
	for _, e := range r.events {
		if e.Type == eventType && !e.Timestamp.Before(from) && !e.Timestamp.After(to) {
			filtered = append(filtered, e)
		}
	}
	return paginate(filtered, opts), nil
}

func (r *MemoryEventRepository) Count(ctx context.Context, opts *EventQueryOpts) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if opts == nil {
		return int64(len(r.events)), nil
	}
	var count int64
	for _, e := range r.events {
		if matchEventOpts(e, opts) {
			count++
		}
	}
	return count, nil
}

func matchEventOpts(e *EventRecord, opts *EventQueryOpts) bool {
	if opts.SessionID != "" && e.SessionID != opts.SessionID {
		return false
	}
	if opts.CPNID != "" && e.CPNID != opts.CPNID {
		return false
	}
	if opts.EventType != "" && e.Type != opts.EventType {
		return false
	}
	if !opts.From.IsZero() && e.Timestamp.Before(opts.From) {
		return false
	}
	if !opts.To.IsZero() && e.Timestamp.After(opts.To) {
		return false
	}
	return true
}

func paginate(items []*EventRecord, opts *EventQueryOpts) *Page[*EventRecord] {
	limit := defaultPageLimit
	offset := 0
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Cursor != "" {
			if o, err := strconv.Atoi(opts.Cursor); err == nil {
				offset = o
			}
		}
	}

	if offset >= len(items) {
		return &Page[*EventRecord]{}
	}

	end := offset + limit
	hasMore := false
	if end < len(items) {
		hasMore = true
	} else {
		end = len(items)
	}

	page := &Page[*EventRecord]{
		Items:   items[offset:end],
		HasMore: hasMore,
	}
	if hasMore {
		page.NextCursor = strconv.Itoa(end)
	}
	return page
}

// ---- MemoryLedgerRepository ----

// MemoryLedgerRepository is a thread-safe in-memory LedgerRepository.
type MemoryLedgerRepository struct {
	ledgers map[string]*LedgerRecord
	mu      sync.RWMutex
}

// NewMemoryLedgerRepository creates an empty MemoryLedgerRepository.
func NewMemoryLedgerRepository() *MemoryLedgerRepository {
	return &MemoryLedgerRepository{ledgers: make(map[string]*LedgerRecord)}
}

// Reset clears all data (test helper).
func (r *MemoryLedgerRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledgers = make(map[string]*LedgerRecord)
}

// Len returns the number of stored ledger records (test helper).
func (r *MemoryLedgerRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.ledgers)
}

func (r *MemoryLedgerRepository) Record(ctx context.Context, rec *LedgerRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.ledgers[rec.SessionID]
	if ok {
		existing.InputTokens += rec.InputTokens
		existing.OutputTokens += rec.OutputTokens
		existing.Calls += rec.Calls
		existing.TotalCostUSD += rec.TotalCostUSD
		existing.LastUpdated = time.Now()
	} else {
		cp := *rec
		cp.LastUpdated = time.Now()
		r.ledgers[rec.SessionID] = &cp
	}
	return nil
}

func (r *MemoryLedgerRepository) GetBySession(ctx context.Context, sessionID string) (*LedgerRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.ledgers[sessionID]
	if !ok {
		return nil, ErrLedgerNotFound
	}
	cp := *l
	return &cp, nil
}

func (r *MemoryLedgerRepository) QueryByDate(ctx context.Context, date time.Time) ([]*LedgerRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	y, m, d := date.Date()
	var result []*LedgerRecord
	for _, l := range r.ledgers {
		ly, lm, ld := l.LastUpdated.Date()
		if ly == y && lm == m && ld == d {
			cp := *l
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (r *MemoryLedgerRepository) SetDailyTotal(ctx context.Context, sessionID string, _ time.Time, totalUSD float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.ledgers[sessionID]
	if !ok {
		return ErrLedgerNotFound
	}
	l.DailyTotalUSD = totalUSD
	return nil
}

func (r *MemoryLedgerRepository) AggregateByDateRange(ctx context.Context, from, to time.Time) (*LedgerAggregate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	agg := &LedgerAggregate{}
	for _, l := range r.ledgers {
		if !l.LastUpdated.Before(from) && !l.LastUpdated.After(to) {
			agg.TotalInputTokens += l.InputTokens
			agg.TotalOutputTokens += l.OutputTokens
			agg.TotalCalls += l.Calls
			agg.TotalCostUSD += l.TotalCostUSD
		}
	}
	return agg, nil
}

// ---- MemoryFlowRepository ----

// MemoryFlowRepository is a thread-safe in-memory FlowRepository with soft-delete.
type MemoryFlowRepository struct {
	flows map[string]*FlowRecord
	mu    sync.RWMutex
}

// NewMemoryFlowRepository creates an empty MemoryFlowRepository.
func NewMemoryFlowRepository() *MemoryFlowRepository {
	return &MemoryFlowRepository{flows: make(map[string]*FlowRecord)}
}

// Reset clears all data (test helper).
func (r *MemoryFlowRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flows = make(map[string]*FlowRecord)
}

// Len returns the number of stored flows including soft-deleted (test helper).
func (r *MemoryFlowRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.flows)
}

func (r *MemoryFlowRepository) Save(ctx context.Context, flow *FlowRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *flow
	r.flows[flow.Hash] = &cp
	return nil
}

func (r *MemoryFlowRepository) GetByHash(ctx context.Context, hash string) (*FlowRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.flows[hash]
	if !ok || f.DeletedAt != nil {
		return nil, ErrFlowNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *MemoryFlowRepository) List(ctx context.Context, opts *FlowListOpts) (*Page[*FlowRecord], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]*FlowRecord, 0, len(r.flows))
	for _, f := range r.flows {
		if f.DeletedAt != nil {
			continue
		}
		if opts != nil && opts.Role != "" && f.Role != opts.Role {
			continue
		}
		items = append(items, f)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

	limit := defaultPageLimit
	offset := 0
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Cursor != "" {
			if o, err := strconv.Atoi(opts.Cursor); err == nil {
				offset = o
			}
		}
	}

	if offset >= len(items) {
		return &Page[*FlowRecord]{}, nil
	}

	end := offset + limit
	hasMore := false
	if end < len(items) {
		hasMore = true
	} else {
		end = len(items)
	}

	page := &Page[*FlowRecord]{
		Items:   items[offset:end],
		HasMore: hasMore,
	}
	if hasMore {
		page.NextCursor = strconv.Itoa(end)
	}
	return page, nil
}

func (r *MemoryFlowRepository) UpdateStats(ctx context.Context, hash string, stats *FlowStats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.flows[hash]
	if !ok || f.DeletedAt != nil {
		return ErrFlowNotFound
	}
	f.Stats = *stats
	f.UpdatedAt = time.Now()
	return nil
}

func (r *MemoryFlowRepository) Delete(ctx context.Context, hash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.flows[hash]
	if !ok {
		return ErrFlowNotFound
	}
	now := time.Now()
	f.DeletedAt = &now
	return nil
}

// ---- MemoryIntelligenceRepository ----

// MemoryIntelligenceRepository is a thread-safe in-memory IntelligenceRepository.
type MemoryIntelligenceRepository struct {
	records []*ExecutionRecord
	mu      sync.RWMutex
}

// NewMemoryIntelligenceRepository creates an empty MemoryIntelligenceRepository.
func NewMemoryIntelligenceRepository() *MemoryIntelligenceRepository {
	return &MemoryIntelligenceRepository{}
}

// Reset clears all data (test helper).
func (r *MemoryIntelligenceRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}

// Len returns the number of stored execution records (test helper).
func (r *MemoryIntelligenceRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.records)
}

func (r *MemoryIntelligenceRepository) RecordExecution(ctx context.Context, rec *ExecutionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *rec
	r.records = append(r.records, &cp)
	return nil
}

func (r *MemoryIntelligenceRepository) QueryByRole(ctx context.Context, role string, from, to time.Time) ([]*ExecutionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*ExecutionRecord
	for _, rec := range r.records {
		if rec.CPNRole == role && !rec.StartedAt.Before(from) && !rec.StartedAt.After(to) {
			cp := *rec
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (r *MemoryIntelligenceRepository) Aggregate(ctx context.Context, role string, from, to time.Time) (*RankingMetrics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matching []*ExecutionRecord
	for _, rec := range r.records {
		if rec.CPNRole == role && !rec.StartedAt.Before(from) && !rec.StartedAt.After(to) {
			matching = append(matching, rec)
		}
	}

	if len(matching) == 0 {
		return &RankingMetrics{Role: role}, nil
	}

	n := float64(len(matching))
	m := &RankingMetrics{
		Role:           role,
		ExecutionCount: int64(len(matching)),
	}

	var totalLLM, totalTool, totalDur float64
	var totalCost float64
	var successes float64
	for _, rec := range matching {
		totalLLM += float64(rec.LLMCalls)
		totalTool += float64(rec.ToolCalls)
		totalCost += rec.TotalCostUSD
		totalDur += float64(rec.DurationMs)
		if rec.Success {
			successes++
		}
	}

	m.AvgLLMCalls = totalLLM / n
	m.AvgToolCalls = totalTool / n
	m.AvgCostUSD = totalCost / n
	m.AvgDurationMs = totalDur / n
	m.SuccessRate = successes / n
	return m, nil
}

func (r *MemoryIntelligenceRepository) TopFlows(ctx context.Context, n int) ([]*RankedFlow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	type flowAgg struct {
		role       string
		count      int64
		successSum float64
		costSum    float64
		durSum     int64
	}

	byHash := make(map[string]*flowAgg)
	for _, rec := range r.records {
		key := rec.CPNID
		a, ok := byHash[key]
		if !ok {
			a = &flowAgg{role: rec.CPNRole}
			byHash[key] = a
		}
		a.count++
		if rec.Success {
			a.successSum++
		}
		a.costSum += rec.TotalCostUSD
		a.durSum += rec.DurationMs
	}

	ranked := make([]*RankedFlow, 0, len(byHash))
	for hash, a := range byHash {
		successRate := a.successSum / float64(a.count)
		avgCost := a.costSum / float64(a.count)
		avgDur := a.durSum / a.count
		score := successRate*100 - avgCost*10
		ranked = append(ranked, &RankedFlow{
			Hash:  hash,
			Role:  a.role,
			Score: score,
			Stats: FlowStats{
				ExecutionCount: a.count,
				SuccessRate:    successRate,
				AvgCostUSD:     avgCost,
				AvgDurationMs:  avgDur,
			},
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	if n > 0 && n < len(ranked) {
		ranked = ranked[:n]
	}
	return ranked, nil
}

// ---- MemoryHITLRepository ----

// MemoryHITLRepository is a thread-safe in-memory HITLRepository.
type MemoryHITLRepository struct {
	requests []*HITLPendingRequest
	mu       sync.RWMutex
}

// NewMemoryHITLRepository creates an empty MemoryHITLRepository.
func NewMemoryHITLRepository() *MemoryHITLRepository {
	return &MemoryHITLRepository{}
}

// Reset clears all data (test helper).
func (r *MemoryHITLRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = nil
}

// Len returns the number of stored pending requests (test helper).
func (r *MemoryHITLRepository) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.requests)
}

func (r *MemoryHITLRepository) Enqueue(ctx context.Context, req *HITLPendingRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *req
	r.requests = append(r.requests, &cp)
	return nil
}

func (r *MemoryHITLRepository) Dequeue(ctx context.Context, sessionID, transitionID string) (*HITLPendingRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, req := range r.requests {
		if req.SessionID == sessionID && req.TransitionID == transitionID {
			r.requests = append(r.requests[:i], r.requests[i+1:]...)
			return req, nil
		}
	}
	return nil, ErrHITLNotFound
}

func (r *MemoryHITLRepository) ListPending(ctx context.Context, sessionID string) ([]*HITLPendingRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*HITLPendingRequest
	for _, req := range r.requests {
		if req.SessionID == sessionID {
			cp := *req
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (r *MemoryHITLRepository) Expire(ctx context.Context, olderThan time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var kept []*HITLPendingRequest
	var removed int64
	for _, req := range r.requests {
		if req.ExpiresAt.Before(cutoff) {
			removed++
		} else {
			kept = append(kept, req)
		}
	}
	r.requests = kept
	return removed, nil
}
