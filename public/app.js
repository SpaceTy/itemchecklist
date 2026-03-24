const state = {
    currentUser: null,
    isAdmin: false,
    isFrozen: false,
    registrationLockedDown: false,
    accessibleLists: [],
    allLists: [],
    users: [],
    activeListId: localStorage.getItem("activeListId") || "",
    activeList: null,
    activeItems: [],
    managedOwnerListId: "",
    managedAdminListId: "",
    ownerManagedList: null,
    adminManagedList: null,
    claimMode: false,
    completionMode: false,
    searchQuery: "",
    eventSource: null,
    dragActive: false,
    pendingRender: null
};

const authCard = document.getElementById("authCard");
const appView = document.getElementById("appView");
const sessionBar = document.getElementById("sessionBar");
const currentUserEl = document.getElementById("currentUser");
const userStateBadgeEl = document.getElementById("userStateBadge");
const activeListTitleEl = document.getElementById("activeListTitle");
const activeListMetaEl = document.getElementById("activeListMeta");
const globalStatusEl = document.getElementById("globalStatus");
const inviteCodeInput = document.getElementById("inviteCodeInput");
const joinListBtn = document.getElementById("joinListBtn");
const itemsEl = document.getElementById("items");
const emptyChecklistEl = document.getElementById("emptyChecklist");
const listSelectorEl = document.getElementById("listSelector");
const claimToggle = document.getElementById("claimToggle");
const completionToggle = document.getElementById("completionToggle");
const completionLabel = document.getElementById("completionLabel");
const completionBar = document.getElementById("completion-bar");
const searchInput = document.getElementById("searchInput");
const clearSearchBtn = document.getElementById("clearSearch");
const sortModeEl = document.getElementById("sortMode");
const finishedPriorityEl = document.getElementById("finishedPriority");
const listMenuWrap = document.getElementById("listMenuWrap");
const listMenuBtn = document.getElementById("listMenuBtn");
const listMenuDropdown = document.getElementById("listMenuDropdown");
const listMenuItemsEl = document.getElementById("listMenuItems");
const ownerManageCardEl = document.getElementById("ownerManageCard");
const adminManageCardEl = document.getElementById("adminManageCard");
const adminListsEl = document.getElementById("adminLists");
const adminNavBtn = document.getElementById("adminNavBtn");
const adminStatusEl = document.getElementById("adminStatus");
const registrationLockBtn = document.getElementById("registrationLockBtn");
const registrationLockStateEl = document.getElementById("registrationLockState");
const userListEl = document.getElementById("userList");

document.getElementById("showRegister").onclick = (event) => {
    event.preventDefault();
    document.getElementById("loginPanel").style.display = "none";
    document.getElementById("registerPanel").style.display = "flex";
};

document.getElementById("showLogin").onclick = (event) => {
    event.preventDefault();
    document.getElementById("registerPanel").style.display = "none";
    document.getElementById("loginPanel").style.display = "flex";
};

document.getElementById("loginBtn").onclick = async () => {
    const payload = {
        username: document.getElementById("loginUsername").value.trim(),
        password: document.getElementById("loginPassword").value
    };
    const result = await api("api/login", { method: "POST", body: payload });
    if (!result.ok) {
        document.getElementById("loginMsg").textContent = result.error || "Login failed";
        return;
    }
    document.getElementById("loginMsg").textContent = "";
    await authCheck();
};

document.getElementById("registerBtn").onclick = async () => {
    const payload = {
        username: document.getElementById("regUsername").value.trim(),
        password: document.getElementById("regPassword").value
    };
    const result = await api("api/register", { method: "POST", body: payload });
    if (!result.ok) {
        document.getElementById("registerMsg").textContent = result.error || "Registration failed";
        return;
    }
    document.getElementById("registerMsg").textContent = "";
    await authCheck();
};

document.getElementById("logoutBtn").onclick = async () => {
    stopStream();
    await api("api/logout", { method: "POST" });
    localStorage.removeItem("activeListId");
    state.currentUser = null;
    state.activeListId = "";
    state.activeList = null;
    state.activeItems = [];
    renderSession();
    await authCheck();
};

document.getElementById("importListForm").onsubmit = async (event) => {
    event.preventDefault();
    const nameInput = document.getElementById("createListName");
    const fileInput = document.getElementById("litematicaFile");
    const honorAvailable = document.getElementById("honorAvailable").checked;
    const file = fileInput.files[0];

    if (!file) {
        setGlobalStatus("Choose a Litematica material list file first.", "error");
        return;
    }

    const formData = new FormData();
    formData.set("file", file);
    formData.set("name", nameInput.value.trim());
    formData.set("honor_available", String(honorAvailable));

    const result = await apiMultipart("api/lists/import", formData);
    if (!result.ok) {
        setGlobalStatus(result.error || "Could not import list", "error");
        return;
    }
    nameInput.value = "";
    fileInput.value = "";
    document.getElementById("honorAvailable").checked = false;
    setGlobalStatus(`Imported "${result.data.list.name}".`, "success");
    await refreshAllData();
    setActiveList(result.data.list.id);
    state.managedOwnerListId = result.data.list.id;
    await loadManagedOwnerList();
};

