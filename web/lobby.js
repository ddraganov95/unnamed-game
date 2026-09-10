window.addEventListener("DOMContentLoaded", async () => {
    await verifySessionAndRoute();
});

// Ensures session is re-checked when navigating back/forward from browser cache
window.addEventListener("pageshow", async (event) => {
    if (event.persisted) { 
        await verifySessionAndRoute();
    }
});

async function verifySessionAndRoute() {
    try {
        // Ping Go server auth verification endpoint
        const response = await fetch("/api/auth/me", { method: "GET" });

        if (response.status === 401 || response.status === 403) {
            // Cookie missing or expired -> Force instant redirect to login
            window.location.href = "/";
        } else if (response.status >= 500) {
            window.location.href = "/offline.html";
        }
    } catch (error) {
        // Fetch failed completely -> Server is down/unreachable
        console.error("Server unreachable:", error);
        window.location.href = "/offline.html";
    }
}

document.addEventListener('DOMContentLoaded', () => {
    const errorLabel = document.getElementById('error-label');
    const chatBox = document.getElementById('chat-box');
    const chatInput = document.getElementById('chat-input');
    const btnStart = document.getElementById('btn-start');
    const btnJoin = document.getElementById('btn-join');
    const btnSendChat = document.getElementById('btn-send-chat');

    // State Tracking
    let profileLoaded = false;
    let achievementsLoaded = false;
    let leaderboardLoaded = false;
    let configurationLoaded = false;
    let currentLeaderboardPage = 1;

    // Display URL error params if present
    const urlParams = new URLSearchParams(window.location.search);
    const errorMsg = urlParams.get('error');
    if (errorMsg && errorLabel) {
        errorLabel.textContent = errorMsg;
    }

    // ==========================================
    // TAB NAVIGATION
    // ==========================================
    const tabBtns = document.querySelectorAll('.tab-btn');
    const tabContents = document.querySelectorAll('.tab-content');

    tabBtns.forEach(btn => {
        btn.addEventListener('click', async () => {
            const targetTab = btn.getAttribute('data-tab');

            tabBtns.forEach(b => b.classList.remove('active'));
            tabContents.forEach(c => c.classList.remove('active'));

            btn.classList.add('active');
            const activeContent = document.getElementById(targetTab);
            if (activeContent) activeContent.classList.add('active');

            // PROFILE TAB
            if (targetTab === 'tab-profile' && !profileLoaded) {
                profileLoaded = true;
                await loadProfileTab();
            }

            // ACHIEVEMENTS TAB
            if (targetTab === 'tab-achievements' && !achievementsLoaded) {
                achievementsLoaded = true;
                await loadAchievementsTab();
            }

            // LEADERBOARD TAB
            if (targetTab === 'tab-leaderboard' && !leaderboardLoaded) {
                leaderboardLoaded = true;
                await loadLeaderboardUser();
            }

            // CONFIGURATION TAB
            if (targetTab === 'tab-configuration' && !configurationLoaded) {
                const tabConfigContainer = document.getElementById('tab-configuration');
                try {
                    const response = await fetch('configuration.html');
                    if (!response.ok) throw new Error('Failed to load configuration.html');
                    
                    const html = await response.text();
                    if (tabConfigContainer) tabConfigContainer.innerHTML = html;
                    
                    configurationLoaded = true; 
                    
                    initConfigTabEvents();
                } catch (err) {
                    console.error('Error loading configuration tab:', err);
                    if (tabConfigContainer) {
                        tabConfigContainer.innerHTML = '<p class="error">Failed to load configuration layout.</p>';
                    }
                }
            }
        });
    });

    // ==========================================
    // PROFILE TAB LOADER
    // ==========================================
    async function loadProfileTab() {
        const tabProfileContainer = document.getElementById('tab-profile');
        try {
            const htmlRes = await fetch('/profile.html');
            if (!htmlRes.ok) throw new Error(`HTML fetch failed: ${htmlRes.statusText}`);
            if (tabProfileContainer) tabProfileContainer.innerHTML = await htmlRes.text();

            let userData = getCachedJSON('user_stats');

            if (!userData) {
                const apiRes = await fetch('/api/users/me');
                if (!apiRes.ok) {
                    if (apiRes.status === 401 || apiRes.status === 403) {
                        localStorage.removeItem('user_stats');
                        window.location.href = '/';
                        return;
                    }
                    throw new Error(`API fetch failed: ${apiRes.status}`);
                }
                userData = await apiRes.json();
                localStorage.setItem('user_stats', JSON.stringify(userData));
            }

            const elPlayerId = document.getElementById('stat-player-id');
            const elHighestLevel = document.getElementById('stat-highest-level');
            const elLevels = document.getElementById('stat-levels');
            const elEnemies = document.getElementById('stat-enemies');
            const elXp = document.getElementById('stat-xp');
            const elDmgDealt = document.getElementById('stat-damage-dealt');
            const elDmgTaken = document.getElementById('stat-damage-taken');
            const elDeaths = document.getElementById('stat-deaths');
            const elGameTime = document.getElementById('stat-game-time');

            if (elPlayerId) elPlayerId.textContent = userData.player_id || '--';
            if (elHighestLevel) elHighestLevel.textContent = userData.highest_player_level ?? 1;
            if (elLevels) elLevels.textContent = userData.total_levels_completed ?? 0;
            if (elEnemies) elEnemies.textContent = userData.total_enemies_killed ?? 0;
            if (elXp) elXp.textContent = userData.total_xp_gained ?? 0;
            if (elDmgDealt) elDmgDealt.textContent = userData.total_damage_dealt ?? 0;
            if (elDmgTaken) elDmgTaken.textContent = userData.total_damage_taken ?? 0;
            if (elDeaths) elDeaths.textContent = userData.total_deaths ?? 0;
            if (elGameTime) elGameTime.textContent = formatGameTime(userData.total_game_time);

        } catch (err) {
            console.error("[Profile Load Error]:", err);
            profileLoaded = false;
            if (tabProfileContainer) {
                tabProfileContainer.innerHTML = `<p class="profile-error">Unable to load profile stats.</p>`;
            }
        }
    }

    // ==========================================
    // ACHIEVEMENTS TAB LOADER
    // ==========================================
    async function loadAchievementsTab() {
        const tabAchievementsContainer = document.getElementById('tab-achievements');
        try {
            const htmlRes = await fetch('/achievements.html');
            if (!htmlRes.ok) throw new Error(`HTML fetch failed: ${htmlRes.statusText}`);
            if (tabAchievementsContainer) tabAchievementsContainer.innerHTML = await htmlRes.text();

            let achievementsData = getCachedJSON('user_achievements');

            if (!achievementsData) {
                const response = await fetch('/api/users/me/achievements');
                if (!response.ok) {
                    if (response.status === 401 || response.status === 403) {
                        localStorage.removeItem('user_achievements');
                        window.location.href = '/';
                        return;
                    }
                    throw new Error(`API fetch failed: ${response.status}`);
                }
                achievementsData = await response.json();
                localStorage.setItem('user_achievements', JSON.stringify(achievementsData));
            }

            renderAchievements(achievementsData);
        } catch (err) {
            console.error("[Achievements Load Error]:", err);
            achievementsLoaded = false;
            if (tabAchievementsContainer) {
                tabAchievementsContainer.innerHTML = `<p class="profile-error">Failed to load achievements.</p>`;
            }
        }
    }

    // ==========================================
    // LEADERBOARD FETCH & PAGINATION
    // ==========================================
    async function loadLeaderboardUser() {
        try {
            const response = await fetch(`/api/users/me/leaderboard`);
            if (!response.ok) {
                if (response.status === 401 || response.status === 403) {
                    window.location.href = '/';
                    return;
                }
                throw new Error(`API fetch failed: ${response.status}`);
            }

            const data = await response.json();
            renderLeaderboardPayload(data);
            currentLeaderboardPage = data.CurrentPage || 1;
        } catch (err) {
            console.error("[Leaderboard Load Error]:", err);
            const tbody = document.getElementById('leaderboard-tbody');
            if (tbody) tbody.innerHTML = `<tr><td colspan="3" style="text-align:center;">Error loading leaderboard.</td></tr>`;
        }
    }

    async function loadLeaderboardPage(page) {
        try {
            const response = await fetch(`/api/leaderboard?page=${page}`);
            if (!response.ok) {
                if (response.status === 401 || response.status === 403) {
                    window.location.href = '/';
                    return;
                }
                throw new Error(`API fetch failed: ${response.status}`);
            }

            const data = await response.json();
            renderLeaderboardPayload(data);
            currentLeaderboardPage = data.CurrentPage || page;
        } catch (err) {
            console.error("[Leaderboard Load Error]:", err);
            const tbody = document.getElementById('leaderboard-tbody');
            if (tbody) tbody.innerHTML = `<tr><td colspan="3" style="text-align:center;">Error loading leaderboard.</td></tr>`;
        }
    }

    const btnPrev = document.getElementById('btn-leaderboard-prev');
    const btnNext = document.getElementById('btn-leaderboard-next');
    const myTbody = document.getElementById('leaderboard-my-tbody');

    if (btnPrev) {
        btnPrev.addEventListener('click', async () => {
            if (currentLeaderboardPage > 1) {
                await loadLeaderboardPage(currentLeaderboardPage - 1);
            }
        });
    }

    if (btnNext) {
        btnNext.addEventListener('click', async () => {
            await loadLeaderboardPage(currentLeaderboardPage + 1);
        });
    }

    if (myTbody) {
        myTbody.addEventListener('click', async () => {
            await loadLeaderboardUser();
        });
    }

    // ==========================================
    // GAME CREATION & JOIN
    // ==========================================
    if (btnStart) {
        btnStart.addEventListener('click', async () => {
            if (errorLabel) errorLabel.textContent = "";
            try {
                const res = await fetch('/api/games', { method: 'POST' });
                if (!res.ok) {
                    const errorMessage = await res.text();
                    if (errorLabel) errorLabel.textContent = errorMessage.trim() || "Failed to start game.";
                    return;
                }
                const data = await res.json();
                window.location.href = `/game.html?gameId=${encodeURIComponent(data.game_id)}`;
            } catch (err) {
                if (errorLabel) errorLabel.textContent = "Server error while creating game.";
            }
        });
    }

    if (btnJoin) {
        btnJoin.addEventListener('click', async () => {
            if (errorLabel) errorLabel.textContent = "";
            const gameIdInput = document.getElementById('game-id-input');
            const gameId = gameIdInput ? gameIdInput.value.trim() : "";

            if (!gameId) {
                if (errorLabel) errorLabel.textContent = "Please paste a Game ID.";
                return;
            }

            try {
                const response = await fetch(`/api/games/${encodeURIComponent(gameId)}/join`, { method: 'POST' });
                if (!response.ok) {
                    const errorMessage = await response.text();
                    if (errorLabel) errorLabel.textContent = errorMessage.trim() || "Failed to join game.";
                    return;
                }
                const data = await response.json();
                window.location.href = `/game.html?gameId=${encodeURIComponent(data.game_id)}`;
            } catch (err) {
                if (errorLabel) errorLabel.textContent = "Failed to join game.";
            }
        });
    }

    // ==========================================
    // GLOBAL CHAT WEBSOCKET
    // ==========================================
    if (chatBox && chatInput) {
        const protocol = location.protocol === "https:" ? "wss:" : "ws:";
        const chatSocket = new WebSocket(`${protocol}//${window.location.host}/ws/global-chat`);

        chatSocket.onmessage = (event) => {
            const msgDiv = document.createElement('div');
            msgDiv.className = 'chat-message';
            msgDiv.textContent = event.data;
            chatBox.appendChild(msgDiv);
            chatBox.scrollTop = chatBox.scrollHeight;
        };

        function sendChatMessage() {
            const text = chatInput.value.trim();
            if (text && chatSocket.readyState === WebSocket.OPEN) {
                chatSocket.send(text);
                chatInput.value = '';
            }
        }

        if (btnSendChat) btnSendChat.addEventListener('click', sendChatMessage);
        chatInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') sendChatMessage();
        });
    }
});

