package game

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"uuid"
)

type Game struct {
	GameSummary
	Mu                sync.RWMutex
	Events            []Event
	Frame             [][]rune
	ChatHistory       []string
	Players           []*Player
	AchievementEngine *AchievementEngine
	GlobalChat        chan string
	GameChat          chan string
	EventChan         chan GameEvent
	ServerEventChan   chan ServerEvent
	DestroyChan       chan struct{}
	GameId            uuid.UUID
	Level             *Level
	EmptyMinutes      int
	State             GameState
}
type GameState int

const (
	StateGamePlaying GameState = iota
	StateGameOver
	StateGameIntermission
)

// ---------------------------------------------------------------------------
// Constructors & Initialization
// ---------------------------------------------------------------------------

func InitializeGame() *Game {
	fmt.Print("Preparing Game...\r\n")
	game := &Game{
		State:       StateGamePlaying,
		DestroyChan: make(chan struct{}),
		GameChat:    make(chan string, MaxChatHistory),
		// Each game has its own global chat inbox; the server fans every
		// message out to all games (see DeliverGlobalChat).
		GlobalChat: make(chan string, MaxChatHistory),
	}
	game.InitFrame()
	InitEventChan(game)
	NewLevel(game)
	return game
}
func (game *Game) goSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ERROR] Panic recovered in background worker: %v", r)
				select {
				case <-game.DestroyChan:
				default:
					close(game.DestroyChan)
				}
			}
		}()
		fn()
	}()
}
// NewGame creates a game and starts its goroutines.
func NewGame(gameId uuid.UUID, achievementCatalog *AchievementCatalog) *Game {
	game := CreateGame(gameId, achievementCatalog)
	game.Start()
	return game
}

// CreateGame builds a game without starting any goroutines, so a caller can
// do fallible setup (e.g. the DB insert) first and simply drop the game if
// it fails. Call Start once the game is ready to run.
func CreateGame(gameId uuid.UUID, achievementCatalog *AchievementCatalog) *Game {
	game := InitializeGame()
	game.GameId = gameId
	game.AchievementEngine = NewAchievementEngine(achievementCatalog, game.DestroyChan, game.ServerEventChan)
	return game
}

// Start launches the game loop, the idle ticker and the achievement
// processor. They run until the game is destroyed. Call it once.
func (game *Game) Start() {
	game.goSafe(game.StartGame)                             // Start the game loop
	game.goSafe(game.HandleEmptyGame)                       // Ticks every 1 min to see if there's activity.
	game.goSafe(game.AchievementEngine.StartEventProcessor) // Listens to achievement events
}

func InitEventChan(game *Game) {
	evntChan := make(chan GameEvent, InputBufferPerPlayer*MaxPlayerCount)
	game.EventChan = evntChan

	serverEvntChan := make(chan ServerEvent, ServerEventBufferLength)
	game.ServerEventChan = serverEvntChan
}

func (game *Game) InitFrame() {
	game.Frame = make([][]rune, MaxScreenHeight)
	for row := 0; row < MaxScreenHeight; row++ {
		game.Frame[row] = make([]rune, MaxScreenWidth)
	}
}

// ---------------------------------------------------------------------------
// Core Game Loop & Lifecycle
// ---------------------------------------------------------------------------

func (game *Game) StartGame() {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	log.Println("[DEBUG] Start Game")
	for {
		select {
		case <-game.DestroyChan:
			log.Println("[DEBUG] StartGame loop terminated.")
			return
		case <-ticker.C:
			game.Tick()
		}
	}
}

// Tick runs one iteration of the game loop: process inputs and chat, update
// players and the level, then render and push a frame to every active player.
// The whole tick runs under game.Mu, so rendering never reads the level while
// another goroutine (Destroy, disconnects, IdleTick) is mutating it.
func (game *Game) Tick() {
	game.Mu.Lock()
	defer game.Mu.Unlock()
	game.ProcessInputs()
	game.PollChat()
	for _, player := range game.GetActivePlayers() {
		player.UpdatePlayer(game)
	}
	if game.State == StateGamePlaying {
		game.UpdateGame()
	}

	for _, player := range game.GetActivePlayers() {
		var renderedFrame string
		switch game.State {
		case StateGamePlaying:
			renderedFrame = game.DrawLevelForPlayer(game.Level, player)
		case StateGameIntermission:
			renderedFrame = game.DrawLevelIntermissionForPlayer(game.Level, player)
		case StateGameOver:
			renderedFrame = game.DrawGameOverForPlayer(game.Level, player)
		}
		select {
		case player.DisplayChan <- renderedFrame:
		default:
		}
	}
}