joinListBtn.onclick = async () => {
    const code = inviteCodeInput.value.trim();
    if (!code) {
        setGlobalStatus("Enter an invite code first.", "error");
        return;
    }
    const result = await api("api/lists/join", {
        method: "POST",
        body: { code }
    });
    if (!result.ok) {
        setGlobalStatus(result.error || "Could not join list.", "error");
        return;
    }
    inviteCodeInput.value = "";
    setGlobalStatus(`Joined "${result.data.list.name}".`, "success");
    await refreshAllData();
    setActiveList(result.data.list.id);
};

claimToggle.onchange = () => {
    state.claimMode = claimToggle.checked;
};

completionToggle.onchange = () => {
    state.completionMode = completionToggle.checked;
    completionLabel.textContent = state.completionMode ? "Item Based" : "Panel Based";
    updateCompletionBar();
};

searchInput.oninput = () => {
    state.searchQuery = searchInput.value;
    clearSearchBtn.style.display = state.searchQuery ? "inline-flex" : "none";
    renderChecklist();
};

clearSearchBtn.onclick = () => {
    searchInput.value = "";
    state.searchQuery = "";
    clearSearchBtn.style.display = "none";
    renderChecklist();
};

sortModeEl.onchange = () => renderChecklist();
finishedPriorityEl.onchange = () => renderChecklist();

completionBar.onclick = () => {
    completionToggle.checked = !completionToggle.checked;
    completionToggle.dispatchEvent(new Event("change"));
};

registrationLockBtn.onclick = async () => {
    const nextValue = !state.registrationLockedDown;
    setAdminStatus(`${nextValue ? "Enabling" : "Disabling"} registration lockdown...`, "info");
    const result = await api("api/admin/settings", {
        method: "POST",
        body: { registration_locked_down: nextValue }
    });
    if (!result.ok) {
        setAdminStatus(result.error || "Could not update settings.", "error");
        return;
    }
    state.registrationLockedDown = nextValue;
    renderAdminSettings();
    setAdminStatus("Registration settings updated.", "success");
};

document.querySelectorAll(".nav-btn").forEach((button) => {
    button.onclick = () => setView(button.dataset.view);
});

listMenuBtn.onclick = (event) => {
    event.stopPropagation();
    const isOpen = listMenuDropdown.style.display !== "none";
    listMenuDropdown.style.display = isOpen ? "none" : "block";
};

document.addEventListener("click", (event) => {
    if (!listMenuWrap.contains(event.target)) {
        listMenuDropdown.style.display = "none";
    }
});

let addItemModalListId = "";
let addItemModalAdminMode = false;

function openAddItemModal(listId, adminMode) {
    addItemModalListId = listId;
    addItemModalAdminMode = adminMode;
    const modal = document.getElementById("addItemModal");
    modal.style.display = "flex";
    const form = document.getElementById("addItemForm");
    form.elements.name.value = "";
    form.elements.target.value = "";
    form.elements.name.focus();
}

function closeAddItemModal() {
    document.getElementById("addItemModal").style.display = "none";
}

document.getElementById("closeAddItemModal").onclick = closeAddItemModal;
document.getElementById("addItemModal").onclick = (event) => {
    if (event.target.id === "addItemModal") closeAddItemModal();
};

document.getElementById("addItemForm").onsubmit = async (event) => {
    event.preventDefault();
    const form = event.target;
    const name = form.elements.name.value.trim();
    const target = Number(form.elements.target.value);
    const result = await api(`api/lists/${addItemModalListId}/items`, {
        method: "POST",
        body: { name, target }
    });
    if (result.ok) {
        closeAddItemModal();
    }
    await handleManageRefresh(result, addItemModalAdminMode, `Saved "${name}".`);
};

function api(path, options = {}) {
    const fetchOptions = { method: options.method || "GET", headers: {} };
    if (options.body !== undefined) {
        fetchOptions.headers["Content-Type"] = "application/json";
        fetchOptions.body = JSON.stringify(options.body);
    }
    return fetch(path, fetchOptions)
        .then(async (response) => {
            const data = await response.json().catch(() => ({}));
            if (!response.ok) {
                return { ok: false, status: response.status, error: data.error || "Request failed" };
            }
            return { ok: true, status: response.status, data };
        })
        .catch(() => ({ ok: false, status: 0, error: "Network error" }));
}

function apiMultipart(path, formData) {
    return fetch(path, {
        method: "POST",
        body: formData
    })
        .then(async (response) => {
            const data = await response.json().catch(() => ({}));
            if (!response.ok) {
                return { ok: false, status: response.status, error: data.error || "Request failed" };
            }
            return { ok: true, status: response.status, data };
        })
        .catch(() => ({ ok: false, status: 0, error: "Network error" }));
}