// ==========================================
// HELPER FUNCTIONS & RENDERERS
// ==========================================
let cachedCurrentUser = null;

function renderLeaderboardPayload(data) {
    const myTbody = document.getElementById('leaderboard-my-tbody');
    const tbody = document.getElementById('leaderboard-tbody');
    const indicator = document.getElementById('leaderboard-page-indicator');
    const btnPrev = document.getElementById('btn-leaderboard-prev');
    const btnNext = document.getElementById('btn-leaderboard-next');

    const currentPage = data.CurrentPage || 1;
    currentLeaderboardPage = currentPage;

    if (indicator) indicator.textContent = `PAGE ${currentPage}`;
    if (btnPrev) btnPrev.disabled = !data.PreviousPageAvailable;
    if (btnNext) btnNext.disabled = !data.NextPageAvailable;

    if (data.CurrentUser) {
        cachedCurrentUser = data.CurrentUser;
        updateUserStatsCache(data.CurrentUser);
    }

    if (myTbody && cachedCurrentUser) {
        myTbody.innerHTML = `
            <tr style="cursor: pointer;" title="Click to jump to your page">
                <td>#${cachedCurrentUser.Rank}</td>
                <td>${escapeHtml(cachedCurrentUser.PlayerID || '--')}</td>
                <td>${cachedCurrentUser.HighestScore ?? 0}</td>
            </tr>
        `;
    }

    if (tbody) {
        tbody.innerHTML = '';
        const users = data.UsersOnPage || [];

        if (users.length === 0) {
            tbody.innerHTML = `<tr><td colspan="3" style="text-align: center;">No rankings found.</td></tr>`;
            return;
        }

        const currentUserId = cachedCurrentUser?.UserID;
        const fragment = document.createDocumentFragment();

        users.forEach(user => {
            const tr = document.createElement('tr');
            if (currentUserId && user.UserID === currentUserId) {
                tr.style.background = "rgba(59, 130, 246, 0.12)";
            }
            tr.innerHTML = `    
                <td>#${user.Rank}</td>
                <td>${escapeHtml(user.PlayerID || '--')}</td>
                <td>${user.HighestScore ?? 0}</td>
            `;
            fragment.appendChild(tr);
        });

        tbody.appendChild(fragment);
    }
}

