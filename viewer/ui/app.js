// Plain-JS single-page app for rhs view. Every number comes from /api/...;
// the browser never computes aggregates or loads a whole table.
"use strict";

let config = null;
const app = document.getElementById("app");
const stateColors = { idle: "#9e9e9e", en_route: "#1e66d0", on_trip: "#2e9d4f", going_offline: "#f08c00", repositioning: "#8e44ad", offline: "#444" };

function escapeHTML(v) {
  return String(v ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
async function api(path) {
  const response = await fetch(path);
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || body.message || response.statusText);
  return body;
}
// ---------- Time ----------
// Simulated times are shown as a T+ offset plus the local wall-clock time in
// the zone the server resolved (config.timezone: -timezone flag, viewer.toml,
// run_metadata), or the browser's zone when that is empty.
let timeFormats = null;
function displayTimezone() { return config.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone; }
function timeFormat() {
  if (!timeFormats) {
    const timeZone = displayTimezone();
    timeFormats = {
      full: new Intl.DateTimeFormat("en-US", { timeZone, weekday: "short", month: "short", day: "numeric", year: "numeric",
        hour: "numeric", minute: "2-digit", second: "2-digit", timeZoneName: "short" }),
      compact: new Intl.DateTimeFormat("en-US", { timeZone, hour: "numeric", minute: "2-digit", second: "2-digit" }),
      parts: new Intl.DateTimeFormat("en-US", { timeZone, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit",
        hour: "2-digit", minute: "2-digit", second: "2-digit" }),
    };
  }
  return timeFormats;
}
// "Wed Jan 7 2026, 5:38:24 AM CST"
function localFull(epoch) {
  const p = Object.fromEntries(timeFormat().full.formatToParts(new Date(Number(epoch) * 1000)).map(x => [x.type, x.value]));
  return `${p.weekday} ${p.month} ${p.day} ${p.year}, ${p.hour}:${p.minute}:${p.second} ${p.dayPeriod} ${p.timeZoneName}`;
}
const localCompact = epoch => timeFormat().compact.format(new Date(Number(epoch) * 1000));
function tOffset(epoch) {
  const t = Number(epoch) - config.simulation_start_at;
  return `T${t >= 0 ? "+" : ""}${t}`;
}
// Plain text for titles and other non-HTML places.
const timeText = epoch => `${tOffset(epoch)} · ${localFull(epoch)}`;
// mode "full" (default): T+20304 · Wed Jan 7 2026, 5:38:24 AM CST.
// mode "compact" for dense tables: T+20304 · 5:38:24 AM, full date in the tooltip.
function offset(epoch, mode = "full") {
  if (epoch === "" || epoch === undefined || epoch === null) return "";
  const title = `${localFull(epoch)} (epoch ${epoch})`;
  const local = mode === "compact" ? localCompact(epoch) : localFull(epoch);
  return `<span class="sim-time" title="${escapeHTML(title)}">${tOffset(epoch)} <span class="local-time">· ${escapeHTML(local)}</span></span>`;
}
// Wall-clock fields of an epoch in the display zone.
function zonedParts(epoch) {
  const p = Object.fromEntries(timeFormat().parts.formatToParts(new Date(epoch * 1000)).map(x => [x.type, Number(x.value)]));
  return { year: p.year, month: p.month, day: p.day, hour: p.hour % 24, minute: p.minute, second: p.second };
}
// Epoch for a wall-clock time in the display zone (two passes settle DST offsets).
function zonedEpoch(year, month, day, hour, minute, second) {
  const wanted = Date.UTC(year, month - 1, day, hour, minute, second) / 1000;
  let guess = wanted;
  for (let i = 0; i < 2; i++) {
    const p = zonedParts(guess);
    guess += wanted - Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) / 1000;
  }
  return guess;
}
// Parses jump-box text: "T+600", "600" (offset), an epoch, "17:30[:15]" (local
// time on the playhead's date) or "2026-01-07 17:30[:15]". Returns null if invalid.
function parseJump(text, start, now) {
  const m = text.match(/^T?\+?(-?\d+)$/i);
  if (m) { const v = Number(m[1]); return /^T/i.test(text) || v < start ? start + v : v; }
  const local = text.match(/^(?:(\d{4})-(\d{1,2})-(\d{1,2})[ T])?(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([ap]\.?m\.?)?$/i);
  if (!local) return null;
  let hour = Number(local[4]);
  if (local[7]) { if (hour < 1 || hour > 12) return null; hour = hour % 12 + (/^p/i.test(local[7]) ? 12 : 0); }
  if (hour > 23 || Number(local[5]) > 59 || Number(local[6] || 0) > 59) return null;
  const day = local[1] ? { year: Number(local[1]), month: Number(local[2]), day: Number(local[3]) } : zonedParts(Math.round(now));
  return zonedEpoch(day.year, day.month, day.day, hour, Number(local[5]), Number(local[6] || 0));
}
function fmt(v) {
  if (v === null || v === undefined || v === "") return "n/a";
  if (typeof v === "number") return Number.isInteger(v) ? String(v) : v.toFixed(4);
  return escapeHTML(v);
}
const tripLink = id => id ? `<a href="/trips/${encodeURIComponent(id)}">${escapeHTML(id)}</a>` : "";
const supplyLink = id => id ? `<a href="/supply/${encodeURIComponent(id)}">${escapeHTML(id)}</a>` : "";
const isTimeField = f => f.endsWith("_at");

// Human-readable labels for metric and field keys. The precise definition
// stays linked (metrics) or in the tooltip (the raw key).
const labelOverrides = {
  on_trip_time_share: "On Trip", en_route_time_share: "En Route", idle_time_share: "Idle",
  on_trip_distance_share: "On Trip", en_route_distance_share: "En Route", idle_distance_share: "Idle",
  deadhead_distance_share: "Deadhead Distance Share", shared_trip_rate: "Shared Trip Rate (Match Rate)",
  rider_meters_per_supply_meter: "Rider Meters per Supply Meter", rider_meters_driven_per_supply_meter: "Rider Meters Driven per Supply Meter",
  requests_served_per_supply_hour: "Requests Served per Supply Hour", request_id: "Request", supply_id: "Car",
  max_co_riders: "Max Co-riders", max_riders_aboard: "Max Riders Aboard", ridepool_type: "Ridepool Type",
  supply_utilization: "Supply Utilization", repositioning: "Repositioning", rider_wait: "Rider Wait", rider_inconvenience: "Rider Inconvenience",
};
const wordOverrides = { eta: "ETA", p90: "P90", id: "ID", sha256: "SHA-256", a: "A", b: "B" };
function label(key) {
  if (labelOverrides[key]) return labelOverrides[key];
  let words = String(key).split("_");
  const stat = ["mean", "median", "p90", "max"].includes(words[words.length - 1]) ? words.pop() : "";
  const unit = { seconds: "(s)", meters: "(m)" }[words[words.length - 1]] || "";
  if (unit) words.pop();
  if (stat) words.push(stat);
  if (unit) words.push(unit);
  return words.map(w => wordOverrides[w] || (w.charAt(0).toUpperCase() + w.slice(1))).join(" ").replace(" At ", " at ");
}
const labelHTML = key => `<span title="${escapeHTML(key)}">${escapeHTML(label(key))}</span>`;

// Pie chart drawn with a CSS conic-gradient; percent in the legend and on hover.
function pieHTML(title, slices) {
  const total = slices.reduce((n, s) => n + (s.value || 0), 0);
  if (!total) return `<section><strong>${escapeHTML(title)}</strong><p>n/a</p></section>`;
  let start = 0;
  const stops = slices.map(s => {
    const end = start + (s.value || 0) / total * 100;
    const stop = `${s.color} ${start}% ${end}%`;
    start = end;
    return stop;
  });
  return `<section class="pie-block"><strong>${escapeHTML(title)}</strong>
    <div class="pie" style="background: conic-gradient(${stops.join(", ")})" role="img" aria-label="${escapeHTML(title)}"></div>
    <ul class="legend">${slices.map(s => `<li title="${escapeHTML(s.key)}"><span class="swatch" style="background:${s.color}"></span>
      ${escapeHTML(label(s.key))}: <strong>${((s.value || 0) / total * 100).toFixed(1)}%</strong></li>`).join("")}</ul></section>`;
}

// Brief trip info for a map marker's InfoWindow.
function infoHTML(trip, stopType, at) {
  if (!trip) return "";
  const row = (k, v) => `<tr><td>${k}</td><td>${v}</td></tr>`;
  return `<div class="info"><strong>${escapeHTML(trip.request_id)}</strong><table>
    ${stopType ? row(escapeHTML(stopType), offset(at)) : ""}
    ${row("Status", escapeHTML(trip.status))}
    ${row("Wait", trip.wait_seconds !== "" ? `${escapeHTML(trip.wait_seconds)} s` : "n/a")}
    ${row("Direct", `${fmt(trip.direct_distance_meters)} m · ${fmt(trip.direct_duration_seconds)} s`)}
    ${row("Actual", trip.status === "completed" ? `${fmt(trip.trip_distance_meters)} m · ${fmt(trip.trip_duration_seconds)} s` : "n/a")}
    </table><a href="/trips/${encodeURIComponent(trip.request_id)}">Open trip</a></div>`;
}

function histogramHTML(buckets, otherBuckets) {
  const all = [...(buckets || []), ...(otherBuckets || [])];
  const most = Math.max(1, ...all.map(b => b.count));
  const bars = (list, cls) => (list || []).map(b =>
    `<div class="${cls}" style="height:${(b.count / most) * 100}%" title="${b.lower}–${b.upper}: ${b.count}"></div>`).join("");
  return `<div class="histogram">${bars(buckets, "")}${otherBuckets ? bars(otherBuckets, "b") : ""}</div>`;
}

function metricTable(metrics) {
  return `<table><tr><th>Metric</th><th>Value</th></tr>${metrics.map(m => {
    const alert = m.name === "plan_rejected_count" && m.value > 0 ? " alert" : "";
    return `<tr><td><a href="${m.definition_url}" target="_blank" title="${escapeHTML(m.name)}: see definition">${escapeHTML(label(m.name))}</a></td><td class="number${alert}">${fmt(m.value)}</td></tr>`;
  }).join("")}</table>`;
}

// ---------- Icons ----------
// Original inline SVGs drawn for this project (tooling spec "Map icons").
const icons = {
  origin: () => `<svg xmlns="http://www.w3.org/2000/svg" width="28" height="36" viewBox="0 0 28 36">
    <ellipse cx="14" cy="34" rx="7" ry="1.6" fill="#000" opacity=".25"/>
    <circle cx="12" cy="7" r="4" fill="#1e66d0" stroke="#fff" stroke-width="1.2"/>
    <path d="M7 13h10l-1 10h-1.5l-.8 10h-2.6l-.6-8-.6 8H7.3l-.8-10H5z" fill="#1e66d0" stroke="#fff" stroke-width="1.2" stroke-linejoin="round"/>
    <path d="M16.5 14.5 22 6.5" stroke="#1e66d0" stroke-width="3" stroke-linecap="round"/>
    <circle cx="22.6" cy="5.4" r="2" fill="#f2c9a0" stroke="#1e66d0" stroke-width="1"/>
    <path d="M7.5 14.5 4.5 21" stroke="#1e66d0" stroke-width="3" stroke-linecap="round"/></svg>`,
  destination: () => `<svg xmlns="http://www.w3.org/2000/svg" width="28" height="36" viewBox="0 0 28 36">
    <ellipse cx="6" cy="34" rx="5" ry="1.4" fill="#000" opacity=".25"/>
    <rect x="4.5" y="2" width="2.6" height="32" rx="1" fill="#333"/>
    <g transform="translate(7 3)"><rect width="18" height="13.5" fill="#fff" stroke="#222" stroke-width="1"/>
    ${[0, 1, 2].map(row => [0, 1, 2, 3].map(col => (row + col) % 2 ? "" : `<rect x="${col * 4.5}" y="${row * 4.5}" width="4.5" height="4.5" fill="#222"/>`).join("")).join("")}</g></svg>`,
  car: (color = "#1e66d0") => `<svg xmlns="http://www.w3.org/2000/svg" width="22" height="34" viewBox="0 0 22 34">
    <rect x="2" y="2" width="18" height="30" rx="6" fill="${color}" stroke="#fff" stroke-width="1.5"/>
    <path d="M5 10q6-4 12 0l-1 5H6z" fill="#cfe3ff" stroke="#123" stroke-width=".6"/>
    <path d="M6 24h10l1 4q-6 2-12 0z" fill="#cfe3ff" stroke="#123" stroke-width=".6"/>
    <rect x="4" y="2.8" width="4" height="2" rx="1" fill="#fff6b0"/><rect x="14" y="2.8" width="4" height="2" rx="1" fill="#fff6b0"/>
    <rect x="4.5" y="29.4" width="3.5" height="1.6" rx=".8" fill="#ff5a5a"/><rect x="14" y="29.4" width="3.5" height="1.6" rx=".8" fill="#ff5a5a"/></svg>`,
};
const iconAnchors = { origin: [14, 34], destination: [6, 34], car: [11, 17] };
const svgURL = svg => "data:image/svg+xml;charset=UTF-8," + encodeURIComponent(svg);
function carColor(state, riders) { return state === "on_trip" && riders >= 2 ? "#17602f" : stateColors[state] || "#444"; }
// heading in degrees clockwise from north, in lat/lng space corrected for latitude.
// The point at fraction f of a path's length, and the piece it lies on.
function alongPath(points, f) {
  const len = (a, b) => Math.hypot((b.lng - a.lng) * Math.cos(a.lat * Math.PI / 180), b.lat - a.lat);
  const lengths = points.slice(1).map((q, i) => len(points[i], q));
  let target = f * lengths.reduce((n, x) => n + x, 0);
  for (let i = 0; i < lengths.length; i++) {
    const a = points[i], b = points[i + 1];
    if (lengths[i] > 0 && (target <= lengths[i] || i === lengths.length - 1)) {
      const g = Math.min(1, target / lengths[i]);
      return [{ lat: a.lat + (b.lat - a.lat) * g, lng: a.lng + (b.lng - a.lng) * g }, a, b];
    }
    target -= lengths[i];
  }
  const a = points[0], b = points[points.length - 1];
  return [{ lat: a.lat + (b.lat - a.lat) * f, lng: a.lng + (b.lng - a.lng) * f }, a, b];
}
function heading(from, to) {
  const dx = (to.lng - from.lng) * Math.cos(from.lat * Math.PI / 180), dy = to.lat - from.lat;
  if (Math.abs(dx) < 1e-12 && Math.abs(dy) < 1e-12) return null;
  return Math.atan2(dx, dy) * 180 / Math.PI;
}
function iconElement(kind, { color, rotation = 0 } = {}) {
  const div = document.createElement("div");
  div.className = `rhs-icon rhs-icon-${kind}`;
  div.innerHTML = kind === "car" ? icons.car(color) : icons[kind]();
  if (kind === "car") div.firstElementChild.style.transform = `rotate(${rotation}deg)`;
  return div;
}
// classic markers can't rotate an image, so the car SVG is rotated inside itself.
function rotatedCarSVG(color, rotation) {
  return icons.car(color).replace('viewBox="0 0 22 34">', `width="34" height="34" viewBox="-6 0 34 34"><g transform="rotate(${rotation} 11 17)">`).replace(/<\/svg>$/, "</g></svg>")
    .replace('width="22" height="34" ', "");
}

// ---------- Maps ----------
let mapsReady = null;
function loadMaps() {
  if (!config.maps_enabled) return Promise.resolve(false);
  if (!mapsReady) {
    mapsReady = new Promise(resolve => {
      window.__rhsMapsLoaded = () => resolve(true);
      const script = document.createElement("script");
      const libraries = config.google_maps_map_id ? "&libraries=marker" : "";
      script.src = `https://maps.googleapis.com/maps/api/js?key=${encodeURIComponent(config.google_maps_api_key)}&v=weekly${libraries}&callback=__rhsMapsLoaded`;
      script.async = true;
      script.onerror = () => resolve(false);
      document.head.appendChild(script);
    });
  }
  return mapsReady;
}
async function makeMap(element) {
  if (!(await loadMaps())) {
    element.classList.add("map-notice");
    element.textContent = "Maps are disabled: set GOOGLE_MAPS_API_KEY or [viewer] google_maps_api_key in viewer.toml.";
    return null;
  }
  // Hide Google's own POI and transit pins: they look like our icons (the purple
  // "Painted Ladies" attraction pin sits right on a pickup in the SF fixtures).
  const options = { zoom: 13, center: { lat: 37.78, lng: -122.42 }, clickableIcons: false };
  if (!config.google_maps_map_id) options.styles = [
    { featureType: "poi", elementType: "labels.icon", stylers: [{ visibility: "off" }] },
    { featureType: "transit", elementType: "labels.icon", stylers: [{ visibility: "off" }] }];
  if (config.google_maps_map_id) options.mapId = config.google_maps_map_id;
  const map = new google.maps.Map(element, options);
  map.__bounds = new google.maps.LatLngBounds();
  return map;
}
function drawLine(map, points, color, { dashed = false, weight = 4, opacity = 0.9, arrows = true } = {}) {
  if (!map || !points || points.length < 2) return null;
  const path = points.map(p => ({ lat: p.lat, lng: p.lng }));
  path.forEach(p => map.__bounds.extend(p));
  // Dashed lines hide the stroke and draw dashes as icons; arrows are a second
  // icon layer with their own opacity, so they show on solid and dashed lines.
  const arrow = { icon: { path: google.maps.SymbolPath.FORWARD_CLOSED_ARROW, scale: Math.max(2, weight * 0.6), strokeColor: color,
    strokeOpacity: 1, fillColor: color, fillOpacity: 1 }, offset: "40px", repeat: "120px" };
  const line = { path, strokeColor: color, strokeOpacity: dashed ? 0 : opacity, strokeWeight: weight, map, icons: arrows ? [arrow] : [] };
  if (dashed) line.icons.unshift({ icon: { path: "M 0,-1 0,1", strokeOpacity: 1, scale: 3, strokeColor: color }, offset: "0", repeat: "12px" });
  return new google.maps.Polyline(line);
}
let infoWindow = null;
// addIcon places one of the project icons; opts: { color, rotation, info, title }.
function addIcon(map, point, kind, { color, rotation = 0, info, title = "", extend = true } = {}) {
  if (!map || !point) return null;
  const position = { lat: point.lat, lng: point.lng };
  if (extend) map.__bounds.extend(position);
  let marker;
  if (config.google_maps_map_id && google.maps.marker) {
    const content = iconElement(kind, { color, rotation });
    if (kind === "car") content.style.transform = "translateY(50%)"; // anchor at center, not bottom
    marker = new google.maps.marker.AdvancedMarkerElement({ map, position, title, content, gmpClickable: !!info });
    marker.__kind = "advanced";
  } else {
    marker = new google.maps.Marker({ map, position, title, icon: iconFor(kind, color, rotation) });
  }
  marker.__info = info;
  bindInfo(map, marker, info);
  return marker;
}
function iconFor(kind, color, rotation) {
  if (kind === "car") return { url: svgURL(rotatedCarSVG(color, rotation)), scaledSize: new google.maps.Size(34, 34), anchor: new google.maps.Point(17, 17) };
  const [x, y] = iconAnchors[kind];
  return { url: svgURL(icons[kind]()), scaledSize: new google.maps.Size(28, 36), anchor: new google.maps.Point(x, y) };
}
function moveIcon(marker, point, color, rotation) {
  if (marker.__kind === "advanced") {
    marker.position = point;
    marker.content.innerHTML = icons.car(color);
    marker.content.firstElementChild.style.transform = `rotate(${rotation}deg)`;
  } else {
    marker.setPosition(point);
    marker.setIcon(iconFor("car", color, rotation));
  }
}
function removeIcon(marker) { if (marker.__kind === "advanced") marker.map = null; else marker.setMap(null); }
function bindInfo(map, marker, info) {
  if (!info) return;
  marker.addListener("click", () => {
    if (!infoWindow) infoWindow = new google.maps.InfoWindow();
    infoWindow.setContent(typeof marker.__info === "function" ? marker.__info() : marker.__info);
    infoWindow.open({ anchor: marker, map });
  });
}
function fit(map) { if (map && !map.__bounds.isEmpty()) map.fitBounds(map.__bounds); }

// ---------- Paged tables ----------
async function pagedTable(container, endpoint, columns, renderCell, params) {
  params = { page: 1, ...params };
  async function load() {
    const query = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== "" && v !== undefined));
    const data = await api(`${endpoint}?${query}`);
    const pages = Math.max(1, Math.ceil(data.total / data.page_size));
    container.innerHTML = `<div class="pager">${data.total} rows · page ${data.page}/${pages}
      <button ${data.page <= 1 ? "disabled" : ""} data-page="${data.page - 1}">Prev</button>
      <button ${data.page >= pages ? "disabled" : ""} data-page="${data.page + 1}">Next</button></div>
      <table><tr>${columns.map(c => `<th class="sortable" data-sort="${c}" title="${c}">${escapeHTML(label(c))}${params.sort === c ? (params.order === "desc" ? " ▼" : " ▲") : ""}</th>`).join("")}</tr>
      ${data.rows.map(row => `<tr>${columns.map(c => renderCell(row, c)).join("")}</tr>`).join("")}</table>`;
    container.querySelectorAll("button[data-page]").forEach(b => b.onclick = () => { params.page = Number(b.dataset.page); load(); });
    container.querySelectorAll("th[data-sort]").forEach(th => th.onclick = () => {
      params.order = params.sort === th.dataset.sort && params.order !== "desc" ? "desc" : "asc";
      params.sort = th.dataset.sort; params.page = 1; load();
    });
  }
  await load();
}
function rowCell(row, column) {
  const v = row[column];
  if (column === "request_id") return `<td>${tripLink(v)}</td>`;
  if (column === "supply_id") return `<td>${supplyLink(v)}</td>`;
  if (isTimeField(column)) return `<td class="number">${offset(v, "compact")}</td>`;
  return `<td class="${/^-?[\d.]+$/.test(v) ? "number" : ""}">${escapeHTML(v)}</td>`;
}

