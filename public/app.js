const itemsEl = document.getElementById("items");
const loginRow = document.getElementById("login");
const claimBtn = document.getElementById("claimToggle");
const claimLabel = document.getElementById("claimLabel");
const completionToggle = document.getElementById("completionToggle");
const completionLabel = document.getElementById("completionLabel");
const completionBar = document.getElementById('completion-bar');
const sortBtn = document.getElementById("sortBtn");
const sortModeEl = document.getElementById("sortMode");
const finishedPriorityEl = document.getElementById("finishedPriority");
const searchInput = document.getElementById("searchInput");
const clearSearchBtn = document.getElementById("clearSearch");
const adminStatusEl = document.getElementById("adminStatus");
const registrationLockBtn = document.getElementById("registrationLockBtn");
const registrationLockStateEl = document.getElementById("registrationLockState");
let claimMode = false;
let completionMode = false; // false = panel based, true = item based
let lastUpdate = {};
let dragActive = false;
let pendingRender = null;
let currentItems = [];
let searchQuery = "";
let allItems = [];
let currentUsername = null;
let isAdmin = false;
let isFrozen = false;
let eventSource = null;
let registrationLockedDown = false;

function setAdminStatus(message, kind = "info") {
  if (!adminStatusEl) return;
  adminStatusEl.textContent = message;
  adminStatusEl.className = `admin-status ${kind}`;
}

function setContributionAvailability() {
  claimBtn.disabled = isFrozen;
  completionToggle.disabled = false;
  if (isFrozen) {
    claimMode = false;
    claimBtn.checked = false;
    document.body.classList.remove("claim-mode");
    claimLabel.textContent = "Frozen";
    document.getElementById("currentUser").textContent = `${currentUsername} (frozen)`;
  } else {
    claimLabel.textContent = "Claim Mode";
    document.getElementById("currentUser").textContent = currentUsername || "";
  }
}

function renderRegistrationLockState() {
  if (!registrationLockBtn || !registrationLockStateEl) return;
  registrationLockBtn.textContent = registrationLockedDown ? "Unlock Registration" : "Lock Registration";
  registrationLockBtn.classList.toggle("btn-warning", !registrationLockedDown);
  registrationLockBtn.classList.toggle("btn-danger", registrationLockedDown);
  registrationLockStateEl.textContent = registrationLockedDown
    ? "Registration lockdown is on. New accounts are created frozen."
    : "Registration lockdown is off. New accounts can contribute immediately.";
}

function computeTotalCompletion(items) {
  let totalGathered = 0;
  let totalTarget = 0;
  items.forEach(item => {
    totalGathered += item.gathered;
    totalTarget += item.target;
  });
  return { totalGathered, totalTarget };
}

function computeItemBasedCompletion(items) {
  let totalRatio = 0;
  let count = 0;
  items.forEach(item => {
    if (item.target > 0) {
      totalRatio += item.gathered / item.target;
      count++;
    }
  });
  return count > 0 ? totalRatio / count : 0;
}

function updateCompletionBar(items) {
  const bar = document.getElementById('completion-bar');
  if (!bar) return;
  let percent;
  if (completionMode) {
    // item based: average completion per item
    const ratio = computeItemBasedCompletion(items);
    percent = Math.round(ratio * 100);
  } else {
    // panel based: total gathered / total target
    const { totalGathered, totalTarget } = computeTotalCompletion(items);
    percent = totalTarget > 0 ? Math.round((totalGathered / totalTarget) * 100) : 0;
  }
  const fill = bar.querySelector('.bar-fill');
  const label = bar.querySelector('.bar-label');
  if (fill) {
    fill.style.height = `${percent}%`;
  }
  if (label) {
    label.textContent = `${percent}%`;
  }
}

async function authCheck() {
  const res = await fetch("api/check-auth");
  if (res.ok) {
    const data = await res.json();
    currentUsername = data.username;
    isAdmin = data.admin;
    isFrozen = Boolean(data.frozen);
    loginRow.style.display = "none";
    document.getElementById("userBar").style.display = "flex";
    setContributionAvailability();
    if (isAdmin) {
      document.getElementById("adminPanel").style.display = "block";
      setAdminStatus("Use Remove Edits to revoke one account's contributions and claims.", "info");
      loadAdminSettings();
      loadAdminUsers();
    } else {
      document.getElementById("adminPanel").style.display = "none";
    }
    loadItems();
    startStream();
  } else {
    currentUsername = null;
    isAdmin = false;
    isFrozen = false;
    loginRow.style.display = "flex";
    document.getElementById("userBar").style.display = "none";
    document.getElementById("adminPanel").style.display = "none";
    setAdminStatus("", "info");
  }
}

