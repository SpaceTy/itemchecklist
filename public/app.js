const loginSection = document.getElementById('login-section');
const appSection = document.getElementById('app-section');
const passwordInput = document.getElementById('password-input');
const loginBtn = document.getElementById('login-btn');
const loginError = document.getElementById('login-error');
const itemsList = document.getElementById('items-list');
const searchInput = document.getElementById('search-input');

let items = [];

// --- Auth ---

async function checkAuth() {
    try {
        const res = await fetch('api/check-auth');
        if (res.ok) {
            showApp();
        } else {
            showLogin();
        }
    } catch (e) {
        showLogin();
    }
}

async function login() {
    const password = passwordInput.value;
    if (!password) return;

    try {
        const res = await fetch('api/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password })
        });

        if (res.ok) {
            showApp();
        } else {
            loginError.textContent = 'Invalid password';
        }
    } catch (e) {
        loginError.textContent = 'Error logging in';
    }
}

function showLogin() {
    loginSection.classList.remove('hidden');
    appSection.classList.add('hidden');
}

function showApp() {
    loginSection.classList.add('hidden');
    appSection.classList.remove('hidden');
    loadItems();
    initSSE();
}

loginBtn.addEventListener('click', login);
passwordInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') login();
});

// --- Items ---

async function loadItems() {
    try {
        const res = await fetch('api/items');
        if (res.ok) {
            items = await res.json();
            renderItems();
        }
    } catch (e) {
        console.error('Failed to load items', e);
    }
}

function renderItems() {
    const term = searchInput.value.toLowerCase();
    const filtered = items.filter(i => i.name.toLowerCase().includes(term));
    
    itemsList.innerHTML = filtered.map(item => {
        const percent = Math.min(100, Math.round((item.gathered / item.target) * 100));
        const isCompleted = item.gathered >= item.target;
        
        return `
            <div class="item-card ${isCompleted ? 'completed' : ''}">
                <div class="item-header">
                    <span class="item-name">${item.name}</span>
                    <span class="item-count">${item.gathered} / ${item.target}</span>
                </div>
                <div class="progress-container">
                    <div class="progress-bar" style="width: ${percent}%"></div>
                </div>
                <div class="slider-container">
                    <input type="range" min="0" max="${item.target}" value="${item.gathered}" 
                           oninput="updateSliderDisplay(this, '${item.name}', '${item.target}')"
                           onchange="updateItem('${item.name}', this.value)">
                    <span class="range-value" id="val-${item.name.replace(/\W/g, '')}">${item.gathered}</span>
                </div>
            </div>
        `;
    }).join('');
}

window.updateSliderDisplay = function(slider, name, target) {
    const valDisplay = document.getElementById(`val-${name.replace(/\W/g, '')}`);
    if (valDisplay) {
        valDisplay.innerText = slider.value;
    }
}

window.updateItem = async function(name, newAmount) {
    newAmount = parseInt(newAmount);
    if (isNaN(newAmount)) return;
    if (newAmount < 0) newAmount = 0;

    // Optimistic update
    const item = items.find(i => i.name === name);
    if (item) {
        item.gathered = newAmount;
        // Don't re-render entire list to avoid jitter while dragging (though onchange fires at end)
        // We might want to just update the text and progress bar if we wanted super smooth interaction
        // For simplicity, we can let renderItems run if we want to update the progress bar visual immediately
        // BUT if we re-render, we lose focus on the slider if we were still holding it (onchange is safe though)
        
        // Update local state visuals manually to keep it snappy without full re-render
        const card = document.querySelector(`input[type="range"][onchange*="${name}"]`).closest('.item-card');
        if (card) {
            const progressBar = card.querySelector('.progress-bar');
            const percent = Math.min(100, Math.round((newAmount / item.target) * 100));
            progressBar.style.width = `${percent}%`;
            
            const countText = card.querySelector('.item-count');
            countText.innerText = `${newAmount} / ${item.target}`;

            if (newAmount >= item.target) card.classList.add('completed');
            else card.classList.remove('completed');
        }
        
        try {
            await fetch('api/items/update', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, gathered: newAmount })
            });
        } catch (e) {
            console.error('Update failed', e);
        }
    }
};

searchInput.addEventListener('input', renderItems);

// --- SSE ---

function initSSE() {
    const evtSource = new EventSource('events');
    
    evtSource.onmessage = function(event) {
        const data = JSON.parse(event.data);
        if (data.type === 'update') {
            // Only update if we are not currently interacting with a slider to avoid jumps
            // Or just update the model and re-render
            items = data.items;
            
            // Re-render is safest to ensure consistency, but might disrupt active user.
            // Given "simple", we will just re-render.
            // Check if active element is a range input
            if (document.activeElement && document.activeElement.type === 'range') {
                // Skip re-render if user is dragging, maybe update data in background
            } else {
                renderItems();
            }
        }
    };
    
    evtSource.onerror = function(err) {
        console.error("EventSource failed:", err);
    };
}

// Start
checkAuth();
