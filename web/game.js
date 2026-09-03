(function() {
    const term = new Terminal({
        cursorBlink: true,
        rows: 60,
        cols: 200,
    });

    // Open terminal inside HTML container
    term.open(document.getElementById('terminal'));

    // Extract gameId from URL
    const urlParams = new URLSearchParams(window.location.search);
    const gameId = urlParams.get('gameId');

    if (!gameId) {
        window.location.href = '/lobby.html?error=' + encodeURIComponent("Missing Game ID.");
        return;
    }

    // Connect to WebSocket — browser automatically attaches player_session HttpOnly cookie
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

    let connectionEstablished = false;

    ws.onopen = () => {
        connectionEstablished = true;
        term.writeln("Connected to dungeon server...\r\n");
    };

    // Receive frames from Go and write them to xterm
    ws.onmessage = (event) => {
    try {
        // Try parsing incoming data as JSON for background actions
        const data = JSON.parse(event.data);

        if (data && data.type === "copy_clipboard") {
            navigator.clipboard.writeText(data.payload).then(() => {
                console.log("Game ID copied to clipboard:", data.payload);
            }).catch(err => {
                console.error("Failed to copy Game ID:", err);
            });
            return; // Prevent rendering JSON payload inside xterm
        }

        if (data && data.type === "session_summary") {
            const userData = data.user || data.payload;
            if (userData) {
                localStorage.setItem('user_stats', JSON.stringify(userData));
            }
            window.location.href = "/lobby.html";
            return; // Prevent rendering JSON payload inside xterm
        }
    } catch (e) {
        // Standard raw terminal frame output
    }

    term.write(event.data);
};

    ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        term.writeln("\r\nConnection lost.");
    };

    ws.onclose = () => {
        if (!connectionEstablished) {
            // Handshake failed or rejected by server
            window.location.href = '/lobby.html?error=' + encodeURIComponent("Connection rejected: Game is full or unavailable.");
        } else {
            // Disconnected mid-game
            term.writeln("\r\nDisconnected from server. Returning to lobby...");
            setTimeout(() => {
                window.location.href = '/lobby.html';
            }, 1000);
        }
    };

    // Capture keyboard inputs and stream to Go
    term.onData(data => {
        if (ws.readyState === WebSocket.OPEN) {
            ws.send(data);
        }
    });
})();