document.getElementById("loginBtn").onclick = async () => {
  const msgEl = document.getElementById("loginMsg");
  msgEl.textContent = "";
  const username = document.getElementById("loginUsername").value.trim();
  const password = document.getElementById("loginPassword").value;
  const res = await fetch("api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password })
  });
  if (res.ok) {
    authCheck();
  } else {
    const data = await res.json().catch(() => ({}));
    msgEl.textContent = data.error || "Login failed";
  }
};

document.getElementById("registerBtn").onclick = async () => {
  const msgEl = document.getElementById("registerMsg");
  msgEl.textContent = "";
  const username = document.getElementById("regUsername").value.trim();
  const password = document.getElementById("regPassword").value;
  const res = await fetch("api/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password })
  });
  if (res.ok) {
    authCheck();
  } else {
    const data = await res.json().catch(() => ({}));
    msgEl.textContent = data.error || "Registration failed";
  }
};

document.getElementById("showRegister").onclick = (e) => {
  e.preventDefault();
  document.getElementById("loginPanel").style.display = "none";
  document.getElementById("registerPanel").style.display = "flex";
};

document.getElementById("showLogin").onclick = (e) => {
  e.preventDefault();
  document.getElementById("registerPanel").style.display = "none";
  document.getElementById("loginPanel").style.display = "flex";
};

document.getElementById("logoutBtn").onclick = async () => {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  await fetch("api/logout", { method: "POST" });
  currentUsername = null;
  isAdmin = false;
  isFrozen = false;
  itemsEl.innerHTML = "";
  currentItems = [];
  allItems = [];
  authCheck();
};

claimBtn.onchange = () => toggleClaimMode(claimBtn.checked);
completionToggle.onchange = () => toggleCompletionMode(completionToggle.checked);
if (completionBar) {
  completionBar.onclick = () => {
    completionToggle.checked = !completionToggle.checked;
    toggleCompletionMode(completionToggle.checked);
  };
}
sortBtn.onclick = () => performSort();

// Initialize completion toggle label
toggleCompletionMode(completionToggle.checked);

// Fuzzy search implementation (fzf-like)
function fuzzyMatch(pattern, str) {
  if (!pattern) return { matched: true, score: 0, indices: [] };

  pattern = pattern.toLowerCase();
  str = str.toLowerCase();

  let patternIdx = 0;
  let strIdx = 0;
  const indices = [];
  let score = 0;
  let consecutiveMatches = 0;

  while (patternIdx < pattern.length && strIdx < str.length) {
    if (pattern[patternIdx] === str[strIdx]) {
      indices.push(strIdx);

      // Bonus for consecutive matches
      if (indices.length > 1 && indices[indices.length - 1] === indices[indices.length - 2] + 1) {
        consecutiveMatches++;
        score += 5 + consecutiveMatches; // Increasing bonus for longer sequences
      } else {
        consecutiveMatches = 0;
        score += 1;
      }

      // Bonus for matching at word start
      if (strIdx === 0 || str[strIdx - 1] === ' ' || str[strIdx - 1] === '-' || str[strIdx - 1] === '_') {
        score += 8;
      }

      patternIdx++;
    }
    strIdx++;
  }

  const matched = patternIdx === pattern.length;
  if (matched) {
    // Penalty for gaps
    const gaps = indices.length > 0 ? indices[indices.length - 1] - indices[0] - indices.length + 1 : 0;
    score -= gaps * 0.5;

    // Bonus for shorter strings (preferring exact/closer matches)
    score += (1 / (str.length + 1)) * 10;
  }

  return { matched, score: matched ? score : -Infinity, indices };
}