function updateUserStatsCache(currentUserData) {
    const cached = localStorage.getItem('user_stats');
    let stats = {};
    if (cached) {
        try { stats = JSON.parse(cached); } catch (e) {}
    }
    stats.rank = currentUserData.Rank;
    stats.highest_score = currentUserData.HighestScore;
    stats.player_id = currentUserData.PlayerID;
    localStorage.setItem('user_stats', JSON.stringify(stats));
}

function renderAchievements(achievements) {
    achievements.forEach(ach => {
        const achievementId = ach.code || ach.achievement_id || ach.id || ach.title.toLowerCase().replace(/\s+/g, '_');
        const card = document.querySelector(`[data-achievement-id="${achievementId}"]`);
        if (!card) return;

        card.classList.toggle("unlocked", Boolean(ach.is_unlocked));

        const icon = card.querySelector(".achievement-icon-placeholder");
        if (icon) {
            icon.textContent = ach.is_unlocked ? "✓" : "🔒";
        }

        const progressContainer = card.querySelector(".achievement-progress-container");
        if (progressContainer) {
            if (ach.is_unlocked) {
                progressContainer.style.display = "none";
            } else {
                progressContainer.style.display = "block";
                const progressRows = card.querySelectorAll("[data-progress-key]");
                progressRows.forEach(progressItem => {
                    const key = progressItem.getAttribute("data-progress-key");
                    const currentVal = ach.progress && ach.progress[key] !== undefined ? ach.progress[key] : 0;
                    const defaultMax = parseInt(progressItem.getAttribute("data-default-max"), 10);
                    const maxVal = ach.max_progress && ach.max_progress[key] !== undefined ? ach.max_progress[key] : defaultMax;
                    const percentage = maxVal > 0 ? Math.min(100, Math.floor((currentVal / maxVal) * 100)) : 0;

                    const fillBar = progressItem.querySelector(".achievement-progress-bar-fill");
                    const textVal = progressItem.querySelector(".achievement-progress-text");

                    if (fillBar) fillBar.style.width = `${percentage}%`;
                    if (textVal) textVal.textContent = `${currentVal} / ${maxVal}`;
                });
            }
        }
    });
}