async function authCheck() {
    const result = await api("api/check-auth");
    if (!result.ok) {
        authCard.style.display = "block";
        appView.style.display = "none";
        sessionBar.style.display = "none";
        stopStream();
        return;
    }

    state.currentUser = result.data.username;
    state.isAdmin = Boolean(result.data.admin);
    state.isFrozen = Boolean(result.data.frozen);

    authCard.style.display = "none";
    appView.style.display = "block";
    sessionBar.style.display = "flex";
    listMenuWrap.style.display = "block";
    adminNavBtn.style.display = state.isAdmin ? "inline-flex" : "none";

    renderSession();
    await refreshAllData();
    if (!state.activeListId && state.accessibleLists.length > 0) {
        setActiveList(state.accessibleLists[0].id);
    } else if (state.activeListId) {
        await loadActiveListItems();
    } else {
        renderHero();
        renderChecklist();
    }
}

async function refreshAllData() {
    const listsResult = await api("api/lists");
    if (listsResult.ok) {
        state.accessibleLists = listsResult.data;
    }

    if (state.isAdmin) {
        const [settingsResult, usersResult, allListsResult] = await Promise.all([
            api("api/admin/settings"),
            api("api/admin/users"),
            api("api/lists?scope=all")
        ]);
        if (settingsResult.ok) {
            state.registrationLockedDown = Boolean(settingsResult.data.registration_locked_down);
        }
        if (usersResult.ok) {
            state.users = usersResult.data;
        }
        if (allListsResult.ok) {
            state.allLists = allListsResult.data;
        }
        renderAdmin();
    } else {
        state.users = [];
        state.allLists = [];
    }

    if (state.activeListId && !state.accessibleLists.find((list) => list.id === state.activeListId)) {
        state.activeListId = state.accessibleLists[0]?.id || "";
        localStorage.setItem("activeListId", state.activeListId);
    }

    if (!state.managedOwnerListId || !state.accessibleLists.find((list) => list.id === state.managedOwnerListId && list.can_manage)) {
        state.managedOwnerListId = state.accessibleLists.find((list) => list.can_manage)?.id || "";
    }

    if (state.isAdmin && (!state.managedAdminListId || !state.allLists.find((list) => list.id === state.managedAdminListId))) {
        state.managedAdminListId = state.allLists[0]?.id || "";
    }

    renderHero();
    renderListMenu();
    renderOwnedLists();
    await loadManagedOwnerList();
    if (state.isAdmin) {
        await loadManagedAdminList();
    }
}

function renderSession() {
    currentUserEl.textContent = state.currentUser || "";
    if (!state.currentUser) {
        userStateBadgeEl.textContent = "";
        return;
    }

    const badges = [];
    if (state.isAdmin) {
        badges.push("admin");
    }
    if (state.isFrozen) {
        badges.push("frozen");
    }
    userStateBadgeEl.textContent = badges.join(" · ") || "member";
}

function setView(viewName) {
    document.querySelectorAll(".view-section").forEach((view) => {
        view.style.display = view.id === `view-${viewName}` ? "block" : "none";
    });
    document.querySelectorAll(".nav-btn").forEach((button) => {
        button.classList.toggle("active", button.dataset.view === viewName);
    });
    if (viewName === "admin" && state.isAdmin) {
        renderAdmin();
    }
}

function setActiveList(listID) {
    state.activeListId = listID;
    localStorage.setItem("activeListId", listID);
    loadActiveListItems();
}

async function loadActiveListItems() {
    if (!state.activeListId) {
        state.activeList = null;
        state.activeItems = [];
        renderHero();
        renderChecklist();
        stopStream();
        return;
    }

    state.activeList = state.accessibleLists.find((list) => list.id === state.activeListId)
        || state.allLists.find((list) => list.id === state.activeListId)
        || null;

    const result = await api(`api/lists/${state.activeListId}/items`);
    if (!result.ok) {
        setGlobalStatus(result.error || "Could not load list.", "error");
        return;
    }

    state.activeItems = result.data;
    renderHero();
    renderChecklist();
    startStream();
}

async function loadManagedOwnerList() {
    if (!state.managedOwnerListId) {
        state.ownerManagedList = null;
        renderOwnerManageCard();
        return;
    }
    const result = await api(`api/lists/${state.managedOwnerListId}`);
    state.ownerManagedList = result.ok ? result.data : null;
    renderOwnerManageCard();
}

async function loadManagedAdminList() {
    if (!state.managedAdminListId || !state.isAdmin) {
        state.adminManagedList = null;
        renderAdminManageCard();
        return;
    }
    const result = await api(`api/lists/${state.managedAdminListId}`);
    state.adminManagedList = result.ok ? result.data : null;
    renderAdminManageCard();
}

function renderHero() {
    if (!state.activeList) {
        activeListTitleEl.textContent = "No list selected";
        activeListMetaEl.textContent = "Create a list or join one with an invite code.";
        return;
    }
    activeListTitleEl.textContent = state.activeList.name;
    activeListMetaEl.textContent = `${state.activeList.owner_username} · ${state.activeList.role} · ${state.activeList.item_count} items`;
}

