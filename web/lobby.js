const playerId = localStorage.getItem('player_id');

if (!playerId) {
    window.location.href = '/index.html';
} else {
    const errorLabel = document.getElementById('error-label');

    // Check if we were redirected back here with an error message
    const urlParams = new URLSearchParams(window.location.search);
    const errorMsg = urlParams.get('error');
    if (errorMsg) {
        errorLabel.textContent = errorMsg;
    }

    // Start New Game
    document.getElementById('btn-start').addEventListener('click', async () => {
        errorLabel.textContent = "";

        const response = await fetch('/api/games', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ player_id: playerId })
        });

        if (!response.ok) {
            const errorMessage = await response.text();
            errorLabel.textContent = errorMessage.trim();
            return;
        }

        const data = await response.json();
        window.location.href = `/game.html?gameId=${encodeURIComponent(data.game_id)}&playerId=${encodeURIComponent(playerId)}`;
    });

    // Join Existing Game
    document.getElementById('btn-join').addEventListener('click', async () => {
        errorLabel.textContent = "";
        const gameId = document.getElementById('game-id-input').value.trim();
        
        if (!gameId) {
            errorLabel.textContent = "Please paste a Game ID.";
            return;
        }

        const response = await fetch(`/api/games/${encodeURIComponent(gameId)}/join`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({player_id: playerId })
        });

        if (!response.ok) {
            const errorMessage = await response.text();
            errorLabel.textContent = errorMessage.trim();
            return;
        }
        
        const data = await response.json();
        window.location.href = `/game.html?gameId=${encodeURIComponent(data.game_id)}&playerId=${encodeURIComponent(playerId)}`;
    });
}