// ---------- Views ----------
async function dashboardView() {
  const d = await api("/api/dashboard");
  const m = Object.fromEntries(d.metrics.map(x => [x.name, x.value]));
  const groups = [...new Set(d.metrics.map(x => x.group))];
  const lastPoint = d.timeline[d.timeline.length - 1];
  app.innerHTML = `<h1>Dashboard</h1>
    <div class="pies">
      ${pieHTML("Supply time", [["on_trip_time_share", stateColors.on_trip], ["en_route_time_share", stateColors.en_route], ["idle_time_share", stateColors.idle]].map(([key, color]) => ({ key, color, value: m[key] })))}
      ${pieHTML("Supply distance", [["on_trip_distance_share", stateColors.on_trip], ["en_route_distance_share", stateColors.en_route], ["idle_distance_share", stateColors.idle]].map(([key, color]) => ({ key, color, value: m[key] })))}
    </div>
    <div class="grid">${groups.map(g => `<section><h2>${escapeHTML(label(g))}</h2>${metricTable(d.metrics.filter(x => x.group === g))}</section>`).join("")}</div>
    <h2>Distributions</h2><div class="grid">${Object.entries(d.distributions).map(([name, buckets]) =>
      `<section><strong>${labelHTML(name)}</strong>${histogramHTML(buckets)}</section>`).join("")}</div>
    <h2>Ridepool Type Breakdown</h2><table><tr><th>Ridepool Type</th><th>Count</th><th>Share</th></tr>${d.ridepool_types.map(r =>
      `<tr><td><a href="/trips?ridepool_type=${encodeURIComponent(r.ridepool_type)}">${escapeHTML(r.ridepool_type)}</a></td><td class="number">${r.count}</td><td class="number">${fmt(r.share)}</td></tr>`).join("")}</table>
    <h2>Requests and Supply over Time (60 s buckets)</h2><table><tr><th>Time</th><th>Open Requests</th><th>Cars by State</th></tr>${d.timeline.map(p =>
      `<tr><td>${offset(p.at, "compact")}</td><td class="number">${p.open_requests}</td><td>${Object.entries(p.cars_by_state).map(([s, n]) => `${label(s)}: ${n}`).join(", ")}</td></tr>`).join("")}</table>
    ${lastPoint ? "" : "<p>No timeline.</p>"}`;
}