function renderListMenu() {
    listMenuItemsEl.innerHTML = "";
    if (state.accessibleLists.length === 0) {
        listMenuItemsEl.innerHTML = '<p class="muted" style="padding:12px">No lists available.</p>';
        return;
    }

    state.accessibleLists.forEach((list) => {
        const item = document.createElement("button");
        item.className = `list-menu-item ${list.id === state.activeListId ? "active" : ""}`;
        item.innerHTML = `
            <span class="list-menu-name">${escapeHtml(list.name)}</span>
            <span class="list-menu-author">by ${escapeHtml(list.owner_username)}</span>
        `;
        item.onclick = () => {
            setActiveList(list.id);
            setView("checklist");
            listMenuDropdown.style.display = "none";
        };
        listMenuItemsEl.appendChild(item);
    });
}

function enableDrag(slider, max, onEndDrag) {
    let dragging = false;
    const endDrag = (event) => {
        if (!dragging) return;
        dragging = false;
        state.dragActive = false;
        window.removeEventListener("pointerup", endDrag);
        window.removeEventListener("pointercancel", endDrag);
        if (event) {
            try { slider.releasePointerCapture(event.pointerId); } catch {}
        }
        if (state.pendingRender) {
            const next = state.pendingRender;
            state.pendingRender = null;
            state.activeItems = next;
            renderChecklist();
        }
        onEndDrag();
    };
    const setVal = (x) => {
        const rect = slider.getBoundingClientRect();
        const ratio = Math.min(Math.max((x - rect.left) / rect.width, 0), 1);
        slider.value = Math.round(ratio * max);
        slider.dispatchEvent(new Event("input"));
    };
    slider.onpointerdown = (event) => {
        dragging = true;
        state.dragActive = true;
        window.addEventListener("pointerup", endDrag);
        window.addEventListener("pointercancel", endDrag);
        try { slider.setPointerCapture(event.pointerId); } catch {}
        setVal(event.clientX);
    };
    slider.onpointermove = (event) => {
        if (dragging) setVal(event.clientX);
    };
    slider.onpointerup = endDrag;
    slider.onpointercancel = endDrag;
}

function sendSliderUpdate(item, value) {
    if (state.claimMode) {
        const remaining = Math.max(item.target - item.gathered, 0);
        const claimed = Math.min(Math.max(value - item.gathered, 0), remaining);
        claimItem(item.name, claimed);
        return;
    }
    updateItem(item.name, value);
}

function renderChecklist() {
    if (state.dragActive) {
        state.pendingRender = state.activeItems;
        return;
    }
    itemsEl.innerHTML = "";
    const hasList = Boolean(state.activeListId);
    emptyChecklistEl.style.display = hasList ? "none" : "block";
    completionBar.style.display = hasList ? "flex" : "none";
    claimToggle.disabled = !hasList || state.isFrozen;

    if (!hasList) {
        updateCompletionBar();
        return;
    }

    const filtered = filterAndSortItems(state.activeItems, state.searchQuery);
    if (filtered.length === 0) {
        itemsEl.innerHTML = '<div class="empty-state compact">No items match this filter.</div>';
        updateCompletionBar([]);
        return;
    }

    filtered.forEach((item) => {
        const card = document.createElement("article");
        const complete = item.gathered >= item.target;
        card.className = `item-card ${complete ? "complete" : ""}`;
        const safeName = escapeHtml(item.name);
        const highlighted = item._matchIndices ? highlightMatches(safeName, item._matchIndices) : safeName;
        card.innerHTML = `
            <div class="item-head">
                <div class="item-name">${highlighted}</div>
                <div class="item-count">${item.gathered} / ${item.target}</div>
            </div>
            <div class="slider-wrap">
                <input type="range" min="0" max="${item.target}" value="${item.gathered}" step="1">
                <div class="claims"></div>
            </div>
        `;

        const slider = card.querySelector("input");
        const countEl = card.querySelector(".item-count");
        slider.disabled = state.isFrozen;

        slider.oninput = () => {
            const value = Number(slider.value);
            countEl.textContent = `${value} / ${item.target}`;
            if (state.dragActive) return;
            sendSliderUpdate(item, value);
        };

        enableDrag(slider, item.target, () => {
            sendSliderUpdate(item, Number(slider.value));
        });

        paintClaims(card.querySelector(".claims"), item);
        itemsEl.appendChild(card);
    });

    updateCompletionBar(filtered);
}

function filterAndSortItems(items, query) {
    let nextItems = [...items];

    if (query.trim()) {
        nextItems = nextItems
            .map((item) => {
                const match = fuzzyMatch(query, item.name);
                return { ...item, _match: match };
            })
            .filter((item) => item._match.matched)
            .sort((a, b) => b._match.score - a._match.score)
            .map((item) => ({ ...item, _matchIndices: item._match.indices }));
        return nextItems;
    }

    const sortMode = sortModeEl.value;
    const finishedPriority = finishedPriorityEl.value;

    nextItems.sort((a, b) => {
        const aComplete = a.target > 0 && a.gathered >= a.target;
        const bComplete = b.target > 0 && b.gathered >= b.target;

        if (finishedPriority !== "neutral" && aComplete !== bComplete) {
            return finishedPriority === "first" ? (aComplete ? -1 : 1) : (aComplete ? 1 : -1);
        }

        if (sortMode === "alphabetical") {
            return a.name.localeCompare(b.name);
        }
        if (sortMode === "progress") {
            const aRatio = a.target > 0 ? a.gathered / a.target : 0;
            const bRatio = b.target > 0 ? b.gathered / b.target : 0;
            return bRatio - aRatio;
        }
        if (sortMode === "target") {
            return b.target - a.target;
        }
        return 0;
    });

    return nextItems;
}

