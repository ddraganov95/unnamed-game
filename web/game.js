(function() {
    const term = new Terminal({
        cursorBlink: true,
        rows: 60,
        cols: 200,
    });

    // Open terminal inside our HTML div
    term.open(document.getElementById('terminal'));

    //Connect to Go WebSocket backend
    const urlParams = new URLSearchParams(window.location.search);
    const gameId = urlParams.get('gameId');
    const playerId = urlParams.get('playerId');

    // Connect using the parameters provided by the lobby API redirect
    const ws = new WebSocket(`ws://${window.location.host}/ws?gameId=${encodeURIComponent(gameId)}&playerId=${encodeURIComponent(playerId)}`);
    
    let connectionEstablished = false;
   ws.onopen = () => {
    connectionEstablished = true;
    term.writeln("Connected to dungeon server...\r\n");
};

    //Receive frames from Go and write them to xterm
    ws.onmessage = (event) => {
        try {
            //Try parsing incoming data as JSON for structured background actions
            const data = JSON.parse(event.data);
            if (data && data.type === "copy_clipboard") {
                navigator.clipboard.writeText(data.payload).then(() => {
                    console.log("Game ID copied to clipboard:", data.payload);
                }).catch(err => {
                    console.error("Failed to copy Game ID:", err);
                });
                return; //Prevent writing the control packet payload into the terminal view
            }
        } catch (e) {
            //Fallback: If JSON parsing fails, it's a standard raw terminal frame string
        }

        term.write(event.data);
    };

    ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        term.writeln("\r\nConnection lost.");
    };

    ws.onclose = () => {
    if (!connectionEstablished) {
        // Handshake failed
        window.location.href = '/lobby.html?error=' + encodeURIComponent("Connection rejected: Game is full or unavailable.");
    } else {
        // Disconnected mid-game after playing
        term.writeln("\r\nDisconnected from server. Returning to lobby...");
        setTimeout(() => {
            window.location.href = '/lobby.html';
        }, 1000);
    }
};

    //Capture keyboard inputs and send them to Go
    term.onData(data => {
        if (ws.readyState === WebSocket.OPEN) {
            ws.send(data);
        }
    });
})();