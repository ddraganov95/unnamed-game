package game

//Log Constants
const (
	LogInfo                      = "[*]"
	LogSuccess                   = "[+]"
	LogWarn                      = "[!]"
	LogError                     = "[-]"
	LogDebug                     = "[?]"
	ChatCursorDefault            = "[Say]"
	ChatCursorGameChat           = "[Game]"
	ChatCursorGlobalChat         = "[Global]"
	ChatChannelDisplayGameChat   = "[1]"
	ChatChannelDisplayGlobalChat = "[2]"
)

// Elements to Draw
const (
	//SymbolPlayer     = '❤'
	//SymbolPlayer     = '⊙'
	SymbolArcher         = '★'
	SymbolPlayer         = '⊗'
	SymbolGoblin         = '¶'
	SymbolWallDefault    = '#'
	SymbolArrowLeft      = '<'
	SymbolArrowRight     = '>'
	SymbolArrowUp        = '^'
	SymbolArrovDown      = 'v'
	SymbolArrowUpLeft    = '\\'
	SymbolArrowUpRight   = '/'
	SymbolArrowDownLeft  = '/'
	SymbolArrowDownRight = '\\'

	SymbolOrb               = 'O'
	SymbolDefault           = ' '
	SymbolDefaultLevelTile  = ' '
	SymbolSpellLeft         = '❄'
	SymbolSpellRight        = '❄'
	SymbolSpellUp           = '❄'
	SymbolSpellDown         = '❄'
	SymbolSpellUpLeft       = '❄'
	SymbolSpellUpRight      = '❄'
	SymbolSpellDownLeft     = '❄'
	SymbolSpellDownRight    = '❄'
	SymbolHitEffect         = 'x'
	SymbolHitPoints         = '❤'
	SymbolCurrentAttack     = '⚔'
	SymbolCurrentExperience = '✦'
	SymbolCurrentLevel      = 'ᛟ'
	SymbolScore             = '★'
	SymbolCurrentGameLevel  = '⚑'
)

// Combat Constants
const (
	EnemyDefaultAggroRange             = 35
	EnemyDefaultMovementSpeed          = 25
	EnemyDefaultAttackSpeed            = 15
	EnemyDefaultExperience             = 10
	ArcherDefaultExperience            = EnemyDefaultExperience * 3
	GoblinDefaultExperience            = EnemyDefaultExperience
	PlayerLevelOneExperience           = 10
	PlayerLevelXpRequirementMultiplier = 1.1
	EnemyDefaultAttackSpeedRanged      = 100
	PlayerDamageReductionPercent       = 50
	EnemyDefaultDamageMultipier        = 100
	PlayerDefaultDamageMultipier       = 100
	ProjectileDefaultTravelSpeed       = 1
	BasicAttackBaseDamage              = 30
	ArrowAttackBaseDamage              = 10
	SpellAttackBaseDamage              = 20
	OrbAttackBaseDamage                = 10
	OrbAttackProjectileDamage          = 15
	BasicAttackBaseRange               = 1
	ArrowAttackBaseRange               = 30
	SpellAttackBaseRange               = 25
	OrbAttackBaseRange                 = 15
)

// Level Constants
const (
	LevelSizeX                = 100
	LevelSizeY                = 50
	MaxRegularZoneSizeX       = 10
	MaxRegularZoneSizeY       = 10
	MinRegularZoneSizeX       = 3
	MinRegularZoneSizeY       = 3
	MaxRegularZonesPerLevel   = 10
	PlayerSpawnPointsPerLevel = 3
	MaxPlayerCount            = 2
)

// Display Constants
const (
	MaxScreenWidth       = 250
	MaxScreenHeight      = 55
	MaxMessageLength     = 70 //EVENTS MESSAGE LENGTH
	MaxEventsLength      = 50 //EVENTS HISTORY LENGTH
	MaxChatHistory       = 50
	MaxChatMessageLength = 70
	SidebarWidth         = 2
	HorizontalPadding    = 1
	VerticalPadding      = 1
)

// Game Constants
const (
	InputBufferPerPlayer     = 10
	StopGameAfterIdleMinutes = 3
	PlayerAllowedAFKMins     = 2
	MaxPlayerLevel           = 100
)
