document.getElementById('submit-btn').addEventListener('click', () => {
    const username = document.getElementById('username').value.trim();
    if (!username) {
        alert("Please enter a valid name.");
        return;
    }

    localStorage.setItem('player_id', username);
    window.location.href = '/lobby.html';
});

document.getElementById('username').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        document.getElementById('submit-btn').click();
    }
});