function getCachedJSON(key) {
    const cached = localStorage.getItem(key);
    if (!cached) return null;
    try {
        return JSON.parse(cached);
    } catch (e) {
        localStorage.removeItem(key);
        return null;
    }
}

function escapeHtml(str) {
    return String(str).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function formatGameTime(totalSeconds) {
    if (!totalSeconds || totalSeconds <= 0) return '0s';
    const days = Math.floor(totalSeconds / 86400);
    const hours = Math.floor((totalSeconds % 86400) / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = Math.floor(totalSeconds % 60);

    const parts = [];
    if (days > 0) parts.push(`${days}d`);
    if (hours > 0) parts.push(`${hours}h`);
    if (minutes > 0) parts.push(`${minutes}m`);
    if (seconds > 0 || parts.length === 0) parts.push(`${seconds}s`);

    return parts.join(' ');
}

// ==========================================
// CONFIGURATION TAB MANAGEMENT
// ==========================================
let configState = { master_volume: 100, sfx_volume: 100, keybinds: {} };
let savedConfigState = { master_volume: 100, sfx_volume: 100, keybinds: {} };
let activeRebindBtn = null;

async function initConfigTabEvents() {
    // Fetch saved server configuration
    try {
        const res = await fetch('/api/users/me/config');
        if (res.ok) {
            const serverConfig = await res.json();
            configState = {
                ...configState,
                ...serverConfig,
                keybinds: { ...configState.keybinds, ...(serverConfig.keybinds || {}) }
            };
            // Cache initial loaded state for "Reset Defaults / Revert" functionality
            savedConfigState = structuredClone(configState);
        }
    } catch (e) {
        console.warn("Using default config state:", e);
    }

    // Populate HTML with the current state
    syncConfigUI();

    // Sub-tab toggle logic
    const subTabBtns = document.querySelectorAll('.config-subtab-btn');
    const subContents = document.querySelectorAll('.config-subcontent');

    subTabBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            const targetId = btn.getAttribute('data-subtab');
            subTabBtns.forEach(b => b.classList.remove('active'));
            subContents.forEach(c => c.classList.remove('active'));

            btn.classList.add('active');
            const target = document.getElementById(targetId);
            if (target) target.classList.add('active');
        });
    });

    // Slider updates (Pending state)
    const masterSlider = document.getElementById('master-volume');
    const masterVal = document.getElementById('master-volume-val');
    if (masterSlider) {
        masterSlider.addEventListener('input', (e) => {
            const val = parseInt(e.target.value, 10);
            configState.master_volume = val;
            if (masterVal) masterVal.textContent = `${val}%`;
        });
    }

    const sfxSlider = document.getElementById('sfx-volume');
    const sfxVal = document.getElementById('sfx-volume-val');
    if (sfxSlider) {
        sfxSlider.addEventListener('input', (e) => {
            const val = parseInt(e.target.value, 10);
            configState.sfx_volume = val;
            if (sfxVal) sfxVal.textContent = `${val}%`;
        });
    }

    // Click badge to start key capture
    const keyBadges = document.querySelectorAll('.key-badge');
    keyBadges.forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (activeRebindBtn) activeRebindBtn.classList.remove('rebinding');

            activeRebindBtn = btn;
            btn.classList.add('rebinding');
            btn.textContent = "PRESS...";
        });
    });

    // Global keydown listener to capture rebinding input
    document.addEventListener('keydown', (e) => {
        if (!activeRebindBtn) return;
        e.preventDefault();

        const action = activeRebindBtn.getAttribute('data-action');
        let pressedKey = e.key.toLowerCase();

        // Allow Escape to cancel rebinding mode gracefully
        if (e.key === "Escape") {
            syncConfigUI();
            activeRebindBtn = null;
            return;
        }

        if (e.key === "Enter") {
            syncConfigUI();
            activeRebindBtn = null;
            showConfigStatus("Enter cannot be assigned.", true);
            return;
        }

        if (pressedKey.length > 1) {
            syncConfigUI();
            activeRebindBtn = null;
            showConfigStatus("Modifier/special keys not allowed", true);
            return;
        }

        if (!configState.keybinds) configState.keybinds = {};
        configState.keybinds[action] = pressedKey;

        activeRebindBtn.textContent = pressedKey.length === 1 ? pressedKey.toUpperCase() : pressedKey;
        activeRebindBtn.classList.remove('rebinding');
        activeRebindBtn = null;
    });

    // Cancel rebinding on outside click
    document.addEventListener('click', () => {
        if (activeRebindBtn) {
            syncConfigUI();
            activeRebindBtn = null;
        }
    });

    // Reset Defaults / Revert Unsaved Changes
    const btnReset = document.getElementById('btn-config-reset');
    if (btnReset) {
        btnReset.addEventListener('click', () => {
            configState = structuredClone(savedConfigState);
            syncConfigUI();
            showConfigStatus("Reverted unsaved changes");
        });
    }

    // Save Changes
    const btnSave = document.getElementById('btn-config-save');
    if (btnSave) {
        btnSave.addEventListener('click', async () => {
            showConfigStatus("Saving...");
            try {
                const response = await fetch('/api/users/me/config', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(configState)
                });

                if (!response.ok) {
                    const errorMsg = await response.text();
                    throw new Error(errorMsg.trim() || "Failed to save configuration");
                }

                const updatedConfig = await response.json();
                
                // Update both local state and baseline cache upon successful save
                configState = structuredClone(updatedConfig);
                savedConfigState = structuredClone(updatedConfig);

                syncConfigUI();
                showConfigStatus("Changes saved!");
            } catch (err) {
                console.error("Config save error:", err);
                showConfigStatus(err.message, true);
            }
        });
    }
}

