package game

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unnamed-game/internal/safe"
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
	StartedAt         time.Time
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
	slog.Debug("preparing game")
	game := &Game{
		State:       StateGamePlaying,
		StartedAt:   time.Now(),
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

// goSafe runs a game goroutine under recovery. A panic ends this game through
// the normal destroy path (sessions saved, DB row finalized) and leaves the
// server and other games running.
func (game *Game) goSafe(name string, fn func()) {
	safe.Go(name, fn, func(any) {
		game.Destroy(DestroyReasonPanic)
	})
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
	game.goSafe("game loop", game.StartGame)                                         // Start the game loop
	game.goSafe("idle check", game.HandleEmptyGame)                                  // Ticks every 1 min to see if there's activity.
	game.goSafe("achievement processor", game.AchievementEngine.StartEventProcessor) // Listens to achievement events
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
	slog.Debug("game loop started", "game_id", game.GameId)
	for {
		select {
		case <-game.DestroyChan:
			slog.Debug("game loop terminated", "game_id", game.GameId)
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
			slog.Debug("idle-check loop terminated", "game_id", game.GameId)
			return
		case <-ticker.C:
			if game.IdleTick() {
				game.Destroy(DestroyReasonIdleCheck)
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
	for _, p := range game.Players {
		p.AFKMinutes++
		slog.Debug("player afk", "game_id", game.GameId, "player_id", p.GetID(), "minutes", p.AFKMinutes)
		if p.AFKMinutes >= PlayerAllowedAFKMins {
			game.AchievementEngine.PublishEvent(AchievementEvent{
				PlayerIDs: game.GetActiveAndAlivePlayerID(),
				Key:       "GAME:default",
				Amount:    0,
			})
			game.HandlePlayerDisconnect(p.GetID(), DisconnectReasonAFKTimeout)
		}
	}
	if len(game.GetActivePlayers()) == 0 || game.State != StateGamePlaying {
		game.EmptyMinutes++
		slog.Debug("game empty", "game_id", game.GameId, "minutes", game.EmptyMinutes)
		game.CreateLog("[SERVER] Game ending in %d minute(s)", StopGameAfterIdleMinutes-game.EmptyMinutes)
		game.AchievementEngine.PublishEvent(AchievementEvent{
			PlayerIDs: game.GetActiveAndAlivePlayerID(),
			Key:       "GAME:default",
			Amount:    0,
		})
		if game.EmptyMinutes >= StopGameAfterIdleMinutes {
			slog.Info("idle threshold reached, shutting down game", "game_id", game.GameId)
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
	DestroyReasonServerShutdown
	DestroyReasonPanic
)

func (dr DestroyReason) String() string {
	switch dr {
	case DestroyReasonIdleCheck:
		return "idle_check"
	case DestroyReasonPlayerQuit:
		return "player_quit"
	case DestroyReasonServerShutdown:
		return "server_shutdown"
	case DestroyReasonPanic:
		return "panic"
	default:
		return "unknown"
	}
}

// Destroy ends the game. Callers outside the game loop use this; the loop
// itself (which already holds game.Mu) uses destroyLocked.
func (game *Game) Destroy(reason DestroyReason) {
	game.Mu.Lock()
	defer game.Mu.Unlock()
	game.destroyLocked(reason)
}

// destroyLocked ends the game. Caller must hold game.Mu. Players still
// connected are disconnected first so their sessions are saved like any
// other disconnect (the server handles those events before GameDestroy).
func (game *Game) destroyLocked(reason DestroyReason) {
	select {
	case <-game.DestroyChan:
		return // Already destroyed, exit safely
	default:
	}
	reasonText := reason.String()
	for _, p := range game.GetActivePlayers() {
		game.HandlePlayerDisconnect(p.GetID(), DisconnectReasonGameDestroyed)
	}
	close(game.DestroyChan)
	dcChan := make(chan error)
	game.ServerEventChan <- ServerEvent{
		Type:     EventTypeGameDestroy,
		Value:    reasonText,
		RespChan: dcChan,
	}
	select {
	case <-dcChan:
	case <-time.After(5 * time.Second):
		slog.Error("timeout waiting for mass disconnect response", "game_id", game.GameId, "reason", reasonText)
	}

	game.ServerEventChan <- ServerEvent{Type: EventTypeDeactivateGame}
	slog.Info("game destroyed", "game_id", game.GameId, "reason", reasonText,
		"duration_s", int(time.Since(game.StartedAt).Seconds()), "players", len(game.Players))
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
	}
	pos, available := game.Level.GetSpawnPoint()
	if !available {
		slog.Warn("no available spawn point", "game_id", game.GameId, "player_id", player.GetID())
		return
	}
	if !game.Level.PutEntityAtPosition(player, pos) {
		slog.Warn("spawn point blocked", "game_id", game.GameId, "player_id", player.GetID())
		return
	}
	slog.Debug("player spawned", "game_id", game.GameId, "player_id", player.GetID(), "x", pos.X, "y", pos.Y)
}

func (game *Game) HandlePlayerConnect(event GameEvent) error {
	// Authoritative capacity check: this runs inside the game loop, so unlike
	// the join pre-check it cannot race with other players connecting. A
	// player who is still counted as active (reconnect) keeps their slot.
	if player, active := game.GetActivePlayerById(event.PlayerID); active {
		// Reconnect while the old socket is still registered (page refresh):
		// the new connection takes over the player as-is. Re-placing them
		// here would leave a stale tile entry behind.
		slog.Info("player reconnected", "game_id", game.GameId, "player_id", event.PlayerID, "old_conn", player.ConnID, "new_conn", event.ConnID)
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
		if game.LevelsCompleted == player.LevelsCompleted {
			if !game.Level.PutEntityAtPosition(player, player.GetPosition()) {
				slog.Debug("old position blocked, falling back to spawn", "game_id", game.GameId, "player_id", event.PlayerID)
				game.SpawnPlayer(player)
			}
		} else {
			game.SpawnPlayer(player)
		}
		player.PlayerState = StatePlaying
	} else {
		keybinds, ok := event.Object.(map[string]string)
		if !ok {
			slog.Error("unexpected keybinds type on connect", "game_id", game.GameId, "player_id", event.PlayerID, "type", fmt.Sprintf("%T", event.Object))
		}
		player := NewPlayer(event.PlayerID, keybinds)
		player.ConnID = event.ConnID
		player.startPlayTime()
		player.GameID = game.GameId.String()
		game.SpawnPlayer(player)
	}
	slog.Info("player connected", "game_id", game.GameId, "player_id", event.PlayerID, "conn_id", event.ConnID)
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

// DisconnectReason says why a player was disconnected, for logs.
type DisconnectReason int

const (
	DisconnectReasonSocketClosed DisconnectReason = iota
	DisconnectReasonPlayerQuit
	DisconnectReasonAFKTimeout
	DisconnectReasonGameDestroyed
)

func (dr DisconnectReason) String() string {
	switch dr {
	case DisconnectReasonSocketClosed:
		return "socket_closed"
	case DisconnectReasonPlayerQuit:
		return "player_quit"
	case DisconnectReasonAFKTimeout:
		return "afk_timeout"
	case DisconnectReasonGameDestroyed:
		return "game_destroyed"
	default:
		return "unknown"
	}
}

// HandlePlayerDisconnect must be called with game.Mu held (game loop).
func (game *Game) HandlePlayerDisconnect(playerID string, reason DisconnectReason) {
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
		slog.Info("player disconnected", "game_id", game.GameId, "player_id", playerID, "conn_id", player.ConnID, "reason", reason.String())
	}
}

// HandlePlayerQuit disconnects the player and destroys the game when nobody
// is left. Caller must hold game.Mu (it runs inside the game loop).
func (game *Game) HandlePlayerQuit(playerID string) {
	game.HandlePlayerDisconnect(playerID, DisconnectReasonPlayerQuit)
	if len(game.GetActivePlayers()) == 0 {
		game.destroyLocked(DestroyReasonPlayerQuit)
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
			faultInjectChat(msg)
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
	slog.Info("game chat", "game_id", game.GameId, "player_id", playerid, "length", len(message))
	slog.Debug("game chat text", "game_id", game.GameId, "player_id", playerid, "text", message)
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