function filterAndSortBySearch(items, query) {
  if (!query.trim()) return items;

  const results = items.map(item => ({
    item,
    match: fuzzyMatch(query, item.name)
  }))
  .filter(({ match }) => match.matched)
  .sort((a, b) => b.match.score - a.match.score);

  return results.map(({ item, match }) => ({ ...item, _matchIndices: match.indices }));
}

searchInput.oninput = (e) => {
  searchQuery = e.target.value;
  clearSearchBtn.style.display = searchQuery ? "block" : "none";
  applySearchFilter();
};

searchInput.onkeydown = (e) => {
  if (e.key === "Escape") {
    searchInput.value = "";
    searchQuery = "";
    clearSearchBtn.style.display = "none";
    applySearchFilter();
  }
};

clearSearchBtn.onclick = () => {
  searchInput.value = "";
  searchQuery = "";
  clearSearchBtn.style.display = "none";
  applySearchFilter();
  searchInput.focus();
};

function applySearchFilter() {
  const filtered = filterAndSortBySearch(allItems, searchQuery);
  const ordered = searchQuery ? filtered : applySortOrder(filtered);
  renderDirect(ordered);
}

function toggleClaimMode(enabled) {
  claimMode = enabled;
  claimBtn.checked = claimMode;
  claimLabel.textContent = "Claim Mode";
  document.body.classList.toggle("claim-mode", claimMode);
  loadItems();
}

function toggleCompletionMode(enabled) {
  completionMode = enabled;
  completionToggle.checked = completionMode;
  completionLabel.textContent = completionMode ? "Item Based" : "Panel Based";
  if (completionBar) {
    completionBar.classList.remove('item-based', 'panel-based');
    completionBar.classList.add(completionMode ? 'item-based' : 'panel-based');
    // Trigger animation
    completionBar.classList.remove('animating');
    void completionBar.offsetWidth; // trigger reflow
    completionBar.classList.add('animating');
    setTimeout(() => completionBar.classList.remove('animating'), 500);
  }
  updateCompletionBar(currentItems.length ? currentItems : allItems);
}

async function loadItems() {
  const res = await fetch("api/items");
  if (!res.ok) {
    itemsEl.textContent = "Auth required";
    return;
  }
  render(await res.json());
}

const safeId = name => name.replace(/[^a-z0-9]/gi, "-");

// LocalStorage utilities for storing sort order
function saveSortOrder(items) {
  const order = items.map(item => item.name);
  try {
    localStorage.setItem('sortOrder', JSON.stringify(order));
  } catch (e) {
    console.error('Failed to save sort order to localStorage:', e);
  }
}

function loadSortOrder() {
  try {
    const stored = localStorage.getItem('sortOrder');
    return stored ? JSON.parse(stored) : null;
  } catch (e) {
    console.error('Failed to load sort order from localStorage:', e);
    return null;
  }
}

function applySortOrder(items) {
  const storedOrder = loadSortOrder();
  if (!storedOrder) return items;

  // Create a map for quick lookup
  const itemMap = new Map(items.map(item => [item.name, item]));
  const sorted = [];

  // Add items in stored order
  storedOrder.forEach(name => {
    if (itemMap.has(name)) {
      sorted.push(itemMap.get(name));
      itemMap.delete(name);
    }
  });

  // Add any new items that weren't in stored order
  itemMap.forEach(item => sorted.push(item));

  return sorted;
}

function sortItems(items) {
  const sortMode = sortModeEl.value;
  const finishedPriority = finishedPriorityEl.value;

  if (sortMode === "none") return items;

  const sorted = [...items];

  // First apply the main sort
  sorted.sort((a, b) => {
    const aCompleted = a.gathered >= a.target;
    const bCompleted = b.gathered >= b.target;

    // Apply finished priority if not neutral
    if (finishedPriority !== "neutral") {
      if (aCompleted !== bCompleted) {
        if (finishedPriority === "first") {
          return aCompleted ? -1 : 1;
        } else { // last
          return aCompleted ? 1 : -1;
        }
      }
    }

    // Then apply the selected sort mode
    switch (sortMode) {
      case "alphabetical":
        return a.name.localeCompare(b.name);
      case "progress":
        const aProgress = a.target > 0 ? a.gathered / a.target : 0;
        const bProgress = b.target > 0 ? b.gathered / b.target : 0;
        return bProgress - aProgress; // Descending
      case "target":
        return b.target - a.target; // Descending
      default:
        return 0;
    }
  });

  return sorted;
}

