// Plain-JS single-page app for rhs view. Every number comes from /api/...;
// the browser never computes aggregates or loads a whole table.
"use strict";

let config = null;
const app = document.getElementById("app");
const stateColors = { idle: "#9e9e9e", en_route: "#1e66d0", on_trip: "#2e9d4f", going_offline: "#f08c00", offline: "#444" };

function escapeHTML(v) {
  return String(v ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
async function api(path) {
  const response = await fetch(path);
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || body.message || response.statusText);
  return body;
}
function offset(epoch) {
  if (epoch === "" || epoch === undefined || epoch === null) return "";
  const t = Number(epoch) - config.simulation_start_at;
  return `<span title="${escapeHTML(epoch)}">T${t >= 0 ? "+" : ""}${t}</span>`;
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
  supply_utilization: "Supply Utilization", rider_wait: "Rider Wait", rider_inconvenience: "Rider Inconvenience",
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
  const options = { zoom: 13, center: { lat: 37.78, lng: -122.42 } };
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
function addMarker(map, point, label, title, info) {
  if (!map || !point) return null;
  const position = { lat: point.lat, lng: point.lng };
  map.__bounds.extend(position);
  if (config.google_maps_map_id && google.maps.marker) {
    const pin = new google.maps.marker.PinElement({ glyph: label });
    const marker = new google.maps.marker.AdvancedMarkerElement({ map, position, title, content: pin.element, gmpClickable: !!info });
    bindInfo(map, marker, info);
    return marker;
  }
  const marker = new google.maps.Marker({ map, position, label, title });
  bindInfo(map, marker, info);
  return marker;
}
function bindInfo(map, marker, info) {
  if (!info) return;
  marker.addListener("click", () => {
    if (!infoWindow) infoWindow = new google.maps.InfoWindow();
    infoWindow.setContent(info);
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
  if (isTimeField(column)) return `<td class="number">${offset(v)}</td>`;
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
      `<tr><td>${offset(p.at)}</td><td class="number">${p.open_requests}</td><td>${Object.entries(p.cars_by_state).map(([s, n]) => `${label(s)}: ${n}`).join(", ")}</td></tr>`).join("")}</table>
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
  addMarker(map, v.car_at_assignment, "C", "Car at assignment", infoHTML(t, "Car assigned", t.assigned_at));
  addMarker(map, v.car_at_cancellation, "X", "Car at cancellation", infoHTML(t, "Canceled", t.canceled_at));
  addMarker(map, v.origin, "P", "Pickup", infoHTML(t, "Pickup", t.pickup_at || t.driver_arrived_at));
  addMarker(map, v.destination, "D", "Dropoff", infoHTML(t, "Dropoff", t.dropoff_at));
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
    ${v.events.map((e, i) => `<tr data-index="${i}" data-at="${e.row.at}" data-request="${escapeHTML(e.row.request_id)}"><td>${offset(e.row.at)}</td><td>${e.plan ? `<details><summary>${e.row.action}</summary>${e.plan.map(p => `${escapeHTML(p.request_id)}.${p.type} eta ${offset(p.eta_at)}`).join("<br>")}</details>` : e.row.action}</td>
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
    const marker = addMarker(map, { lat: Number(e.row.lat), lng: Number(e.row.lng) }, e.row.action === "pickup" ? "P" : "D", e.row.request_id,
      infoHTML(tripsByID[e.row.request_id], e.row.action === "pickup" ? "Pickup" : "Dropoff", e.row.at));
    const row = document.querySelector(`#timeline tr[data-index="${i}"]`);
    if (marker && row) {
      marker.addListener("mouseover", () => row.classList.add("highlight"));
      marker.addListener("mouseout", () => row.classList.remove("highlight"));
    }
  });
  if (v.position_at) addMarker(map, v.position_at, "🚗", `Car at ${at}`);
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

async function decisionsView() {
  app.innerHTML = `<h1>Matching decisions</h1><p>TBD. This view is reserved for a later spec (docs/tooling_spec.md section 5).</p>`;
}

// ---------- Router ----------
async function route() {
  const url = new URL(location.href);
  const parts = url.pathname.split("/").filter(Boolean).map(decodeURIComponent);
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
  route();
})();