function updateCompletionBar(items = state.activeItems) {
    const list = items || [];
    let percent = 0;

    if (state.completionMode) {
        let totalRatio = 0;
        let count = 0;
        list.forEach((item) => {
            if (item.target > 0) {
                totalRatio += item.gathered / item.target;
                count += 1;
            }
        });
        percent = count > 0 ? Math.round((totalRatio / count) * 100) : 0;
        completionBar.classList.add("item-based");
    } else {
        const totals = list.reduce((acc, item) => {
            acc.gathered += item.gathered;
            acc.target += item.target;
            return acc;
        }, { gathered: 0, target: 0 });
        percent = totals.target > 0 ? Math.round((totals.gathered / totals.target) * 100) : 0;
        completionBar.classList.remove("item-based");
    }

    completionBar.querySelector(".bar-fill").style.height = `${percent}%`;
    completionBar.querySelector(".bar-label").textContent = `${percent}%`;
}

function paintClaims(container, item) {
    container.innerHTML = "";
    (item.claims || []).forEach((claim) => {
        const widthBase = item.target || 1;
        const start = (100 * claim.claim_start) / widthBase;
        const width = Math.max((100 * (claim.claim_end - claim.claim_start)) / widthBase, 1);
        const chip = document.createElement("button");
        chip.className = "claim-chip";
        chip.style.left = `${start}%`;
        chip.style.width = `${width}%`;
        chip.textContent = claim.claimer;
        chip.title = claim.claimer === state.currentUser ? "Click to clear your claim" : claim.claimer;
        chip.disabled = claim.claimer !== state.currentUser;
        chip.onclick = () => claimItem(item.name, 0);
        container.appendChild(chip);
    });
}

async function updateItem(name, gathered) {
    const result = await api(`api/lists/${state.activeListId}/items/update`, {
        method: "POST",
        body: { name, gathered }
    });
    if (!result.ok) {
        setGlobalStatus(result.error || "Could not update item.", "error");
        await loadActiveListItems();
    }
}

async function claimItem(name, claimed) {
    const result = await api(`api/lists/${state.activeListId}/items/claim`, {
        method: "POST",
        body: { name, claimed }
    });
    if (!result.ok) {
        setGlobalStatus(result.error || "Could not update claim.", "error");
        await loadActiveListItems();
    }
}

function startStream() {
    stopStream();
    if (!state.activeListId) {
        return;
    }

    state.eventSource = new EventSource(`events?list_id=${encodeURIComponent(state.activeListId)}`);
    state.eventSource.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.type === "update" && data.list_id === state.activeListId) {
            if (state.dragActive) {
                state.pendingRender = data.items;
            } else {
                state.activeItems = data.items;
                renderChecklist();
            }
        }
    };
    state.eventSource.onerror = () => stopStream();
}

function stopStream() {
    if (state.eventSource) {
        state.eventSource.close();
        state.eventSource = null;
    }
}

function renderOwnedLists() {
    listSelectorEl.innerHTML = "";
    const ownedLists = state.accessibleLists.filter((list) => list.can_manage);
    if (ownedLists.length === 0) {
        listSelectorEl.innerHTML = '<div class="empty-state compact">You do not own any lists yet.</div>';
        return;
    }

    ownedLists.forEach((list) => {
        const btn = document.createElement("button");
        btn.className = `chooser-pill ${list.id === state.managedOwnerListId ? "active" : ""}`;
        btn.textContent = list.name;
        btn.onclick = async () => {
            state.managedOwnerListId = list.id;
            await loadManagedOwnerList();
            renderOwnedLists();
        };
        listSelectorEl.appendChild(btn);
    });
}

function renderOwnerManageCard() {
    if (!state.ownerManagedList) {
        ownerManageCardEl.innerHTML = '<div class="empty-state compact" style="margin-bottom:18px">Select one of your lists to manage it.</div>';
        return;
    }
    ownerManageCardEl.innerHTML = "";
    const panel = document.createElement("div");
    panel.className = "panel";
    panel.style.marginBottom = "18px";
    panel.appendChild(buildManagePanel(state.ownerManagedList, false));
    ownerManageCardEl.appendChild(panel);
}

function renderAdmin() {
    renderAdminSettings();
    renderAdminUsers();
    renderAdminLists();
    renderAdminManageCard();
}

function renderAdminSettings() {
    registrationLockBtn.textContent = state.registrationLockedDown ? "Unlock Registration" : "Lock Registration";
    registrationLockStateEl.textContent = state.registrationLockedDown
        ? "New accounts are created frozen."
        : "New accounts can contribute immediately.";
}