function performSort() {
  if (!currentItems.length) return;

  const sorted = sortItems(currentItems);
  renderWithAnimation(sorted);
}

function renderWithAnimation(newList) {
  if (dragActive) {
    pendingRender = newList;
    return;
  }

  // Save the sort order to cookies
  saveSortOrder(newList);

  // Get current positions of all cards
  const cards = Array.from(itemsEl.children);
  const oldPositions = new Map();

  cards.forEach((card, index) => {
    const rect = card.getBoundingClientRect();
    const itemName = currentItems[index]?.name;
    if (itemName) {
      oldPositions.set(itemName, {
        top: rect.top,
        left: rect.left,
        element: card
      });
    }
  });

  // Render new order (skip applying stored order since we're providing it)
  renderDirect(newList);

  // Get new positions and check which are visible
  const newCards = Array.from(itemsEl.children);
  const viewportHeight = window.innerHeight;
  const animations = [];

  newCards.forEach((card, index) => {
    const itemName = newList[index]?.name;
    const oldPos = oldPositions.get(itemName);

    if (oldPos) {
      const newRect = card.getBoundingClientRect();
      const deltaY = oldPos.top - newRect.top;
      const deltaX = oldPos.left - newRect.left;

      // Only animate if card is visible or becomes visible
      const isVisible = (newRect.top < viewportHeight && newRect.bottom > 0) ||
                       (oldPos.top < viewportHeight && oldPos.top > 0);

      if (isVisible && (Math.abs(deltaY) > 1 || Math.abs(deltaX) > 1)) {
        animations.push({ card, deltaX, deltaY });
      }
    }
  });

  // Perform animations
  if (animations.length > 0) {
    animations.forEach(({ card, deltaX, deltaY }) => {
      card.style.transform = `translate(${deltaX}px, ${deltaY}px)`;
      card.style.transition = "none";
    });

    // Force reflow
    itemsEl.offsetHeight;

    // Animate to final positions with staggered delay for better tracking
    animations.forEach(({ card }, index) => {
      const delay = index * 0.02; // 20ms stagger between each card
      card.style.transition = `transform 1.8s cubic-bezier(0.25, 0.46, 0.45, 0.94) ${delay}s`;
      card.style.transform = "translate(0, 0)";
    });

    // Clean up after animation (duration + max stagger delay)
    const maxDelay = animations.length * 0.02;
    setTimeout(() => {
      animations.forEach(({ card }) => {
        card.style.transition = "";
        card.style.transform = "";
      });
    }, 1800 + maxDelay * 1000);
  }
}

function setSliderVars(slider, max) {
  slider.style.setProperty("--min", 0);
  slider.style.setProperty("--max", max);
  slider.style.setProperty("--value", slider.value || 0);
}

function highlightMatches(text, indices) {
  if (!indices || indices.length === 0) {
    return text;
  }

  const indicesSet = new Set(indices);
  return text.split('').map((char, i) => {
    if (indicesSet.has(i)) {
      return `<mark class="fuzzy-match">${char}</mark>`;
    }
    return char;
  }).join('');
}

function renderDirect(list) {
  // Render without applying stored order (used when we already have sorted list)
  if (dragActive) {
    pendingRender = list;
    return;
  }
  pendingRender = null;
  currentItems = list;

  itemsEl.innerHTML = "";
  list.forEach(item => {
    const card = document.createElement("div");
    const isCompleted = item.gathered >= item.target;
    card.className = isCompleted ? "card completed" : "card";
    const id = "g-" + safeId(item.name);
    const max = item.target;
    const displayName = item._matchIndices ? highlightMatches(item.name, item._matchIndices) : item.name;
    card.innerHTML = `<div class="row"><div class="name">${displayName}</div><div class="count"><span id="${id}">${item.gathered}</span> / ${item.target}</div></div><div style="position:relative;padding-bottom:12px"><div class="claims"></div><input type="range" min="0" max="${max}" value="${item.gathered}" step="1"></div>`;
    paintClaims(card.querySelector(".claims"), item);
    const slider = card.querySelector("input");
    setSliderVars(slider, max);
    slider.oninput = e => {
      setSliderVars(slider, max);
      if (dragActive) {
        if (!claimMode) {
          const count = document.getElementById(id);
          if (count) count.textContent = +e.target.value;
        }
        return;
      }
      update(item, id, +e.target.value);
    };
    enableDrag(slider, max, () => {
      update(item, id, +slider.value);
    });
    itemsEl.appendChild(card);
  });
  updateCompletionBar(list);
}