func (game *Game) UpdateGame() {
	for _, proj := range game.Level.Projectiles {
		proj.Update(game)
	}
	for _, enemy := range game.Level.Enemies {
		UpdateEnemy(game, enemy)
	}
	for _, effect := range game.Level.Effects {
		effect.Update(game)
	}
	game.Level.Update(game) // Check win/loss conditions
}

func (game *Game) HandleEmptyGame() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-game.DestroyChan:
			log.Println("[DEBUG] HandleEmptyGame loop terminated.")
			return
		case <-ticker.C:
			if game.IdleTick() {
				game.Destroy(DestroyReasonIdleCheck.String())
				return
			}
		}
	}
}

// IdleTick runs one minute of AFK/idle bookkeeping. It returns true when the
// game has been idle long enough that the caller should destroy it.
func (game *Game) IdleTick() bool {
	game.Mu.Lock()
	defer game.Mu.Unlock()
	log.Printf("[DEBUG] AFK TIMER TICK!")
	for _, p := range game.Players {
		p.AFKMinutes++
		log.Printf("[DEBUG] Player %s afk for %d minute(s)", p.GetID(), p.AFKMinutes)
		if p.AFKMinutes >= PlayerAllowedAFKMins {
			game.AchievementEngine.PublishEvent(AchievementEvent{
				PlayerIDs: game.GetActiveAndAlivePlayerID(),
				Key:       "GAME:default",
				Amount:    0,
			})
			game.HandlePlayerDisconnect(p.GetID())
		}
	}
	if len(game.GetActivePlayers()) == 0 || game.State != StateGamePlaying {
		game.EmptyMinutes++
		log.Printf("[DEBUG] Game empty for %d minute(s)", game.EmptyMinutes)
		game.CreateLog("[SERVER] Game ending in %d minute(s)", StopGameAfterIdleMinutes-game.EmptyMinutes)
		game.AchievementEngine.PublishEvent(AchievementEvent{
			PlayerIDs: game.GetActiveAndAlivePlayerID(),
			Key:       "GAME:default",
			Amount:    0,
		})
		if game.EmptyMinutes >= StopGameAfterIdleMinutes {
			log.Println("[DEBUG] Idle threshold reached via ticker. Shutting down game.")
			return true
		}
	} else {
		game.EmptyMinutes = 0
	}
	return false
}

type DestroyReason int

const (
	DestroyReasonIdleCheck DestroyReason = iota
	DestroyReasonPlayerQuit
)

func (dr DestroyReason) String() string {
	switch dr {
	case DestroyReasonIdleCheck:
		return "idle_check"
	case DestroyReasonPlayerQuit:
		return "player_quit"
	default:
		return "unknown"
	}
}

// Destroy ends the game. Callers outside the game loop use this; the loop
// itself (which already holds game.Mu) uses destroyLocked.
func (game *Game) Destroy(reason string) {
	game.Mu.Lock()
	defer game.Mu.Unlock()
	game.destroyLocked(reason)
}

// destroyLocked ends the game. Caller must hold game.Mu. Players still
// connected are disconnected first so their sessions are saved like any
// other disconnect (the server handles those events before GameDestroy).
func (game *Game) destroyLocked(reason string) {
	select {
	case <-game.DestroyChan:
		return // Already destroyed, exit safely
	default:
	}
	for _, p := range game.GetActivePlayers() {
		game.HandlePlayerDisconnect(p.GetID())
	}
	close(game.DestroyChan)
	dcChan := make(chan error)
	game.ServerEventChan <- ServerEvent{
		Type:     EventTypeGameDestroy,
		Value:    reason,
		RespChan: dcChan,
	}
	select {
	case <-dcChan:
	case <-time.After(5 * time.Second):
		log.Println("[ERROR] Timeout waiting for mass disconnect response")
	}

	game.ServerEventChan <- ServerEvent{Type: EventTypeDeactivateGame}
	log.Println("[DEBUG] Game instance successfully destroyed and idle check sent.")
}

