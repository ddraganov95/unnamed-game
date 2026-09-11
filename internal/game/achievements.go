package game

import (
	"fmt"
	"log"
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
		log.Printf("[ACHIEVEMENT DEBUG] Loaded Definition: %s (%s) with %d requirement groups", def.Code, def.ID, len(achDef.Groups))
	}

	for key, targets := range catalog.InvertedIndex {
		log.Printf("[ACHIEVEMENT DEBUG] InvertedIndex Registered Key '%s' -> %d Target(s)", key, len(targets))
	}

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
	log.Printf("[ACHIEVEMENT DEBUG] Engine Event Processor Started")
	for {
		select {
		case <-e.gameDestroyChan:
			log.Printf("[ACHIEVEMENT DEBUG] Engine Event Processor Stopping (Game Destroyed)")
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
		log.Printf("[ACHIEVEMENT DEBUG] PublishEvent DROPPED: Key '%s' has 0 active listeners", event.Key)
		return
	}

	select {
	case e.eventChan <- event:
		log.Printf("[ACHIEVEMENT DEBUG] PublishEvent QUEUED: Key '%s' | Amount: %d | Target Players: %v", event.Key, event.Amount, event.PlayerIDs)
	default:
		log.Printf("[ACHIEVEMENT DEBUG] PublishEvent DROPPED: Buffer Full for Key '%s'", event.Key)
	}
}

func (e *AchievementEngine) evaluateEvent(event AchievementEvent) {
	e.RLock()
	targets, exists := e.invertedIndex[event.Key]
	e.RUnlock()

	if !exists || len(targets) == 0 {
		log.Printf("[ACHIEVEMENT DEBUG] evaluateEvent: Key '%s' not found in InvertedIndex", event.Key)
		return
	}

	log.Printf("[ACHIEVEMENT DEBUG] evaluateEvent: Processing Key '%s' matching %d targets for players %v", event.Key, len(targets), event.PlayerIDs)

	for _, target := range targets {
		e.RLock()
		achDef, exists := e.catalog[target.AchievementID]
		e.RUnlock()
		if !exists {
			log.Printf("[ACHIEVEMENT DEBUG] Target Achievement ID %s not found in Defs catalog", target.AchievementID)
			continue
		}

		for _, playerID := range event.PlayerIDs {
			e.RLock()
			p, exists := e.playerSessions[playerID]
			e.RUnlock()

			if !exists {
				log.Printf("[ACHIEVEMENT DEBUG] Player '%s' not found in active playerSessions", playerID)
				continue
			}

			e.checkAndApplyProgress(playerID, p, achDef, target, event.Key, event.Amount)
		}
	}
}

func (e *AchievementEngine) checkAndApplyProgress(playerID string, p *PlayerProgress, def *AchievementDef, target AchReqTarget, eventKey string, amount int64) {
	p.Lock()
	defer p.Unlock()

	if p.Unlocked[def.ID] {
		return
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
			e.onAchievementUnlocked(playerID, def)
		} else {
			p.GroupMasks[def.ID][target.GroupID] = 0
			groupPrefix := fmt.Sprintf("%s:%d:", def.ID.String(), target.GroupID)
			for k := range p.ReqValues {
				if strings.HasPrefix(k, groupPrefix) {
					delete(p.ReqValues, k)
				}
			}
			log.Printf("[ACHIEVEMENT DEBUG] TIME failed for group %d on achievement %s. Group reset.", target.GroupID, def.Code)
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
			e.onAchievementUnlocked(playerID, def)
		} else {
			p.GroupMasks[def.ID][target.GroupID] = 0
			groupPrefix := fmt.Sprintf("%s:%d:", def.ID.String(), target.GroupID)
			for k := range p.ReqValues {
				if strings.HasPrefix(k, groupPrefix) {
					delete(p.ReqValues, k)
				}
			}
			log.Printf("[ACHIEVEMENT DEBUG] GAME evaluation failed for group %d on achievement %s. Group reset.", target.GroupID, def.Code)
		}

	default:
		p.ReqValues[reqKey] += amount
		newCount := p.ReqValues[reqKey]
		conditionMet := newCount >= target.TargetValue

		log.Printf("[ACHIEVEMENT DEBUG] Player '%s' | Cumulative Ach: %s | Group: %d | Bit: %d | Progress: %d/%d",
			playerID, def.Code, target.GroupID, target.BitIndex, newCount, target.TargetValue)

		if conditionMet {
			if p.GroupMasks[def.ID] == nil {
				p.GroupMasks[def.ID] = make(map[int]uint64)
			}

			p.GroupMasks[def.ID][target.GroupID] |= (1 << target.BitIndex)

			groupDef := def.Groups[target.GroupID]
			if p.GroupMasks[def.ID][target.GroupID] == groupDef.TargetMask {
				p.Unlocked[def.ID] = true
				e.onAchievementUnlocked(playerID, def)
			}
		}
	}
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
	log.Printf("[Achievement] Player '%s' unlocked '%s' (%s)\n", playerID, def.Code, def.ID)
	log.Printf("[ACHIEVEMENT DEBUG] Checking def fields: Code=%s, Title=%s, ID=%v, IsGlobal=%t\n", def.Code, def.Title, def.ID, def.IsGlobalAnnouncement)
	e.serverEventChan <- ServerEvent{
		Type:     EventTypeAchievementUnlocked,
		PlayerID: playerID,
		Value:    def.ID.String(),
		Object: AchievementUnlockedPayload{
			Message:              message,
			IsGlobalAnnouncement: def.IsGlobalAnnouncement,
		},
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