function renderAdminUsers() {
    userListEl.innerHTML = "";
    if (state.users.length === 0) {
        userListEl.innerHTML = '<div class="empty-state compact">No users found.</div>';
        return;
    }

    state.users.forEach((user) => {
        const row = document.createElement("div");
        row.className = "user-row";
        row.innerHTML = `
            <div class="user-summary">
                <strong>${escapeHtml(user.username)}</strong>
                <span class="pill subtle">${user.admin ? "admin" : "member"}</span>
                ${user.frozen ? '<span class="pill warn">frozen</span>' : ""}
            </div>
            <div class="user-actions">
                <button class="ghost-btn" data-action="toggle_frozen">${user.frozen ? "Unfreeze" : "Freeze"}</button>
                <button class="ghost-btn" data-action="purge_progress">Remove Edits</button>
                <button class="ghost-btn" data-action="toggle_admin">${user.admin ? "Remove Admin" : "Make Admin"}</button>
                <button class="danger-btn" data-action="delete" ${user.username === state.currentUser ? "disabled" : ""}>Delete</button>
            </div>
        `;

        row.querySelectorAll("button[data-action]").forEach((button) => {
            button.onclick = async () => {
                const action = button.dataset.action;
                if (action === "delete" && !confirm(`Delete ${user.username} and remove owned lists and contributions?`)) {
                    return;
                }
                if (action === "purge_progress" && !confirm(`Remove all contributions and claims by ${user.username}?`)) {
                    return;
                }
                setAdminStatus(`Updating ${user.username}...`, "info");
                const result = await api("api/admin/users", {
                    method: "POST",
                    body: { username: user.username, action }
                });
                if (!result.ok) {
                    setAdminStatus(result.error || "Admin action failed.", "error");
                    return;
                }
                setAdminStatus(`Updated ${user.username}.`, "success");
                await refreshAllData();
                await loadActiveListItems();
            };
        });

        userListEl.appendChild(row);
    });
}

function renderAdminLists() {
    adminListsEl.innerHTML = "";
    if (state.allLists.length === 0) {
        adminListsEl.innerHTML = '<div class="empty-state compact">No lists found.</div>';
        return;
    }

    state.allLists.forEach((list) => {
        adminListsEl.appendChild(createListCard(list, {
            onOpen: () => {
                setActiveList(list.id);
                setView("checklist");
            },
            onManage: async () => {
                state.managedAdminListId = list.id;
                await loadManagedAdminList();
            },
            onDelete: () => deleteList(list.id, true)
        }));
    });
}

function createListCard(list, actions) {
    const card = document.createElement("article");
    card.className = "list-card";
    card.innerHTML = `
        <div>
            <div class="list-card-head">
                <h3>${escapeHtml(list.name)}</h3>
                <span class="pill">${escapeHtml(list.role)}</span>
            </div>
            <p class="muted">Owner: ${escapeHtml(list.owner_username)}</p>
            <p class="muted">${list.item_count} items · ${list.collaborators.length} collaborators</p>
        </div>
        <div class="card-actions">
            <button>Open</button>
            <button class="ghost-btn">${list.can_manage || state.isAdmin ? "Manage" : "View"}</button>
            <button class="danger-btn">Delete</button>
        </div>
    `;
    const [openBtn, manageBtn, deleteBtn] = card.querySelectorAll("button");
    openBtn.onclick = actions.onOpen;
    manageBtn.onclick = actions.onManage;
    deleteBtn.onclick = actions.onDelete;
    if (!list.can_manage && !state.isAdmin) {
        deleteBtn.style.display = "none";
    }
    return card;
}