// ---------------------------------------------------------------------------
// Player & Session Management
// ---------------------------------------------------------------------------

func (game *Game) SpawnPlayer(player *Player) {
	alreadyExists := false
	for _, p := range game.Players {
		if p.GetID() == player.GetID() {
			alreadyExists = true
			break
		}
	}
	if !alreadyExists {
		game.Players = append(game.Players, player)
		log.Printf("Successfully spawned player %s", player.GetID())
	}
	pos, available := game.Level.GetSpawnPoint()
	if !available {
		log.Println("No available spawn points found!")
		return
	}
	if !game.Level.PutEntityAtPosition(player, pos) {
		log.Println("Failed to place player: Spawn point was blocked.")
		return
	}
	log.Printf("Successfully spawned player at %d, %d", pos.X, pos.Y)
}

func (game *Game) HandlePlayerConnect(event GameEvent) error {
	log.Println("[DEBUG] Player Connecting")
	// Authoritative capacity check: this runs inside the game loop, so unlike
	// the join pre-check it cannot race with other players connecting. A
	// player who is still counted as active (reconnect) keeps their slot.
	if player, active := game.GetActivePlayerById(event.PlayerID); active {
		// Reconnect while the old socket is still registered (page refresh):
		// the new connection takes over the player as-is. Re-placing them
		// here would leave a stale tile entry behind.
		log.Printf("[DEBUG] %s reconnected (conn %d -> %d)", event.PlayerID, player.ConnID, event.ConnID)
		player.ConnID = event.ConnID
		game.EmptyMinutes = 0
		return nil
	}
	if err := game.ValidateSpace(); err != nil {
		return err
	}
	if player, exists := game.GetPlayerByID(event.PlayerID); exists {
		player.ConnID = event.ConnID
		player.startPlayTime()
		log.Println("[DEBUG] Existing Player Connecting")
		if game.LevelsCompleted == player.LevelsCompleted {
			if !game.Level.PutEntityAtPosition(player, player.GetPosition()) {
				log.Printf("[DEBUG] Old position blocked for %s, falling back to spawn", event.PlayerID)
				game.SpawnPlayer(player)
			}
		} else {
			game.SpawnPlayer(player)
		}
		player.PlayerState = StatePlaying
	} else {
		log.Println("[DEBUG] New Player Connecting")
		keybinds, ok := event.Object.(map[string]string)
		if !ok {
			log.Printf("[ERROR] Expected map[string]string in event.Object, got %T", event.Object)
		}
		player := NewPlayer(event.PlayerID, keybinds)
		player.ConnID = event.ConnID
		player.startPlayTime()
		player.GameID = game.GameId.String()
		log.Printf("[DEBUG] New Player Game id: %s", player.GameID)
		game.SpawnPlayer(player)
	}
	game.EmptyMinutes = 0
	return nil
}

// PlayerDisconnectPayload travels on the EventTypeDisconnect server event.
// It is built inside the game loop so the server never reads live player
// state, and it carries only what has not been saved yet.
type PlayerDisconnectPayload struct {
	Unsaved             PlayerSessionSummary // deltas since the last save
	AchievementProgress []AchProgressDTO     // full current progress (upserted)
	Achievements        []AchievementView    // for the client's session summary
}

