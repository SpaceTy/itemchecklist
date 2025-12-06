const itemsEl = document.getElementById("items");
const loginRow = document.getElementById("login");
const msg = document.getElementById("loginMsg");
const claimBtn = document.getElementById("claimToggle");
const claimLabel = document.getElementById("claimLabel");
const sortBtn = document.getElementById("sortBtn");
const sortModeEl = document.getElementById("sortMode");
const finishedPriorityEl = document.getElementById("finishedPriority");
const searchInput = document.getElementById("searchInput");
const clearSearchBtn = document.getElementById("clearSearch");
let claimMode = false;
let lastUpdate = {};
let dragActive = false;
let pendingRender = null;
let currentItems = [];
let searchQuery = "";
let allItems = [];

function computeTotalCompletion(items) {
  let totalGathered = 0;
  let totalTarget = 0;
  items.forEach(item => {
    totalGathered += item.gathered;
    totalTarget += item.target;
  });
  return { totalGathered, totalTarget };
}

function updateCompletionBar(items) {
  const bar = document.getElementById('completion-bar');
  if (!bar) return;
  const { totalGathered, totalTarget } = computeTotalCompletion(items);
  const percent = totalTarget > 0 ? Math.round((totalGathered / totalTarget) * 100) : 0;
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
  const ok = await fetch("api/check-auth").then(r => r.ok);
  loginRow.style.display = ok ? "none" : "flex";
  if (ok) {
    loadItems();
    startStream();
  }
}

document.getElementById("loginBtn").onclick = async () => {
  msg.textContent = "";
  const pwd = document.getElementById("pwd").value.trim();
  const res = await fetch("api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password: pwd })
  });
  msg.textContent = res.ok ? "" : "Invalid password";
  if (res.ok) authCheck();
};

claimBtn.onchange = () => toggleClaimMode(claimBtn.checked);
sortBtn.onclick = () => performSort();

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
    const clear = () => clearClaim(item.name, c.claimer);
    bar.onclick = clear;
    label.onclick = clear;
    el.append(label, bar);
  });
}

async function update(item, id, val) {
  if (claimMode) {
    const claimer = document.getElementById("claimer").value.trim() || "anon";
    const remaining = Math.max(item.target - item.gathered, 0);
    const claimed = Math.min(Math.max(val - item.gathered, 0), remaining);
    await fetch("api/items/claim", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: item.name, claimed, claimer })
    });
    return;
  }
  if (lastUpdate[item.name] === val) return;
  lastUpdate[item.name] = val;
  const count = document.getElementById(id);
  if (count) count.textContent = val;
  await fetch("api/items/update", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: item.name, gathered: val })
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

function clearClaim(itemName, claimer) {
  fetch("api/items/claim", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: itemName, claimed: 0, claimer })
  });
}

function startStream() {
  const es = new EventSource("events");
  es.onmessage = e => {
    try {
      const data = JSON.parse(e.data);
      if (data.type === "update") render(data.items);
    } catch {}
  };
  es.onerror = () => es.close();
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
