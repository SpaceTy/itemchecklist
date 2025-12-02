const itemsEl = document.getElementById("items");
const loginRow = document.getElementById("login");
const msg = document.getElementById("loginMsg");
const claimBtn = document.getElementById("claimToggle");
const claimLabel = document.getElementById("claimLabel");
let claimMode = false;
let lastUpdate = {};
let dragActive = false;
let pendingRender = null;

async function authCheck() {
  const ok = await fetch("/api/check-auth").then(r => r.ok);
  loginRow.style.display = ok ? "none" : "flex";
  if (ok) {
    loadItems();
    startStream();
  }
}

document.getElementById("loginBtn").onclick = async () => {
  msg.textContent = "";
  const pwd = document.getElementById("pwd").value.trim();
  const res = await fetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password: pwd })
  });
  msg.textContent = res.ok ? "" : "Invalid password";
  if (res.ok) authCheck();
};

claimBtn.onchange = () => toggleClaimMode(claimBtn.checked);

function toggleClaimMode(enabled) {
  claimMode = enabled;
  claimBtn.checked = claimMode;
  claimLabel.textContent = "Claim Mode";
  document.body.classList.toggle("claim-mode", claimMode);
  loadItems();
}

async function loadItems() {
  const res = await fetch("/api/items");
  if (!res.ok) {
    itemsEl.textContent = "Auth required";
    return;
  }
  render(await res.json());
}

const safeId = name => name.replace(/[^a-z0-9]/gi, "-");

function setSliderVars(slider, max) {
  slider.style.setProperty("--min", 0);
  slider.style.setProperty("--max", max);
  slider.style.setProperty("--value", slider.value || 0);
}

function render(list) {
  if (dragActive) {
    pendingRender = list;
    return;
  }
  pendingRender = null;
  itemsEl.innerHTML = "";
  list.forEach(item => {
    const card = document.createElement("div");
    card.className = "card";
    const id = "g-" + safeId(item.name);
    const max = item.target;
    card.innerHTML = `<div class="row"><div class="name">${item.name}</div><div class="count"><span id="${id}">${item.gathered}</span> / ${item.target}</div></div><div style="position:relative;padding-top:12px"><div class="claims"></div><input type="range" min="0" max="${max}" value="${item.gathered}" step="1"></div>`;
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
    await fetch("/api/items/claim", {
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
  await fetch("/api/items/update", {
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
  fetch("/api/items/claim", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: itemName, claimed: 0, claimer })
  });
}

function startStream() {
  const es = new EventSource("/events");
  es.onmessage = e => {
    try {
      const data = JSON.parse(e.data);
      if (data.type === "update") render(data.items);
    } catch {}
  };
  es.onerror = () => es.close();
}

authCheck();