// HandlePlayerDisconnect must be called with game.Mu held (game loop).
func (game *Game) HandlePlayerDisconnect(playerID string) {
	log.Println("[DEBUG] Player Disconnecting")
	if player, exists := game.GetActivePlayerById(playerID); exists {
		player.PlayerState = StateDisconnected
		player.stopPlayTime()
		payload := PlayerDisconnectPayload{
			Unsaved: player.TakeUnsavedSummary(),
			// Export before RemovePlayerSession, which discards the progress.
			AchievementProgress: game.AchievementEngine.ExportPlayerProgress(playerID),
			Achievements:        game.AchievementEngine.BuildClientAchievementPayload(playerID),
		}
		game.AchievementEngine.RemovePlayerSession(playerID)
		game.Level.RemoveEntity(player)
		game.ServerEventChan <- ServerEvent{PlayerID: playerID, Type: EventTypeDisconnect, ConnID: player.ConnID, Object: payload}
		log.Printf("[DEBUG] Player %s Disconnected", playerID)
	}
}

// HandlePlayerQuit disconnects the player and destroys the game when nobody
// is left. Caller must hold game.Mu (it runs inside the game loop).
func (game *Game) HandlePlayerQuit(playerID string) {
	log.Printf("[DEBUG] Player %s quit", playerID)
	game.HandlePlayerDisconnect(playerID)
	if len(game.GetActivePlayers()) == 0 {
		log.Printf("[DEBUG] Game destroy")
		game.destroyLocked(DestroyReasonPlayerQuit.String())
	}
}

// HandleNextLevel generates the next level and resumes play. Caller must
// hold game.Mu (it runs inside the game loop).
func (game *Game) HandleNextLevel() {
	NewLevel(game)
	game.EmptyMinutes = 0
	game.State = StateGamePlaying
}

func (game *Game) GetPlayerByID(playerID string) (*Player, bool) {
	for _, p := range game.Players {
		if p.GetID() == playerID {
			return p, true
		}
	}
	return nil, false
}

func (game *Game) GetActivePlayers() []*Player {
	var active []*Player
	for _, p := range game.Players {
		if p.PlayerState != StateDisconnected {
			active = append(active, p)
		}
	}
	return active
}

func (game *Game) GetActiveAndAlivePlayerID() []string {
	var active []string
	for _, p := range game.Players {
		if p.PlayerState != StateDisconnected && p.IsAlive() {
			active = append(active, p.GetID())
		}
	}
	return active
}

func (game *Game) GetActivePlayerById(playerId string) (*Player, bool) {
	for _, p := range game.Players {
		if p.PlayerState != StateDisconnected && p.GetID() == playerId {
			return p, true
		}
	}
	return nil, false
}

// IsDestroyed reports whether Destroy has been called.
func (game *Game) IsDestroyed() bool {
	select {
	case <-game.DestroyChan:
		return true
	default:
		return false
	}
}

// SendEvent queues an event for the game loop. It waits while the queue is
// full (the loop drains it every tick) but gives up and returns false once
// the game is destroyed, since nothing reads EventChan after that. Code
// outside the game loop must use this instead of sending to EventChan.
func (game *Game) SendEvent(ev GameEvent) bool {
	select {
	case <-game.DestroyChan:
		return false
	default:
	}
	select {
	case game.EventChan <- ev:
		return true
	case <-game.DestroyChan:
		return false
	}
}

// Validate is a pre-check for joining, called from HTTP handlers (outside the
// game loop). Capacity is enforced again on connect by HandlePlayerConnect.
func (game *Game) Validate(playerid string) error {
	game.Mu.RLock()
	defer game.Mu.RUnlock()
	if game.IsDestroyed() {
		return fmt.Errorf("Game has ended")
	}
	if game.State == StateGameOver {
		return fmt.Errorf("Game is over!")
	}
	if err := game.ValidateSpace(); err != nil {
		return err
	}
	if err := game.ValidatePlayer(playerid); err != nil {
		return err
	}
	return nil
}