async function tripsView(query) {
  app.innerHTML = `<h1>Trips</h1>
    <form id="filters">status <input name="status" value="${escapeHTML(query.get("status") || "")}">
    ridepool_type <input name="ridepool_type" value="${escapeHTML(query.get("ridepool_type") || "")}">
    cancel_reason <input name="cancel_reason" value="${escapeHTML(query.get("cancel_reason") || "")}">
    <button>Filter</button></form><div id="table"></div>`;
  document.getElementById("filters").onsubmit = e => {
    e.preventDefault();
    const params = new URLSearchParams([...new FormData(e.target)].filter(([, v]) => v));
    navigate(`/trips?${params}`);
  };
  const columns = ["request_id", "status", "supply_id", "ridepool_type", "requested_at", "pickup_at", "dropoff_at", "wait_seconds",
    "trip_distance_meters", "detour_ratio", "max_co_riders", "reassign_count", "cancel_reason"];
  await pagedTable(document.getElementById("table"), "/api/trips", columns, rowCell,
    { status: query.get("status") || "", ridepool_type: query.get("ridepool_type") || "", cancel_reason: query.get("cancel_reason") || "" });
}

function tripSection(v, mapID) {
  const t = v.trip;
  const group = (title, fields) => `<h2>${title}</h2><table>${fields.map(f =>
    `<tr><td>${labelHTML(f)}</td><td class="number">${f === "supply_id" ? supplyLink(t[f]) : isTimeField(f) ? offset(t[f]) : fmt(t[f])}</td></tr>`).join("")}</table>`;
  return `<h1>${escapeHTML(t.request_id)} <small>${escapeHTML(t.status)}</small> ${v.shared ? '<span class="badge">shared</span>' : ""}</h1>
    <div id="${mapID}" class="map"></div>
    <p>Ridepool: ${v.ridepool.map(tok => `<span class="token" title="${escapeHTML(tok.request_id)}">${tok.token} ${tok.link ? `<a href="${tok.link}">${escapeHTML(tok.request_id)}</a>` : "(this trip)"}</span>`).join("") || "none"}</p>
    <p>${v.supply_link ? `<a href="${v.supply_link}">Supply view at assignment</a>` : "No car assigned"}
      ${v.earlier_supply_ids.length ? ` · Earlier cars: ${v.earlier_supply_ids.map(supplyLink).join(", ")}` : ""}
      ${v.compare_link ? ` · <a href="${v.compare_link}">Compare runs</a>` : ""}</p>
    <div class="grid">
      ${group("Times", ["requested_at", "first_assigned_at", "assigned_at", "driver_arrived_at", "pickup_at", "dropoff_at", "canceled_at", "wait_seconds", "trip_duration_seconds", "direct_duration_seconds", "eta_at_assignment_seconds"])}
      ${group("Distances", ["trip_distance_meters", "direct_distance_meters", "pickup_distance_meters", "detour_ratio"])}
      ${group("Assignment", ["supply_id", "reassign_count", "ridepool_type", "max_co_riders", "num_requested_seats", "cancel_reason"])}
      <section><h2>Derived</h2><table>${Object.entries(v.derived).map(([k, x]) => `<tr><td>${labelHTML(k)}</td><td class="number">${fmt(x)}</td></tr>`).join("")}</table></section>
    </div>`;
}
async function drawTripMap(v, element) {
  const map = await makeMap(element);
  if (!map) return null;
  drawLine(map, [v.origin, v.destination], "#888", { weight: 1.5, opacity: 0.8, arrows: false });
  v.path.forEach(seg => drawLine(map, seg.points, seg.kind === "on_trip" ? stateColors.on_trip : stateColors.en_route, { dashed: seg.kind === "to_pickup" }));
  if (v.planned_route) drawLine(map, v.planned_route, stateColors.en_route, { dashed: true });
  const t = v.trip;
  const firstLeg = v.path.find(seg => seg.points.length >= 2);
  const assignedHeading = firstLeg ? heading(firstLeg.points[0], firstLeg.points[1]) || 0 : 0;
  addIcon(map, v.car_at_assignment, "car", { color: stateColors.en_route, rotation: assignedHeading, title: "Car at assignment", info: infoHTML(t, "Car assigned", t.assigned_at) });
  addIcon(map, v.car_at_cancellation, "car", { color: stateColors.idle, title: "Car at cancellation", info: infoHTML(t, "Canceled", t.canceled_at) });
  addIcon(map, v.origin, "origin", { title: "Pickup", info: infoHTML(t, "Pickup", t.pickup_at || t.driver_arrived_at) });
  addIcon(map, v.destination, "destination", { title: "Dropoff", info: infoHTML(t, "Dropoff", t.dropoff_at) });
  fit(map);
  return map;
}
async function tripView(id) {
  const v = await api(`/api/trips/${encodeURIComponent(id)}`);
  app.innerHTML = tripSection(v, "trip-map");
  await drawTripMap(v, document.getElementById("trip-map"));
}

