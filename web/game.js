(function() {
    const term = new Terminal({
        cursorBlink: true,
        rows: 60,
        cols: 200,
    });

    // Open terminal inside our HTML div
    term.open(document.getElementById('terminal'));

    //Connect to Go WebSocket backend
    // Hardcoded name for testing
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";

    const myPlayerName = "HeroOfBulgaria"; 

    //Build the full dynamic URL including the query parameter
    const wsUrl = `${protocol}//${window.location.host}/ws?name=${encodeURIComponent(myPlayerName)}`;

    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
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
        term.writeln("\r\nDisconnected from server.");
    };

    //Capture keyboard inputs and send them to Go
    term.onData(data => {
        if (ws.readyState === WebSocket.OPEN) {
            ws.send(data);
        }
    });
})();