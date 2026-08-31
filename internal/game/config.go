package game

//Log Constants
const (
	LogInfo    = "[*]"
	LogSuccess = "[+]"
	LogWarn    = "[!]"
	LogError   = "[-]"
	LogDebug   = "[?]"
	ChatCursor = "[Say]"
)

// Elements to Draw
const (
	//SymbolPlayer     = '❤'
	//SymbolPlayer     = '⊙'
	SymbolArcher         = '★'
	SymbolPlayer         = '⊗'
	SymbolGoblin         = '¶'
	SymbolTopWall        = '#'
	SymbolSideWall       = '#'
	SymbolArrowLeft      = '<'
	SymbolArrowRight     = '>'
	SymbolArrowUp        = '^'
	SymbolArrovDown      = 'v'
	SymbolArrowUpLeft    = '\\'
	SymbolArrowUpRight   = '/'
	SymbolArrowDownLeft  = '/'
	SymbolArrowDownRight = '\\'

	//SymbolArrowLeft        = '★'
	//SymbolArrowRight       = '★'
	//SymbolArrowUp          = '★'
	//SymbolArrovDown        = '★'
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
	BasicAttackBaseRange               = 1
	ArrowAttackBaseRange               = 30
	SpellAttackBaseRange               = 25
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
	MaxScreenWidth    = 200
	MaxScreenHeight   = 55
	MaxMessageLength  = 50
	MaxEventsLength   = 50
	MaxChatHistory    = 50
	SidebarWidth      = 2
	HorizontalPadding = 1
	VerticalPadding   = 1
)

// Game Constants
const (
	InputBufferPerPlayer     = 10
	StopGameAfterIdleMinutes = 5
	PlayerAllowedAFKMins     = 10
)