function render(list) {
  // Store all items for searching
  allItems = list;
  // If there's a search query, apply it; otherwise use stored sort order
  if (searchQuery) {
    applySearchFilter();
  } else {
    const orderedList = applySortOrder(list);
    renderDirect(orderedList);
  }
}

function paintClaims(el, item) {
  el.innerHTML = "";
  (item.claims || []).forEach(c => {
    const w = item.target || 1;
    const start = (100 * c.claim_start) / w;
    const end = (100 * c.claim_end) / w;
    const bar = document.createElement("div");
    bar.style = `left:${start}%;width:${Math.max(end - start, 1)}%`;
    const label = document.createElement("span");
    label.textContent = c.claimer;
    label.style.left = `${start}%`;
    // Only show the clear button for the current user's own claim.
    if (c.claimer === currentUsername) {
      const clear = () => clearClaim(item.name);
      bar.onclick = clear;
      label.onclick = clear;
      bar.title = "Click to remove your claim";
      label.style.cursor = "pointer";
    }
    el.append(label, bar);
  });
}

async function update(item, id, val) {
  if (isFrozen) return;
  if (claimMode) {
    const remaining = Math.max(item.target - item.gathered, 0);
    const claimed = Math.min(Math.max(val - item.gathered, 0), remaining);
    await fetch("api/items/claim", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: item.name, claimed })
    });
    return;
  }
  if (lastUpdate[item.name] === val) return;
  lastUpdate[item.name] = val;
  const count = document.getElementById(id);
  if (count) count.textContent = val;
  const delta = val - item.gathered;
  await fetch("api/items/update", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: item.name, gathered: val, delta })
  });
}

function enableDrag(slider, max, onEndDrag = () => {}) {
  let dragging = false;
  const removeListeners = () => {
    window.removeEventListener("pointerup", endDrag);
    window.removeEventListener("pointercancel", endDrag);
  };
  const endDrag = e => {
    if (!dragging) return;
    dragging = false;
    dragActive = false;
    removeListeners();
    if (e) {
      try {
        slider.releasePointerCapture(e.pointerId);
      } catch {}
    }
    if (pendingRender) {
      const next = pendingRender;
      pendingRender = null;
      render(next);
    }
    onEndDrag();
  };
  const setVal = x => {
    const r = slider.getBoundingClientRect();
    const ratio = Math.min(Math.max((x - r.left) / r.width, 0), 1);
    slider.value = Math.round(ratio * max);
    slider.dispatchEvent(new Event("input"));
  };
  slider.onpointerdown = e => {
    dragging = true;
    dragActive = true;
    window.addEventListener("pointerup", endDrag);
    window.addEventListener("pointercancel", endDrag);
    try {
      slider.setPointerCapture(e.pointerId);
    } catch {}
    setVal(e.clientX);
  };
  slider.onpointermove = e => {
    if (dragging) setVal(e.clientX);
  };
  slider.onpointerup = endDrag;
  slider.onpointercancel = endDrag;
}

function clearClaim(itemName) {
  if (isFrozen) return;
  fetch("api/items/claim", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: itemName, claimed: 0 })
  });
}

function startStream() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  const connect = () => {
    eventSource = new EventSource("events");
    eventSource.onmessage = e => {
      try {
        const data = JSON.parse(e.data);
        if (data.type === "update") render(data.items);
      } catch {}
    };
    eventSource.onerror = () => {
      eventSource.close();
      eventSource = null;
    };
  };
  // Defer until the page is fully loaded to avoid the browser warning
  // "connection interrupted while page was loading".
  if (document.readyState === "complete") {
    connect();
  } else {
    window.addEventListener("load", connect, { once: true });
  }
}

async function loadAdminUsers() {
  const res = await fetch("api/admin/users");
  if (!res.ok) return;
  const users = await res.json();
  renderAdminUsers(users);
}