func (game *Game) ValidateSpace() error {
	if len(game.GetActivePlayers()) >= MaxPlayerCount {
		return fmt.Errorf("Game is full")
	}
	return nil
}
func (game *Game) ValidatePlayer(playerid string) error {
	if player, exist := game.GetPlayerByID(playerid); exist && !player.IsAlive() {
		return fmt.Errorf("Failed to join game. Player: %s is dead", playerid)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rendering & UI Engine
// ---------------------------------------------------------------------------

func (game *Game) DrawLevelForPlayer(level *Level, player *Player) string {
	if !player.IsAlive() {
		return game.DrawGameOverForPlayer(level, player)
	}
	game.ClearFrame()
	game.DrawLogsPanel()
	game.DrawEntities(game.Level)
	game.DrawEffects(game.Level)
	game.DrawGlobalChatPanel(level, player)
	game.DrawPlayerHUD(player)
	game.DrawFooter()
	return game.FlushFrame()
}

func (game *Game) DrawSummaryScreenForPlayer(level *Level, player *Player, summaryLines []string) string {
	game.ClearFrame()
	game.DrawLogsPanel()
	game.DrawGlobalChatPanel(level, player)

	startRow := 4
	startCol := MaxMessageLength + 4

	for rIdx, line := range summaryLines {
		targetRow := startRow + rIdx
		if targetRow >= MaxScreenHeight {
			break
		}
		for cIdx, ch := range line {
			targetCol := startCol + cIdx
			if targetCol < MaxScreenWidth {
				game.Frame[targetRow][targetCol] = ch
			}
		}
	}

	game.DrawPlayerHUD(player)
	game.DrawFooter()
	return game.FlushFrame()
}

func (game *Game) DrawLevelIntermissionForPlayer(level *Level, player *Player) string {
	if !player.IsAlive() {
		return game.DrawGameOverForPlayer(level, player)
	}
	var summary PlayerSessionSummary
	if player != nil {
		summary = player.GenerateSummary()
	}
	return game.DrawSummaryScreenForPlayer(level, player, GetSummaryLines(summary, player.GetQuitKey()))
}

func (game *Game) DrawGameOverForPlayer(level *Level, player *Player) string {
	var summary PlayerSessionSummary
	if player != nil {
		summary = player.GenerateSummary()
	}
	return game.DrawSummaryScreenForPlayer(level, player, GetGameOverSummaryLines(summary, player.GetQuitKey()))
}

func (game *Game) ClearFrame() {
	for row := 0; row < MaxScreenHeight; row++ {
		for col := 0; col < MaxScreenWidth; col++ {
			game.Frame[row][col] = SymbolDefault
		}
	}
}

func (game *Game) DrawEntities(level *Level) {
	for _, entity := range level.Entities {
		if drawable, ok := entity.(Drawable); ok {
			game.DrawObject(level, drawable)
		}
	}
}

func (game *Game) DrawEffects(level *Level) {
	for _, effect := range level.Effects {
		game.DrawObject(level, effect)
	}
}

func (game *Game) DrawObject(level *Level, obj Drawable) {

	X, Y := LevelSizeX, LevelSizeY
	pos := obj.GetPosition()

	if pos.X < 0 || pos.X >= X || pos.Y < 0 || pos.Y >= Y {
		return
	}

	targetRow := VerticalPadding + pos.Y
	targetCol := MaxMessageLength + 3 + pos.X

	if targetRow < MaxScreenHeight && targetCol < MaxScreenWidth {
		game.Frame[targetRow][targetCol] = obj.GetSymbol()
	}
}

func (game *Game) DrawGlobalChatPanel(level *Level, player *Player) {
	gameStartCol := MaxMessageLength + 3
	chatStartCol := gameStartCol + LevelSizeX + 3

	for i, msg := range game.ChatHistory {
		row := VerticalPadding + i
		if row >= MaxScreenHeight {
			break
		}
		for colIdx, ch := range msg {
			targetCol := chatStartCol + colIdx
			if targetCol < MaxScreenWidth {
				game.Frame[row][targetCol] = ch
			}
		}
	}

	if player != nil && player.PlayerState == StateTyping {
		typingRow := VerticalPadding + len(game.ChatHistory) + 1
		if typingRow < MaxScreenHeight {
			prompt := fmt.Sprintf("%s: %s_", player.GetCursor(), player.MessageBuffer)
			for colIdx, ch := range prompt {
				targetCol := chatStartCol + colIdx
				if targetCol < MaxScreenWidth {
					game.Frame[typingRow][targetCol] = ch
				}
			}
		}
	}
}

func (game *Game) DrawPlayerHUD(player *Player) {
	if player == nil {
		return
	}
	hudText := fmt.Sprintf(" %c%d | %c%v | %c%d/%d | %c%d | %c%d |%c%d",
		SymbolHitPoints, player.CurrentHealth, SymbolCurrentAttack, player.GetEquippedAttack().String(), SymbolCurrentExperience, player.ExperienceVal, player.NextLevelXP, SymbolCurrentLevel, player.Level, SymbolCurrentGameLevel, game.TotalLevels, SymbolScore, player.Score)
	gameStartCol := MaxMessageLength + 3
	for col, ch := range hudText {
		targetCol := gameStartCol + col
		if targetCol < MaxScreenWidth {
			game.Frame[0][targetCol] = ch
		}
	}
}

func (game *Game) DrawLogsPanel() {
	for row := 0; row < MaxScreenHeight; row++ {
		if row >= MaxEventsLength {
			break
		}
		for col := 0; col < MaxMessageLength; col++ {
			game.Frame[row][col] = game.GetEventRuneAt(row, col)
		}
	}
}

func (game *Game) DrawFooter() {
	row1Idx := MaxScreenHeight - 2
	row2Idx := MaxScreenHeight - 1

	if row1Idx >= 0 && row2Idx < len(game.Frame) {
		for col := range game.Frame[row1Idx] {
			game.Frame[row1Idx][col] = ' '
			game.Frame[row2Idx][col] = ' '
		}

		text1 := "Press [C] to copy Game ID"
		text2 := fmt.Sprintf("Game ID: %s", game.GameId.String())

		startCol1 := (MaxScreenWidth - len(text1)) / 2
		for i, ch := range text1 {
			target := startCol1 + i
			if target >= 0 && target < MaxScreenWidth {
				game.Frame[row1Idx][target] = ch
			}
		}

		startCol2 := (MaxScreenWidth - len(text2)) / 2
		for i, ch := range text2 {
			target := startCol2 + i
			if target >= 0 && target < MaxScreenWidth {
				game.Frame[row2Idx][target] = ch
			}
		}
	}
}

func (game *Game) FlushFrame() string {
	var sb strings.Builder
	sb.WriteString("\033[H")
	for _, row := range game.Frame {
		sb.WriteString(string(row))
		sb.WriteString("\r\n")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Chat & Utilities
// ---------------------------------------------------------------------------
func (game *Game) PollChat() {
	for {
		select {
		case msg := <-game.GlobalChat:
			game.appendChat(fmt.Sprintf("%s%s", ChatChannelDisplayGlobalChat, msg))
		case msg := <-game.GameChat:
			game.appendChat(fmt.Sprintf("%s%s", ChatChannelDisplayGameChat, msg))
		default:
			return
		}
	}
}

// DeliverGlobalChat queues a global chat message for this game without
// blocking; it returns false if the game's inbox is full (message dropped).
// Safe to call from any goroutine.
func (game *Game) DeliverGlobalChat(msg string) bool {
	select {
	case game.GlobalChat <- msg:
		return true
	default:
		return false
	}
}

func (game *Game) appendChat(msg string) {
	game.ChatHistory = append(game.ChatHistory, msg)
	if len(game.ChatHistory) > MaxChatHistory {
		game.ChatHistory = game.ChatHistory[1:]
	}
}
func (game *Game) BroadcastGameChat(playerid string, message string) {
	msg := fmt.Sprintf("[%s]: %s", playerid, message)
	log.Println(msg)
	select {
	case game.GameChat <- msg:
	default:
	}
}

func (game *Game) DistributeXp(xp int) {
	for _, player := range game.GetActivePlayers() {
		player.GainXp(xp)
	}
}

func (game *Game) GetAveragePlayerLevel() int {
	if len(game.GetActivePlayers()) == 0 {
		return 1
	}
	totalLevel := 0
	for _, player := range game.GetActivePlayers() {
		totalLevel += player.Level
	}
	return totalLevel / len(game.GetActivePlayers())
}

func InitGameRegistries() {
	InitAttacks()
	InitSpawnRules()
	InitEnemyBlueprints()
	InitXpTable()
}