function buildManagePanel(list, adminMode) {
    const wrapper = document.createElement("div");
    wrapper.className = "manage-stack";

    /* ── Header: name shown once, editable via button ── */
    const header = document.createElement("div");
    header.className = "manage-head";
    header.innerHTML = `
        <div class="editable-name-group">
            <h3 class="editable-name-display">${escapeHtml(list.name)}</h3>
            <button class="ghost-btn edit-name-btn" title="Rename list">&#9998;</button>
            <div class="editable-name-form" style="display:none">
                <input type="text" value="${escapeHtmlAttr(list.name)}" class="name-input">
                <button type="button" class="ghost-btn save-name-btn">Save</button>
                <button type="button" class="ghost-btn cancel-name-btn">Cancel</button>
            </div>
        </div>
        <div class="manage-meta">
            <p class="muted" style="margin:0">Owner: ${escapeHtml(list.owner_username)} &middot; ${list.items.length} items</p>
            <div class="manage-actions">
                <span class="pill">${escapeHtml(list.role)}</span>
                <button class="ghost-btn open-list-btn">Open</button>
                <button class="danger-btn delete-list-btn">Delete</button>
            </div>
        </div>
    `;

    const nameDisplay = header.querySelector(".editable-name-display");
    const editBtn = header.querySelector(".edit-name-btn");
    const nameForm = header.querySelector(".editable-name-form");
    const nameInput = header.querySelector(".name-input");

    editBtn.onclick = () => {
        nameDisplay.style.display = "none";
        editBtn.style.display = "none";
        nameForm.style.display = "flex";
        nameInput.focus();
        nameInput.select();
    };
    header.querySelector(".cancel-name-btn").onclick = () => {
        nameDisplay.style.display = "";
        editBtn.style.display = "";
        nameForm.style.display = "none";
        nameInput.value = list.name;
    };
    header.querySelector(".save-name-btn").onclick = async () => {
        const name = nameInput.value.trim();
        if (!name) return;
        const result = await api(`api/lists/${list.id}`, {
            method: "PATCH",
            body: { action: "rename", name }
        });
        await handleManageRefresh(result, adminMode, `Renamed to "${name}".`);
    };

    header.querySelector(".open-list-btn").onclick = () => {
        setActiveList(list.id);
        setView("checklist");
    };
    header.querySelector(".delete-list-btn").onclick = () => deleteList(list.id, adminMode);

    wrapper.appendChild(header);

    /* ── Transfer ownership (admin only) ── */
    if (adminMode) {
        const transferForm = document.createElement("form");
        transferForm.className = "inline-form";
        transferForm.innerHTML = `
            <input type="text" name="transfer_owner" placeholder="Transfer owner to username">
            <button type="submit" class="ghost-btn">Transfer Ownership</button>
        `;
        transferForm.onsubmit = async (event) => {
            event.preventDefault();
            const transferOwner = transferForm.elements.transfer_owner.value.trim();
            const result = await api(`api/lists/${list.id}`, {
                method: "PATCH",
                body: { action: "transfer_owner", transfer_owner: transferOwner }
            });
            await handleManageRefresh(result, true, `Transferred ownership to ${transferOwner}.`);
        };
        wrapper.appendChild(transferForm);
    }

    /* ── Items section (add item via modal) ── */
    const itemsSection = document.createElement("section");
    itemsSection.className = "subpanel";
    itemsSection.innerHTML = `
        <div class="subpanel-head">
            <h4>Items</h4>
            <button class="ghost-btn add-item-trigger">+ Add Item</button>
        </div>
        <div class="managed-items" data-role="items"></div>
    `;

    itemsSection.querySelector(".add-item-trigger").onclick = () => {
        openAddItemModal(list.id, adminMode);
    };

    const itemsContainer = itemsSection.querySelector('[data-role="items"]');
    if (!list.items.length) {
        itemsContainer.innerHTML = '<div class="empty-state compact">This list has no items yet.</div>';
    } else {
        list.items
            .slice()
            .sort((a, b) => a.name.localeCompare(b.name))
            .forEach((item) => {
                const row = document.createElement("form");
                row.className = "managed-item-row";
                row.innerHTML = `
                    <input type="text" name="name" value="${escapeHtmlAttr(item.name)}">
                    <input type="number" name="target" value="${item.target}" min="0">
                    <button type="submit" class="ghost-btn">Save</button>
                    <button type="button" class="danger-btn">Delete</button>
                `;
                row.onsubmit = async (event) => {
                    event.preventDefault();
                    const result = await api(`api/lists/${list.id}/items`, {
                        method: "POST",
                        body: {
                            original_name: item.name,
                            name: row.elements.name.value.trim(),
                            target: Number(row.elements.target.value)
                        }
                    });
                    await handleManageRefresh(result, adminMode, `Updated "${item.name}".`);
                };
                row.querySelector(".danger-btn").onclick = async () => {
                    const result = await api(`api/lists/${list.id}/items`, {
                        method: "DELETE",
                        body: { name: item.name }
                    });
                    await handleManageRefresh(result, adminMode, `Deleted "${item.name}".`);
                };
                itemsContainer.appendChild(row);
            });
    }
    wrapper.appendChild(itemsSection);

    /* ── Collaborators section (at bottom, single panel) ── */
    const collabSection = document.createElement("section");
    collabSection.className = "subpanel";
    collabSection.innerHTML = `
        <div class="subpanel-head">
            <h4>Collaborators</h4>
            <button class="ghost-btn" data-action="new-invite">Generate Invite Code</button>
        </div>
        <div class="collab-content">
            <div data-role="invites"></div>
            <div data-role="members"></div>
        </div>
    `;

    collabSection.querySelector('[data-action="new-invite"]').onclick = async () => {
        const result = await api(`api/lists/${list.id}/invites`, { method: "POST" });
        await handleManageRefresh(result, adminMode, "Generated invite code.");
    };

    const invitesEl = collabSection.querySelector('[data-role="invites"]');
    if (list.invite_codes.length) {
        const inviteLabel = document.createElement("p");
        inviteLabel.className = "muted";
        inviteLabel.style.margin = "12px 0 6px";
        inviteLabel.textContent = "Invite Codes";
        invitesEl.appendChild(inviteLabel);

        list.invite_codes.forEach((invite) => {
            const row = document.createElement("div");
            row.className = "token-row";
            row.innerHTML = `
                <code>${escapeHtml(invite.code)}</code>
                <div class="token-actions">
                    <button class="ghost-btn">Copy</button>
                    <button class="danger-btn">Revoke</button>
                </div>
            `;
            row.querySelector(".ghost-btn").onclick = async () => {
                await navigator.clipboard.writeText(invite.code).catch(() => {});
                setGlobalStatus("Invite code copied.", "success");
            };
            row.querySelector(".danger-btn").onclick = async () => {
                const result = await api(`api/lists/${list.id}/invites`, {
                    method: "DELETE",
                    body: { code: invite.code }
                });
                await handleManageRefresh(result, adminMode, "Revoked invite code.");
            };
            invitesEl.appendChild(row);
        });
    }

    const membersEl = collabSection.querySelector('[data-role="members"]');
    if (list.collaborators.length) {
        const memberLabel = document.createElement("p");
        memberLabel.className = "muted";
        memberLabel.style.margin = "12px 0 6px";
        memberLabel.textContent = "Members";
        membersEl.appendChild(memberLabel);

        list.collaborators.forEach((username) => {
            const row = document.createElement("div");
            row.className = "token-row";
            row.innerHTML = `
                <span>${escapeHtml(username)}</span>
                <button class="danger-btn">Remove</button>
            `;
            row.querySelector("button").onclick = async () => {
                const result = await api(`api/lists/${list.id}/members`, {
                    method: "DELETE",
                    body: { username }
                });
                await handleManageRefresh(result, adminMode, `Removed ${username}.`);
            };
            membersEl.appendChild(row);
        });
    }

    if (!list.invite_codes.length && !list.collaborators.length) {
        collabSection.querySelector(".collab-content").innerHTML =
            '<p class="muted">No collaborators or invite codes yet. Generate an invite code to share this list.</p>';
    }

    wrapper.appendChild(collabSection);

    return wrapper;
}