async function supplyListView() {
  app.innerHTML = `<h1>Supply</h1><div id="table"></div>`;
  const columns = ["supply_id", "online_at", "offline_at", "online_seconds", "idle_seconds", "en_route_seconds", "on_trip_seconds",
    "idle_meters", "en_route_meters", "on_trip_meters", "total_meters", "requests_served", "utilization", "deadhead_ratio"];
  await pagedTable(document.getElementById("table"), "/api/supply", columns, rowCell, {});
}

async function supplyView(id, query) {
  const at = query.get("at");
  const v = await api(`/api/supply/${encodeURIComponent(id)}${at ? `?at=${encodeURIComponent(at)}` : ""}`);
  const s = v.summary;
  const total = v.states.reduce((n, x) => n + (x.end_at - x.start_at), 0) || 1;
  app.innerHTML = `<h1>${escapeHTML(s.supply_id)}</h1><div id="supply-map" class="map"></div>
    <div class="stacked">${v.states.map(x => `<span class="state-${x.state}" style="width:${((x.end_at - x.start_at) / total) * 100}%" title="${x.state} ${x.end_at - x.start_at}s">${x.state}</span>`).join("")}</div>
    <div class="grid"><section><h2>Stats</h2><table>${Object.entries({ ...s, online_seconds: v.online_seconds, total_meters: v.total_meters,
      max_riders_aboard: v.max_riders_aboard, plan_rejected_count: v.plan_rejected_count }).filter(([k]) => k !== "full_polyline")
      .map(([k, x]) => `<tr><td>${labelHTML(k)}</td><td class="number">${isTimeField(k) ? offset(x) : fmt(x)}</td></tr>`).join("")}</table></section>
    <section><h2>Trips</h2><table><tr><th>Request</th><th>Status</th><th>Ridepool Type</th><th></th></tr>${v.trips.map(t =>
      `<tr><td>${tripLink(t.request_id)}</td><td>${escapeHTML(t.status)}</td><td>${escapeHTML(t.ridepool_type)}</td><td>${t.reassigned_away ? "reassigned away" : ""}</td></tr>`).join("")}</table></section></div>
    <h2>Timeline</h2><table id="timeline"><tr><th>Time</th><th>Action</th><th>State After</th><th>Request</th><th>Seats in Use</th><th>Odometer (m)</th></tr>
    ${v.events.map((e, i) => `<tr data-index="${i}" data-at="${e.row.at}" data-request="${escapeHTML(e.row.request_id)}"><td>${offset(e.row.at, "compact")}</td><td>${e.plan ? `<details><summary>${e.row.action}</summary>${e.plan.map(p => `${escapeHTML(p.request_id)}.${p.type} eta ${offset(p.eta_at, "compact")}`).join("<br>")}</details>` : e.row.action}</td>
      <td>${escapeHTML(label(e.row.state_after))}</td><td>${tripLink(e.row.request_id)}</td><td class="number">${e.row.seats_in_use}</td><td class="number">${e.row.odometer_meters}</td></tr>`).join("")}</table>`;
  if (at) {
    const rows = [...document.querySelectorAll("#timeline tr[data-at]")];
    const row = rows.find(r => Number(r.dataset.at) >= Number(at)) || rows[rows.length - 1];
    if (row) { row.classList.add("highlight"); row.scrollIntoView({ block: "center" }); }
  }
  const map = await makeMap(document.getElementById("supply-map"));
  if (!map) return;
  v.segments.forEach(seg => drawLine(map, seg.points, seg.state === "on_trip" && seg.riders_aboard >= 2 ? "#17602f" : stateColors[seg.state] || "#444"));
  v.events.forEach(e => { if (e.planned_route) drawLine(map, e.planned_route, "#555", { dashed: true, weight: 2 }); });
  const tripsByID = Object.fromEntries(v.trips.map(t => [t.request_id, t.trip]));
  v.events.forEach((e, i) => {
    if (e.row.action !== "pickup" && e.row.action !== "dropoff") return;
    const marker = addIcon(map, { lat: Number(e.row.lat), lng: Number(e.row.lng) }, e.row.action === "pickup" ? "origin" : "destination",
      { title: e.row.request_id, info: infoHTML(tripsByID[e.row.request_id], e.row.action === "pickup" ? "Pickup" : "Dropoff", e.row.at) });
    const row = document.querySelector(`#timeline tr[data-index="${i}"]`);
    if (marker && row) {
      marker.addListener("mouseover", () => row.classList.add("highlight"));
      marker.addListener("mouseout", () => row.classList.remove("highlight"));
    }
  });
  if (v.position_at) {
    // state and heading from the segment containing ?at=
    const span = v.states.find(x => x.start_at <= Number(at) && Number(at) < x.end_at);
    let rotation = 0;
    const later = v.events.find(e => Number(e.row.at) > Number(at));
    if (later) rotation = heading(v.position_at, { lat: Number(later.row.lat), lng: Number(later.row.lng) }) ?? 0;
    addIcon(map, v.position_at, "car", { color: stateColors[span ? span.state : "idle"], rotation, title: `Car at ${timeText(at)}` });
  }
  fit(map);
}

