package game

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"uuid"
)

// --- DTOs (Data Transfer Objects from DB) ---

type AchCatalogRequirement struct {
	TargetValue int64  `json:"target_value"`
	ReqType     string `json:"req_type"`
	GroupID     int    `json:"group_id"`
}

type AchCatalogDTO struct {
	Requirements         []AchCatalogRequirement `json:"requirements"`
	ID                   uuid.UUID               `json:"id"`
	Code                 string                  `json:"code"`
	Title                string                  `json:"title"`
	Description          string                  `json:"description"`
	IsGlobalAnnouncement bool                    `json:"is_global_announcement"`
}

type AchProgressDTO struct {
	AchievementID uuid.UUID        `json:"achievement_id"`
	Code          string           `json:"code"`
	MaxProgress   map[string]int64 `json:"max_progress"`
	Progress      map[string]int64 `json:"progress"`
	IsUnlocked    bool             `json:"is_unlocked"`
}

type AchievementView struct {
	AchievementID uuid.UUID        `json:"achievement_id"`
	Code          string           `json:"code"`
	Progress      map[string]int64 `json:"progress"`
	MaxProgress   map[string]int64 `json:"max_progress"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	IsUnlocked    bool             `json:"is_unlocked"`
}

// --- Global Catalog Structs ---

type AchReqTarget struct {
	AchievementID uuid.UUID
	TargetValue   int64
	GroupID       int
	BitIndex      uint8
}

type AchReqGroupDef struct {
	GroupID    int
	TargetMask uint64
}

type AchievementDef struct {
	ID                   uuid.UUID
	Code                 string
	Title                string
	Description          string
	Groups               map[int]AchReqGroupDef
	MaxProgress          map[string]int64
	IsGlobalAnnouncement bool
}

type AchievementCatalog struct {
	Defs          map[uuid.UUID]*AchievementDef
	InvertedIndex map[string][]AchReqTarget
}

// BuildAchievementCatalog builds the shared read-only rulebook from AchCatalogDTOs
func BuildAchievementCatalog(defs []AchCatalogDTO) (*AchievementCatalog, error) {
	catalog := &AchievementCatalog{
		Defs:          make(map[uuid.UUID]*AchievementDef),
		InvertedIndex: make(map[string][]AchReqTarget),
	}

	for _, def := range defs {
		achDef := &AchievementDef{
			ID:                   def.ID,
			Code:                 def.Code,
			Title:                def.Title,
			Description:          def.Description,
			IsGlobalAnnouncement: def.IsGlobalAnnouncement,
			Groups:               make(map[int]AchReqGroupDef),
			MaxProgress:          make(map[string]int64),
		}

		groupBitCounters := make(map[int]uint8)

		for _, req := range def.Requirements {
			groupID := req.GroupID
			bitIndex := groupBitCounters[groupID]

			if bitIndex >= 64 {
				return nil, fmt.Errorf("achievement %s (%s) group %d exceeds 64 requirements", def.Code, def.ID, groupID)
			}

			reqKey := fmt.Sprintf("%d:%d", groupID, bitIndex)
			achDef.MaxProgress[reqKey] = req.TargetValue

			target := AchReqTarget{
				AchievementID: def.ID,
				GroupID:       groupID,
				BitIndex:      bitIndex,
				TargetValue:   req.TargetValue,
			}
			catalog.InvertedIndex[req.ReqType] = append(catalog.InvertedIndex[req.ReqType], target)

			group := achDef.Groups[groupID]
			group.GroupID = groupID
			group.TargetMask |= (1 << bitIndex)
			achDef.Groups[groupID] = group

			groupBitCounters[groupID]++
		}

		catalog.Defs[def.ID] = achDef
	}

	slog.Debug("achievement catalog loaded", "definitions", len(catalog.Defs), "index_keys", len(catalog.InvertedIndex))

	return catalog, nil
}

// --- Runtime Engine Structs ---

type PlayerProgress struct {
	sync.Mutex
	ReqValues  map[string]int64
	GroupMasks map[uuid.UUID]map[int]uint64
	Unlocked   map[uuid.UUID]bool
}

type AchievementEvent struct {
	PlayerIDs []string
	Key       string
	Amount    int64
}

type AchievementEngine struct {
	sync.RWMutex
	invertedIndex   map[string][]AchReqTarget
	catalog         map[uuid.UUID]*AchievementDef
	activeListeners map[string]*int64
	playerSessions  map[string]*PlayerProgress
	serverEventChan chan ServerEvent
	eventChan       chan AchievementEvent
	gameDestroyChan <-chan struct{}
}

func NewAchievementEngine(catalog *AchievementCatalog, destroyChan <-chan struct{}, serverEventChan chan ServerEvent) *AchievementEngine {
	return &AchievementEngine{
		catalog:         catalog.Defs,
		invertedIndex:   catalog.InvertedIndex,
		activeListeners: make(map[string]*int64),
		playerSessions:  make(map[string]*PlayerProgress),
		eventChan:       make(chan AchievementEvent, 256),
		gameDestroyChan: destroyChan,
		serverEventChan: serverEventChan,
	}
}

func (e *AchievementEngine) StartEventProcessor() {
	slog.Debug("achievement event processor started")
	for {
		select {
		case <-e.gameDestroyChan:
			slog.Debug("achievement event processor stopped, game destroyed")
			return
		case event, ok := <-e.eventChan:
			if !ok {
				return
			}
			e.evaluateEvent(event)
		}
	}
}

func (e *AchievementEngine) LoadPlayerAchievements(playerID string, states []AchProgressDTO) {
	e.Lock()
	defer e.Unlock()

	p := &PlayerProgress{
		ReqValues:  make(map[string]int64),
		GroupMasks: make(map[uuid.UUID]map[int]uint64),
		Unlocked:   make(map[uuid.UUID]bool),
	}

	for _, dto := range states {
		achDef, exists := e.catalog[dto.AchievementID]
		if !exists {
			continue
		}

		if dto.IsUnlocked {
			p.Unlocked[dto.AchievementID] = true
			continue
		}

		e.registerActiveListeners(achDef)

		if dto.Progress != nil {
			if p.GroupMasks[dto.AchievementID] == nil {
				p.GroupMasks[dto.AchievementID] = make(map[int]uint64)
			}

			for key, count := range dto.Progress {
				reqKey := fmt.Sprintf("%s:%s", dto.AchievementID.String(), key)
				p.ReqValues[reqKey] = count

				var groupID int
				var bitIndex uint8
				if _, err := fmt.Sscanf(key, "%d:%d", &groupID, &bitIndex); err == nil {
					targetVal, ok := e.getTargetValue(dto.AchievementID, groupID, bitIndex)
					if ok && count >= targetVal {
						p.GroupMasks[dto.AchievementID][groupID] |= (1 << bitIndex)
					}
				}
			}
		}
	}

	e.playerSessions[playerID] = p
}

func (e *AchievementEngine) PublishEvent(event AchievementEvent) {
	event.Key = strings.ToLower(event.Key)
	e.RLock()
	listenerPtr := e.activeListeners[event.Key]
	var listenerCount int64
	if listenerPtr != nil {
		listenerCount = *listenerPtr
	}
	e.RUnlock()

	if listenerCount == 0 {
		slog.Debug("achievement event dropped, no listeners", "key", event.Key)
		return
	}

	select {
	case e.eventChan <- event:
	default:
		slog.Warn("achievement event dropped, buffer full", "key", event.Key)
	}
}

func (e *AchievementEngine) evaluateEvent(event AchievementEvent) {
	e.RLock()
	targets, exists := e.invertedIndex[event.Key]
	e.RUnlock()

	if !exists || len(targets) == 0 {
		slog.Debug("no achievement targets for key", "key", event.Key)
		return
	}

	slog.Debug("evaluating achievement event", "key", event.Key, "targets", len(targets), "players", event.PlayerIDs)

	for _, target := range targets {
		e.RLock()
		achDef, exists := e.catalog[target.AchievementID]
		e.RUnlock()
		if !exists {
			slog.Error("achievement target not in catalog", "achievement_id", target.AchievementID)
			continue
		}

		for _, playerID := range event.PlayerIDs {
			e.RLock()
			p, exists := e.playerSessions[playerID]
			e.RUnlock()

			if !exists {
				slog.Error("achievement player session not found", "player_id", playerID)
				continue
			}

			// Notify after the player lock is released: the send can block
			// while the server listener is busy, and the listener itself
			// needs that lock (ExportPlayerProgress on disconnect).
			if e.checkAndApplyProgress(playerID, p, achDef, target, event.Key, event.Amount) {
				e.onAchievementUnlocked(playerID, achDef)
			}
		}
	}
}

// checkAndApplyProgress applies one event to the player's progress and
// reports whether it just unlocked def. It must not notify the server
// itself, because it runs with the player's progress lock held.
func (e *AchievementEngine) checkAndApplyProgress(playerID string, p *PlayerProgress, def *AchievementDef, target AchReqTarget, eventKey string, amount int64) (unlocked bool) {
	p.Lock()
	defer p.Unlock()

	if p.Unlocked[def.ID] {
		return false
	}

	reqKey := fmt.Sprintf("%s:%d:%d", target.AchievementID, target.GroupID, target.BitIndex)

	switch {
	case strings.HasPrefix(eventKey, "time:"):
		p.ReqValues[reqKey] = amount
		timeMet := amount <= target.TargetValue

		if p.GroupMasks[def.ID] == nil {
			p.GroupMasks[def.ID] = make(map[int]uint64)
		}

		if timeMet {
			p.GroupMasks[def.ID][target.GroupID] |= (1 << target.BitIndex)
		} else {
			p.GroupMasks[def.ID][target.GroupID] &^= (1 << target.BitIndex)
		}

		groupDef := def.Groups[target.GroupID]
		currentMask := p.GroupMasks[def.ID][target.GroupID]

		if currentMask == groupDef.TargetMask {
			p.Unlocked[def.ID] = true
			unlocked = true
		} else {
			p.GroupMasks[def.ID][target.GroupID] = 0
			groupPrefix := fmt.Sprintf("%s:%d:", def.ID.String(), target.GroupID)
			for k := range p.ReqValues {
				if strings.HasPrefix(k, groupPrefix) {
					delete(p.ReqValues, k)
				}
			}
			slog.Debug("achievement time requirement failed, group reset", "achievement", def.Code, "group", target.GroupID)
		}

	case strings.HasPrefix(eventKey, "game:"):
		groupDef := def.Groups[target.GroupID]
		if p.GroupMasks[def.ID] == nil {
			p.GroupMasks[def.ID] = make(map[int]uint64)
		}

		p.GroupMasks[def.ID][target.GroupID] |= (1 << target.BitIndex)

		currentMask := p.GroupMasks[def.ID][target.GroupID]

		if currentMask == groupDef.TargetMask {
			p.Unlocked[def.ID] = true
			unlocked = true
		} else {
			p.GroupMasks[def.ID][target.GroupID] = 0
			groupPrefix := fmt.Sprintf("%s:%d:", def.ID.String(), target.GroupID)
			for k := range p.ReqValues {
				if strings.HasPrefix(k, groupPrefix) {
					delete(p.ReqValues, k)
				}
			}
			slog.Debug("achievement game requirement failed, group reset", "achievement", def.Code, "group", target.GroupID)
		}

	default:
		p.ReqValues[reqKey] += amount
		newCount := p.ReqValues[reqKey]
		conditionMet := newCount >= target.TargetValue

		slog.Debug("achievement progress", "player_id", playerID, "achievement", def.Code,
			"group", target.GroupID, "bit", target.BitIndex, "progress", newCount, "target", target.TargetValue)

		if conditionMet {
			if p.GroupMasks[def.ID] == nil {
				p.GroupMasks[def.ID] = make(map[int]uint64)
			}

			p.GroupMasks[def.ID][target.GroupID] |= (1 << target.BitIndex)

			groupDef := def.Groups[target.GroupID]
			if p.GroupMasks[def.ID][target.GroupID] == groupDef.TargetMask {
				p.Unlocked[def.ID] = true
				unlocked = true
			}
		}
	}
	return unlocked
}

func (e *AchievementEngine) isAchievementComplete(def *AchievementDef, p *PlayerProgress) bool {
	for groupID, groupDef := range def.Groups {
		currentMask := p.GroupMasks[def.ID][groupID]
		if currentMask == groupDef.TargetMask {
			return true
		}
	}
	return false
}

type AchievementUnlockedPayload struct {
	Message              string
	IsGlobalAnnouncement bool
}

func (e *AchievementEngine) onAchievementUnlocked(playerID string, def *AchievementDef) {
	message := fmt.Sprintf("[Achievement]: %s unlocked %s!", playerID, def.Title)
	slog.Info("achievement unlocked", "player_id", playerID, "achievement", def.Code, "achievement_id", def.ID)
	ev := ServerEvent{
		Type:     EventTypeAchievementUnlocked,
		PlayerID: playerID,
		Value:    def.ID.String(),
		Object: AchievementUnlockedPayload{
			Message:              message,
			IsGlobalAnnouncement: def.IsGlobalAnnouncement,
		},
	}
	select {
	case e.serverEventChan <- ev:
	case <-e.gameDestroyChan:
		// Game is gone and the server listener with it; don't block forever.
		slog.Warn("achievement unlock dropped, game destroyed", "achievement", def.Code, "player_id", playerID)
	}
}

func (e *AchievementEngine) registerActiveListeners(def *AchievementDef) {
	for reqType, targets := range e.invertedIndex {
		for _, target := range targets {
			if target.AchievementID == def.ID {
				if e.activeListeners[reqType] == nil {
					var count int64
					e.activeListeners[reqType] = &count
				}
				*e.activeListeners[reqType]++
			}
		}
	}
}

func (e *AchievementEngine) getTargetValue(achID uuid.UUID, groupID int, bitIndex uint8) (int64, bool) {
	for _, targets := range e.invertedIndex {
		for _, target := range targets {
			if target.AchievementID == achID && target.GroupID == groupID && target.BitIndex == bitIndex {
				return target.TargetValue, true
			}
		}
	}
	return 0, false
}

func (e *AchievementEngine) ExportPlayerProgress(playerID string) []AchProgressDTO {
	e.RLock()
	p, exists := e.playerSessions[playerID]
	e.RUnlock()

	if !exists {
		return nil
	}

	p.Lock()
	defer p.Unlock()

	allAchIDs := make(map[uuid.UUID]bool)
	for key := range p.ReqValues {
		if parts := strings.SplitN(key, ":", 2); len(parts) == 2 {
			if id, err := uuid.Parse(parts[0]); err == nil {
				allAchIDs[id] = true
			}
		}
	}
	for achID := range p.GroupMasks {
		allAchIDs[achID] = true
	}
	for achID := range p.Unlocked {
		allAchIDs[achID] = true
	}

	progressMap := make(map[uuid.UUID]map[string]int64)
	for achID := range allAchIDs {
		progressMap[achID] = make(map[string]int64)
	}

	for key, count := range p.ReqValues {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		achID, err := uuid.Parse(parts[0])
		if err != nil {
			continue
		}
		progressMap[achID][parts[1]] = count
	}

	var dtoList []AchProgressDTO
	for achID, progData := range progressMap {
		if progData == nil {
			progData = make(map[string]int64)
		}

		dtoList = append(dtoList, AchProgressDTO{
			AchievementID: achID,
			IsUnlocked:    p.Unlocked[achID],
			Progress:      progData,
		})
	}

	return dtoList
}

func (e *AchievementEngine) RemovePlayerSession(playerID string) {
	e.Lock()
	defer e.Unlock()
	delete(e.playerSessions, playerID)
}

func (e *AchievementEngine) GetAllDefinitions() []*AchievementDef {
	e.RLock()
	defer e.RUnlock()

	defs := make([]*AchievementDef, 0, len(e.catalog))
	for _, def := range e.catalog {
		defs = append(defs, def)
	}
	return defs
}

func (e *AchievementEngine) IsUnlocked(playerID string, achID uuid.UUID) bool {
	e.RLock()
	p, exists := e.playerSessions[playerID]
	e.RUnlock()

	if !exists {
		return false
	}

	p.Lock()
	defer p.Unlock()
	return p.Unlocked[achID]
}

func (e *AchievementEngine) GetProgress(playerID string, achID uuid.UUID) map[string]int64 {
	e.RLock()
	p, exists := e.playerSessions[playerID]
	e.RUnlock()

	progress := make(map[string]int64)
	if !exists {
		return progress
	}

	p.Lock()
	defer p.Unlock()

	prefix := achID.String() + ":"
	for k, v := range p.ReqValues {
		if strings.HasPrefix(k, prefix) {
			subKey := strings.TrimPrefix(k, prefix)
			progress[subKey] = v
		}
	}
	return progress
}

func (e *AchievementEngine) BuildClientAchievementPayload(playerID string) []AchievementView {
	var viewList []AchievementView

	for _, def := range e.GetAllDefinitions() {
		isUnlocked := e.IsUnlocked(playerID, def.ID)
		progress := e.GetProgress(playerID, def.ID)

		viewList = append(viewList, AchievementView{
			AchievementID: def.ID,
			Code:          def.Code,
			Title:         def.Title,
			Description:   def.Description,
			IsUnlocked:    isUnlocked,
			Progress:      progress,
			MaxProgress:   def.MaxProgress,
		})
	}
	return viewList
}