async function handleManageRefresh(result, adminMode, successMessage) {
    if (!result.ok) {
        if (adminMode) {
            setAdminStatus(result.error || "Action failed.", "error");
        } else {
            setGlobalStatus(result.error || "Action failed.", "error");
        }
        return;
    }
    if (adminMode) {
        setAdminStatus(successMessage, "success");
    } else {
        setGlobalStatus(successMessage, "success");
    }
    await refreshAllData();
    await loadActiveListItems();
}

async function deleteList(listID, adminMode) {
    const target = (adminMode ? state.allLists : state.accessibleLists).find((list) => list.id === listID);
    if (!target) {
        return;
    }
    if (!confirm(`Delete "${target.name}"?`)) {
        return;
    }
    const result = await api(`api/lists/${listID}`, { method: "DELETE" });
    if (!result.ok) {
        if (adminMode) {
            setAdminStatus(result.error || "Could not delete list.", "error");
        } else {
            setGlobalStatus(result.error || "Could not delete list.", "error");
        }
        return;
    }
    if (state.activeListId === listID) {
        state.activeListId = "";
        localStorage.removeItem("activeListId");
        stopStream();
    }
    await refreshAllData();
    await loadActiveListItems();
}

function setGlobalStatus(message, kind) {
    globalStatusEl.textContent = message;
    globalStatusEl.dataset.kind = kind || "info";
}

function setAdminStatus(message, kind) {
    adminStatusEl.textContent = message;
    adminStatusEl.dataset.kind = kind || "info";
}

function renderAdminManageCard() {
    if (!state.isAdmin || !state.adminManagedList) {
        adminManageCardEl.innerHTML = '<div class="empty-state compact">Select a list from the admin list grid to manage it.</div>';
        return;
    }
    adminManageCardEl.innerHTML = "";
    adminManageCardEl.appendChild(buildManagePanel(state.adminManagedList, true));
}

function fuzzyMatch(pattern, text) {
    if (!pattern) {
        return { matched: true, score: 0, indices: [] };
    }

    const query = pattern.toLowerCase();
    const source = text.toLowerCase();
    let qIndex = 0;
    let score = 0;
    let consecutive = 0;
    const indices = [];

    for (let i = 0; i < source.length && qIndex < query.length; i += 1) {
        if (source[i] !== query[qIndex]) {
            consecutive = 0;
            continue;
        }

        indices.push(i);
        score += 1;
        if (i === 0 || [" ", "-", "_"].includes(source[i - 1])) {
            score += 6;
        }
        if (indices.length > 1 && indices[indices.length - 1] === indices[indices.length - 2] + 1) {
            consecutive += 1;
            score += 4 + consecutive;
        } else {
            consecutive = 0;
        }
        qIndex += 1;
    }

    if (qIndex !== query.length) {
        return { matched: false, score: -Infinity, indices: [] };
    }

    const gaps = indices.length > 1 ? indices[indices.length - 1] - indices[0] - indices.length + 1 : 0;
    score -= gaps * 0.5;
    score += (1 / (source.length + 1)) * 10;
    return { matched: true, score, indices };
}

function highlightMatches(text, indices) {
    const positions = new Set(indices);
    return text
        .split("")
        .map((char, index) => positions.has(index) ? `<mark>${char}</mark>` : char)
        .join("");
}

function escapeHtml(text) {
    return String(text)
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
}

function escapeHtmlAttr(text) {
    return escapeHtml(text);
}

authCheck();
