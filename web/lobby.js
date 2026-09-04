document.addEventListener('DOMContentLoaded', () => {
    const errorLabel = document.getElementById('error-label');
    const chatBox = document.getElementById('chat-box');
    const chatInput = document.getElementById('chat-input');
    const btnStart = document.getElementById('btn-start');
    const btnJoin = document.getElementById('btn-join');
    const btnSendChat = document.getElementById('btn-send-chat');

    // Display URL error params if present
    const urlParams = new URLSearchParams(window.location.search);
    const errorMsg = urlParams.get('error');
    if (errorMsg && errorLabel) {
        errorLabel.textContent = errorMsg;
    }

    // Tab Navigation
    const tabBtns = document.querySelectorAll('.tab-btn');
    const tabContents = document.querySelectorAll('.tab-content');
    let profileLoaded = false;

    tabBtns.forEach(btn => {
        btn.addEventListener('click', async () => {
            const targetTab = btn.getAttribute('data-tab');

            tabBtns.forEach(b => b.classList.remove('active'));
            tabContents.forEach(c => c.classList.remove('active'));

            btn.classList.add('active');
            const activeContent = document.getElementById(targetTab);
            if (activeContent) activeContent.classList.add('active');

            if (targetTab === 'tab-profile' && !profileLoaded) {
                const tabProfileContainer = document.getElementById('tab-profile');

                try {
                    // 1. Fetch template partial
                    const htmlRes = await fetch('/profile.html');
                    if (!htmlRes.ok) throw new Error(`HTML template fetch failed: ${htmlRes.statusText}`);
                    tabProfileContainer.innerHTML = await htmlRes.text();

                    // 2. Read cached stats directly from localStorage
                    let userData = null;
                    const cached = localStorage.getItem('user_stats');

                    if (cached) {
                        try {
                            userData = JSON.parse(cached);
                        } catch (e) {
                            localStorage.removeItem('user_stats');
                        }
                    }

                    // 3. Fallback: Fetch fresh data if cache missing
                    if (!userData) {
                       const apiRes = await fetch('/api/users/me');

                        if (!apiRes.ok) {
                            if (apiRes.status === 401 || apiRes.status === 403) {
                                localStorage.removeItem('user_stats');
                                window.location.href = '/'; // Kick unauthenticated user to login
                                return;
                            }
                            throw new Error(`API fetch failed: ${apiRes.status}`);
                        }

                        userData = await apiRes.json();
                        localStorage.setItem('user_stats', JSON.stringify(userData));
                    }

                    // 4. Populate UI
                        document.getElementById('stat-player-id').textContent = userData.player_id || '--';
                        document.getElementById('stat-highest-level').textContent = userData.highest_player_level ?? 1;
                        document.getElementById('stat-levels').textContent = userData.total_levels_completed ?? 0;
                        document.getElementById('stat-enemies').textContent = userData.total_enemies_killed ?? 0;
                        document.getElementById('stat-xp').textContent = userData.total_xp_gained ?? 0;
                        document.getElementById('stat-damage-dealt').textContent = userData.total_damage_dealt ?? 0;
                        document.getElementById('stat-damage-taken').textContent = userData.total_damage_taken ?? 0;
                        document.getElementById('stat-deaths').textContent = userData.total_deaths ?? 0;
                        document.getElementById('stat-game-time').textContent = formatGameTime(userData.total_game_time);

                    profileLoaded = true;
                } catch (err) {
                    console.error("[Profile Load Error]:", err);
                    tabProfileContainer.innerHTML = `<p class="profile-error">Unable to load profile stats.</p>`;
                }
            }
        });
    });

    // Start New Game
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

    // Join Existing Game
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
                const response = await fetch(`/api/games/${encodeURIComponent(gameId)}/join`, {
                    method: 'POST'
                });

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

    // Connect Global Chat WebSocket
    if (chatBox && chatInput) {
        const protocol = location.protocol === "https:" ? "wss:" : "ws:";
        const chatSocket = new WebSocket(`${protocol}//${window.location.host}/ws/global-chat`);

        chatSocket.onmessage = (event) => {
            const msgDiv = document.createElement('div');
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

        if (btnSendChat) {
            btnSendChat.addEventListener('click', sendChatMessage);
        }
        chatInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') sendChatMessage();
        });
    }
});
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