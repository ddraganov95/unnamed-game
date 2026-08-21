(function() {
    const term = new Terminal({
        cursorBlink: true,
        rows: 60,
        cols: 200,
    });

    // Open terminal inside our HTML div
    term.open(document.getElementById('terminal'));

    //Connect to Go WebSocket backend
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

    ws.onopen = () => {
        term.writeln("Connected to dungeon server...\r\n");
    };

    //Receive frames from Go and write them to xterm
    ws.onmessage = (event) => {
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