// UI Sync Helper
function syncConfigUI() {
    const keyBadges = document.querySelectorAll('.key-badge');
    keyBadges.forEach(btn => {
        const action = btn.getAttribute('data-action');
        if (configState.keybinds && configState.keybinds[action]) {
            const key = configState.keybinds[action];
            btn.textContent = key.length === 1 ? key.toUpperCase() : key;
        }
        btn.classList.remove('rebinding');
    });

    const masterSlider = document.getElementById('master-volume');
    const masterVal = document.getElementById('master-volume-val');
    if (masterSlider && configState.master_volume !== undefined) {
        masterSlider.value = configState.master_volume;
        if (masterVal) masterVal.textContent = `${configState.master_volume}%`;
    }

    const sfxSlider = document.getElementById('sfx-volume');
    const sfxVal = document.getElementById('sfx-volume-val');
    if (sfxSlider && configState.sfx_volume !== undefined) {
        sfxSlider.value = configState.sfx_volume;
        if (sfxVal) sfxVal.textContent = `${configState.sfx_volume}%`;
    }
}

function showConfigStatus(msg, isError = false) {
    const statusMsg = document.getElementById('config-status-msg');
    if (!statusMsg) return;
    statusMsg.textContent = msg;
    statusMsg.style.color = isError ? "#ef4444" : "#34d399";
    setTimeout(() => {
        if (statusMsg.textContent === msg) statusMsg.textContent = "";
    }, 3000);
}