async function loadAdminSettings() {
  const res = await fetch("api/admin/settings");
  if (!res.ok) return;
  const settings = await res.json();
  registrationLockedDown = Boolean(settings.registration_locked_down);
  renderRegistrationLockState();
}

function renderAdminUsers(users) {
  const list = document.getElementById("userList");
  list.innerHTML = "";
  if (users.length === 0) {
    list.innerHTML = '<p style="color:#64748b;margin:0">No users yet.</p>';
    return;
  }
  users.forEach(u => {
    const row = document.createElement("div");
    row.className = "user-row";
    row.innerHTML = `
      <span class="user-name">${u.username}${u.admin ? ' <span class="admin-badge">admin</span>' : ''}${u.frozen ? ' <span class="frozen-badge">frozen</span>' : ''}</span>
      <div class="user-actions">
        <button class="btn-sm ${u.frozen ? '' : 'btn-warning'}" data-action="toggle_frozen" data-user="${u.username}">
          ${u.frozen ? "Unfreeze" : "Freeze"}
        </button>
        <button class="btn-sm btn-warning" data-action="purge_progress" data-user="${u.username}">
          Remove Edits
        </button>
        <button class="btn-sm" data-action="toggle_admin" data-user="${u.username}">
          ${u.admin ? "Remove Admin" : "Make Admin"}
        </button>
        <button class="btn-sm btn-danger" data-action="delete" data-user="${u.username}"
          ${u.username === currentUsername ? "disabled title='Cannot delete your own account'" : ""}>
          Delete
        </button>
      </div>
    `;
    list.appendChild(row);
  });

  list.querySelectorAll("button[data-action]").forEach(btn => {
    if (btn.disabled) return;
    btn.onclick = async () => {
      const action = btn.dataset.action;
      const username = btn.dataset.user;
      const actionLabel = action === "purge_progress" ? "remove edits for" : action === "delete" ? "delete" : action === "toggle_frozen" ? "toggle freeze for" : "update";
      if (action === "purge_progress" && !confirm(`Remove all collected progress and claims made by "${username}"? This cannot be undone.`)) return;
      if (action === "delete" && !confirm(`Delete user "${username}" and remove all of their collected progress and claims? This cannot be undone.`)) return;
      if (action === "toggle_frozen" && !confirm(`Toggle whether "${username}" can make contributions?`)) return;
      setAdminStatus(`Working on ${actionLabel} ${username}...`, "info");
      const res = await fetch("api/admin/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, action })
      });
      if (res.ok) {
        if (action === "purge_progress") {
          setAdminStatus(`Removed all edits and claims made by ${username}.`, "success");
        } else if (action === "delete") {
          setAdminStatus(`Deleted ${username} and removed their edits.`, "success");
        } else if (action === "toggle_frozen") {
          setAdminStatus(`Updated contribution access for ${username}.`, "success");
        } else {
          setAdminStatus(`Updated ${username}.`, "success");
        }
        loadAdminUsers();
      } else {
        const data = await res.json().catch(() => ({}));
        setAdminStatus(data.error || `Failed to ${actionLabel} ${username}.`, "error");
      }
    };
  });
}

if (registrationLockBtn) {
  registrationLockBtn.onclick = async () => {
    const nextValue = !registrationLockedDown;
    setAdminStatus(`${nextValue ? "Enabling" : "Disabling"} registration lockdown...`, "info");
    const res = await fetch("api/admin/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ registration_locked_down: nextValue })
    });
    if (res.ok) {
      registrationLockedDown = nextValue;
      renderRegistrationLockState();
      setAdminStatus(
        nextValue
          ? "Registration lockdown enabled. New accounts will be created frozen."
          : "Registration lockdown disabled. New accounts can contribute immediately.",
        "success"
      );
    } else {
      const data = await res.json().catch(() => ({}));
      setAdminStatus(data.error || "Failed to update registration lockdown.", "error");
    }
  };
}

authCheck();

// Add shadow to sticky header when scrolling
const headerRow = document.querySelector("#app > .row:first-child");
if (headerRow) {
  window.addEventListener("scroll", () => {
    if (window.scrollY > 10) {
      headerRow.classList.add("scrolled");
    } else {
      headerRow.classList.remove("scrolled");
    }
  });
}