async function compareView() {
  const c = await api("/api/compare");
  const verdictClass = v => v === "better" ? "better" : v === "worse" ? "worse" : "";
  app.innerHTML = `<h1>Run comparison</h1>
    ${c.paired ? '<div class="banner ok"><strong>Paired:</strong> identical requests and supply</div>'
      : '<div class="banner bad"><strong>Not paired:</strong> requests or supply differ, so per-trip comparison is off.</div>'}
    ${c.version_mismatch ? `<div class="banner warn">simulator_version differs: ${escapeHTML(c.metadata_a.simulator_version)} vs ${escapeHTML(c.metadata_b.simulator_version)}</div>` : ""}
    ${c.settings_differ ? `<h2>Settings Diff</h2><table><tr><th>Setting</th><th>A</th><th>B</th></tr>${c.settings_diff.map(d =>
      `<tr><td>${labelHTML(d.name)}</td><td>${escapeHTML(d.a)}</td><td>${escapeHTML(d.b)}</td></tr>`).join("")}</table>` : "<p>Settings are identical.</p>"}
    <h2>Aggregate metrics</h2><table><tr><th>Metric</th><th>A</th><th>B</th><th>Delta</th><th>Percent Delta</th></tr>${c.metrics.map(m =>
      `<tr><td><a href="${m.definition_url}" target="_blank" title="${escapeHTML(m.name)}: see definition">${escapeHTML(label(m.name))}</a></td><td class="number">${fmt(m.a)}</td><td class="number">${fmt(m.b)}</td>
       <td class="number ${verdictClass(m.verdict)}">${fmt(m.delta)}</td><td class="number ${verdictClass(m.verdict)}">${m.percent_delta === null ? "n/a" : m.percent_delta.toFixed(1) + "%"}</td></tr>`).join("")}</table>
    <h2>Distributions <small>(A blue, B orange)</small></h2><div class="grid">${Object.entries(c.distributions).map(([name, d]) =>
      `<section><strong>${labelHTML(name)}</strong>${histogramHTML(d.a, d.b)}</section>`).join("")}</div>
    <h2>Fleet</h2><table><tr><th>Car</th><th>Utilization A</th><th>B</th><th>Delta</th><th>Total (m) A</th><th>B</th><th>Delta</th></tr>${c.fleet.map(f =>
      `<tr><td>${supplyLink(f.supply_id)}</td><td class="number">${fmt(f.utilization_a)}</td><td class="number">${fmt(f.utilization_b)}</td><td class="number">${fmt(f.utilization_delta)}</td>
       <td class="number">${fmt(f.total_meters_a)}</td><td class="number">${fmt(f.total_meters_b)}</td><td class="number">${fmt(f.total_meters_delta)}</td></tr>`).join("")}</table>
    ${c.paired ? `<h2>Paired trips</h2><label><input type="checkbox" id="changed"> only trips whose status changed</label><div id="paired"></div>` : ""}`;
  if (!c.paired) return;
  const columns = ["request_id", "status_a", "status_b", "wait_seconds_delta", "trip_distance_meters_delta", "detour_ratio_delta"];
  const render = (row, column) => column === "request_id" ? `<td><a href="${row.link}">${escapeHTML(row.request_id)}</a></td>` : `<td class="number">${fmt(row[column])}</td>`;
  const load = () => pagedTable(document.getElementById("paired"), "/api/compare/trips", columns, render,
    { status_changed: document.getElementById("changed").checked ? "true" : "" });
  document.getElementById("changed").onchange = load;
  await load();
}

