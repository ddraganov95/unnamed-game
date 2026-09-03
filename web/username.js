document.addEventListener('DOMContentLoaded', () => {
    //Wipe stale cache as soon as the login page opens
    localStorage.removeItem('user_stats');

    const submitBtn = document.getElementById('submit-btn');
    const usernameInput = document.getElementById('username');

    if (submitBtn) {
        submitBtn.addEventListener('click', () => {
            const username = usernameInput ? usernameInput.value.trim() : '';
            if (!username) {
                alert("Please enter a valid name.");
                return;
            }
            submitName(username);
        });
    }

    if (usernameInput) {
        usernameInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && submitBtn) {
                submitBtn.click();
            }
        });
    }
});

async function submitName(playerName) {
    try {
        const res = await fetch("/api/users", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ player_id: playerName })
        });

        if (!res.ok) {
            const errText = await res.text();
            console.error("Server Error:", res.status, errText);
            alert(`Error: ${errText}`);
            return;
        }

        //Ensure local cache is clean before redirecting
        localStorage.removeItem('user_stats');

        const data = await res.json();
        if (data.redirect) {
            window.location.href = data.redirect;
        }
    } catch (err) {
        console.error("Caught rejected promise:", err);
    }
}