async function compareTripView(id) {
  const v = await api(`/api/compare/trips/${encodeURIComponent(id)}`);
  app.innerHTML = `<div class="side-by-side"><section><h2>A (baseline)</h2>${tripSection(v.a, "map-a")}</section>
    <section><h2>B (candidate)</h2>${tripSection(v.b, "map-b")}</section></div>`;
  const [a, b] = await Promise.all([drawTripMap(v.a, document.getElementById("map-a")), drawTripMap(v.b, document.getElementById("map-b"))]);
  if (a && b) {
    let syncing = false;
    const link = (from, to) => from.addListener("bounds_changed", () => {
      if (syncing) return; syncing = true; to.setCenter(from.getCenter()); to.setZoom(from.getZoom()); syncing = false;
    });
    link(a, b); link(b, a);
  }
}

// ---------- Playback ----------
let playbackTimer = null;
async function playbackView(query) {
  const runKey = query.get("run") === "b" && config.compare_enabled ? "b" : "a";
  const first = await api(`/api/playback?run=${runKey}`);
  const start = first.simulation_start_at, end = first.simulation_end_at;
  let now = Math.min(Math.max(Number(query.get("at")) || start, start), end);
  let speed = [1, 10, 60, 300].includes(Number(query.get("speed"))) ? Number(query.get("speed")) : 60;
  let playing = false;
  app.innerHTML = `<h1>Playback</h1>
    <div class="playback-controls">
      <button id="pb-play">Play</button>
      ${[1, 10, 60, 300].map(s => `<button class="pb-speed" data-speed="${s}">${s}x</button>`).join("")}
      <input id="pb-slider" type="range" min="${start}" max="${end}" step="1" value="${now}">
      <span id="pb-time"></span>
      <span id="pb-range" class="muted">${escapeHTML(localCompact(start))} – ${escapeHTML(localFull(end))}</span>
      <form id="pb-jump"><input id="pb-jump-input" placeholder="T+600, 17:30 or 2026-01-07 17:30" title="A T+ offset, an epoch, or a local time (HH:MM[:SS], optionally with YYYY-MM-DD) in ${escapeHTML(displayTimezone())}" size="26"><button>Go</button></form>
      ${config.compare_enabled ? `<label>Run <select id="pb-run"><option value="a">A</option><option value="b">B</option></select></label>` : ""}
    </div>
    <div id="pb-counters" class="playback-counters"></div>
    <div id="playback-map" class="map tall"></div>`;
  if (config.compare_enabled) { const sel = document.getElementById("pb-run"); sel.value = runKey; sel.onchange = () => navigate(`/playback?run=${sel.value}&at=${Math.round(now)}&speed=${speed}`); }
  const map = await makeMap(document.getElementById("playback-map"));

  // Windows of timeline data keyed by their start; prefetched ahead of the playhead.
  const windowSeconds = 900, windows = new Map();
  const windowStart = t => start + Math.floor((t - start) / windowSeconds) * windowSeconds;
  function fetchWindow(from) {
    if (from > end || from < start) return Promise.resolve(null);
    if (!windows.has(from)) windows.set(from, api(`/api/playback?run=${runKey}&from=${from}&to=${Math.min(from + windowSeconds, end)}`));
    return windows.get(from);
  }
  for (const key of windows.keys()) if (Math.abs(key - windowStart(now)) > 2 * windowSeconds) windows.delete(key);

  const cars = new Map(), requestMarkers = new Map(), headings = new Map();
  let fitted = false;
  async function render() {
    const t = Math.round(now);
    const data = await fetchWindow(windowStart(now));
    fetchWindow(windowStart(now) + windowSeconds); // prefetch
    for (const key of [...windows.keys()]) if (key < windowStart(now) - windowSeconds) windows.delete(key);
    if (!data) return;
    const byState = {}, liveCars = new Set(), liveRequests = new Set();
    let open = 0, active = 0;
    for (const car of data.cars) {
      // At a boundary prefer the segment starting now (e.g. a pickup at T+0), so
      // the car's state matches the request icons; the last segment holds at its end.
      const seg = car.segments.find(s => s.start_at <= now && now < s.end_at) || car.segments.find(s => s.start_at <= now && now <= s.end_at);
      if (!seg) continue;
      const f = seg.end_at > seg.start_at ? (now - seg.start_at) / (seg.end_at - seg.start_at) : 0;
      const [p, a, b] = alongPath(seg.path || [seg.from, seg.to], f);
      const h = heading(a, b);
      if (h !== null) headings.set(car.supply_id, h);
      const rotation = headings.get(car.supply_id) || 0, color = carColor(seg.state, seg.riders_aboard);
      byState[seg.state] = (byState[seg.state] || 0) + 1;
      liveCars.add(car.supply_id);
      const info = () => `<div class="info"><strong>${supplyLink(car.supply_id)}</strong><br>${escapeHTML(label(seg.state))}, riders aboard ${seg.riders_aboard}<br>
        <a href="/supply/${encodeURIComponent(car.supply_id)}?at=${Math.round(now)}">Supply view at ${offset(Math.round(now))}</a></div>`;
      if (!map) continue;
      const existing = cars.get(car.supply_id);
      if (existing) { moveIcon(existing, p, color, rotation); existing.__info = info; }
      else cars.set(car.supply_id, addIcon(map, p, "car", { color, rotation, title: car.supply_id, info, extend: !fitted }));
    }
    for (const r of data.requests) {
      const until = r.pickup_at ?? r.canceled_at ?? end;
      let kind = null;
      if (r.requested_at <= now && now < until) { kind = "origin"; open++; }
      else if (r.pickup_at !== undefined && r.pickup_at <= now && now < (r.dropoff_at ?? end)) { kind = "destination"; active++; }
      if (!kind) continue;
      const key = `${r.request_id}:${kind}`;
      liveRequests.add(key);
      if (map && !requestMarkers.has(key)) {
        const info = `<div class="info"><strong>${tripLink(r.request_id)}</strong> (${escapeHTML(r.status)})<br>Requested ${offset(r.requested_at)}
          ${r.pickup_at !== undefined ? `<br>Pickup ${offset(r.pickup_at)}` : ""}${r.dropoff_at !== undefined ? `<br>Dropoff ${offset(r.dropoff_at)}` : ""}
          ${r.canceled_at !== undefined ? `<br>Canceled ${offset(r.canceled_at)}` : ""}${r.supply_id ? `<br>Car ${supplyLink(r.supply_id)}` : ""}</div>`;
        requestMarkers.set(key, addIcon(map, kind === "origin" ? r.origin : r.destination, kind, { title: r.request_id, info, extend: !fitted }));
      }
    }
    for (const [id, m] of cars) if (!liveCars.has(id)) { removeIcon(m); cars.delete(id); }
    for (const [key, m] of requestMarkers) if (!liveRequests.has(key)) { removeIcon(m); requestMarkers.delete(key); }
    if (map && !fitted) { fit(map); fitted = true; }
    document.getElementById("pb-time").innerHTML = offset(t);
    document.getElementById("pb-slider").value = t;
    document.getElementById("pb-slider").title = timeText(t);
    document.getElementById("pb-counters").innerHTML = `Open requests: ${open} · Active trips: ${active} · ` +
      Object.entries(byState).map(([s, n]) => `<span class="swatch" style="background:${stateColors[s]}"></span>${escapeHTML(label(s))}: ${n}`).join(" · ");
  }
  function syncURL() { history.replaceState(null, "", `/playback?${runKey === "b" ? "run=b&" : ""}at=${Math.round(now)}&speed=${speed}`); }
  function setPlaying(on) {
    playing = on;
    document.getElementById("pb-play").textContent = on ? "Pause" : "Play";
    if (playbackTimer) cancelAnimationFrame(playbackTimer);
    playbackTimer = null;
    if (!on) { syncURL(); return; }
    let last = performance.now();
    const tick = async ts => {
      if (!document.getElementById("playback-map")) return; // navigated away
      now = Math.min(end, now + (ts - last) / 1000 * speed); last = ts;
      await render();
      if (now >= end) { setPlaying(false); return; }
      if (playing) playbackTimer = requestAnimationFrame(tick);
    };
    playbackTimer = requestAnimationFrame(tick);
  }
  function highlightSpeed() { document.querySelectorAll(".pb-speed").forEach(b => b.classList.toggle("active", Number(b.dataset.speed) === speed)); }
  document.getElementById("pb-play").onclick = () => { if (now >= end) now = start; setPlaying(!playing); };
  document.querySelectorAll(".pb-speed").forEach(b => b.onclick = () => { speed = Number(b.dataset.speed); highlightSpeed(); if (!playing) syncURL(); });
  document.getElementById("pb-slider").oninput = e => { now = Number(e.target.value); render(); if (!playing) syncURL(); };
  document.getElementById("pb-jump").onsubmit = e => {
    e.preventDefault();
    const text = document.getElementById("pb-jump-input").value.trim();
    const target = parseJump(text, start, now);
    document.getElementById("pb-jump-input").classList.toggle("invalid", target === null);
    if (target === null) return;
    now = Math.min(Math.max(target, start), end);
    render(); if (!playing) syncURL();
  };
  highlightSpeed();
  await render();
}

async function decisionsView() {
  app.innerHTML = `<h1>Matching decisions</h1><p>TBD. This view is reserved for a later spec (docs/tooling_spec.md section 5).</p>`;
}

// ---------- Router ----------
async function route() {
  const url = new URL(location.href);
  const parts = url.pathname.split("/").filter(Boolean).map(decodeURIComponent);
  if (playbackTimer) { cancelAnimationFrame(playbackTimer); playbackTimer = null; }
  app.innerHTML = "Loading…";
  try {
    if (parts.length === 0) await dashboardView();
    else if (parts[0] === "trips" && parts.length === 1) await tripsView(url.searchParams);
    else if (parts[0] === "trips") await tripView(parts[1]);
    else if (parts[0] === "supply" && parts.length === 1) await supplyListView();
    else if (parts[0] === "supply") await supplyView(parts[1], url.searchParams);
    else if (parts[0] === "compare" && parts.length === 1) await compareView();
    else if (parts[0] === "compare" && parts[1] === "trips") await compareTripView(parts[2]);
    else if (parts[0] === "decisions") await decisionsView();
    else if (parts[0] === "playback") await playbackView(url.searchParams);
    else app.innerHTML = "<h1>Not found</h1>";
  } catch (error) {
    app.innerHTML = `<div class="banner bad">${escapeHTML(error.message)}</div>`;
  }
}
function navigate(path) { history.pushState(null, "", path); route(); }
document.addEventListener("click", e => {
  const a = e.target.closest("a");
  if (a && a.origin === location.origin && !a.target && !e.metaKey && !e.ctrlKey) { e.preventDefault(); navigate(a.pathname + a.search); }
});
window.addEventListener("popstate", route);
(async () => {
  config = await api("/api/config");
  if (!config.compare_enabled) document.getElementById("compare-link").classList.add("disabled");
  document.getElementById("tz-label").textContent = `Times in ${displayTimezone()}${config.timezone ? "" : " (browser)"}`;
  document.getElementById("tz-label").title = `Time zone source: ${config.timezone_source}`;
  route();
})();
