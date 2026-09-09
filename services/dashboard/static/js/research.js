"use strict";

(() => {
  const SCENARIOS = ["baseline", "hemat", "hemat_plus"];
  const LABELS = { baseline: "Baseline", hemat: "Hemat", hemat_plus: "Hemat Plus" };
  const COLORS = { baseline: "#687983", hemat: "#0076b9", hemat_plus: "#6fae22" };
  const DEFAULTS = { date: "2025-01-25", scenario: "hemat_plus", time: "12:00", month: "2025-01" };
  const charts = new Map();
  const state = readState();
  let chartsAvailable = false;
  let activeRequestController = null;
  let dailyData = null;
  let dispatchData = null;
  let monthlyData = null;
  let dispatchTableRenderedFor = "";
  let hasRevealed = false;

  function byId(id) { return document.getElementById(id); }
  function setText(id, value) { const element = byId(id); if (element) element.textContent = value; }
  function clamp(value, min, max) { return Math.min(max, Math.max(min, value)); }
  function number(value, digits = 1) { return new Intl.NumberFormat("id-ID", { maximumFractionDigits: digits, minimumFractionDigits: 0 }).format(Number(value) || 0); }
  function idr(value, compact = true) {
    const amount = Number(value) || 0;
    if (compact && Math.abs(amount) >= 1_000_000) return `Rp${number(amount / 1_000_000, 2)} jt`;
    return new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(amount);
  }
  function formatDate(value) { return new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "long", year: "numeric", timeZone: "Asia/Jakarta" }).format(new Date(`${value}T00:00:00+07:00`)); }
  function updateTimestamp() {
    const time = new Intl.DateTimeFormat("id-ID", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false, timeZone: "Asia/Jakarta" }).format(new Date()).replaceAll(".", ":");
    setText("last-updated", `Data dimuat ${time} WIB`);
  }
  function energy(values) { return (values || []).reduce((sum, value) => sum + Number(value || 0), 0) * 0.25; }
  function weatherLabel(value) { return ({ clear: "Cerah", cloudy: "Berawan", variable: "Variabel" })[value] || String(value || "—"); }
  function modeLabel(value) { return ({ discharge: "Discharge", charge: "Charge", idle: "Idle" })[value] || "—"; }

  function normalizePage(value) {
    const page = String(value || "").toLowerCase().replace("ringkasan", "daily");
    return ["daily", "dispatch", "monthly", "risk", "modules"].includes(page) ? page : "daily";
  }

  function readState() {
    const query = new URLSearchParams(window.location.search);
    const pathPage = window.location.pathname.split("/").filter(Boolean).pop();
    const bodyPage = document.body.dataset.page;
    return {
      page: normalizePage(bodyPage === "research" ? pathPage : bodyPage || pathPage),
      date: query.get("date") || DEFAULTS.date,
      scenario: SCENARIOS.includes(query.get("scenario")) ? query.get("scenario") : DEFAULTS.scenario,
      time: /^([01]\d|2[0-3]):(00|15|30|45)$/.test(query.get("time") || "") ? query.get("time") : DEFAULTS.time,
      compare: SCENARIOS.includes(query.get("compare")) ? query.get("compare") : "",
      weather: ["auto", "clear", "cloudy"].includes(query.get("weather")) ? query.get("weather") : "auto"
    };
  }

  function syncURL() {
    const url = new URL(window.location.href);
    ["date", "scenario", "time", "compare", "weather"].forEach((key) => url.searchParams.delete(key));
    if (["daily", "dispatch"].includes(state.page)) {
      url.searchParams.set("date", state.date);
      url.searchParams.set("scenario", state.scenario);
    }
    if (state.page === "daily") url.searchParams.set("time", state.time);
    if (state.page === "dispatch" && state.compare) url.searchParams.set("compare", state.compare);
    if (["daily", "dispatch"].includes(state.page) && state.weather !== "auto") url.searchParams.set("weather", state.weather);
    window.history.replaceState(null, "", url);
  }

  function shortDate(value) {
    return new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "Asia/Jakarta" }).format(new Date(`${value}T00:00:00+07:00`));
  }

  function updateFilterSummary() {
    if (state.page === "daily") setText("filter-summary", `${shortDate(state.date)} · ${state.time} · ${LABELS[state.scenario]}`);
    else if (state.page === "dispatch") setText("filter-summary", `${shortDate(state.date)} · ${LABELS[state.scenario]}`);
    else setText("filter-summary", LABELS[state.scenario]);
  }

  function toggleFilterPanel(force) {
    const button = byId("filter-toggle");
    const panel = byId("context-filter-panel");
    const expanded = typeof force === "boolean" ? force : button.getAttribute("aria-expanded") !== "true";
    button.setAttribute("aria-expanded", String(expanded));
    button.querySelector("b").textContent = expanded ? "Tutup" : "Ubah Filter";
    panel.classList.toggle("is-open", expanded);
  }

  function setStatus(kind, message) {
    const node = byId("page-status");
    const text = byId("page-status-text");
    const retry = byId("retry-button");
    if (!node || !text || !retry) return;
    node.className = `page-status is-${kind}`;
    text.textContent = message;
    retry.hidden = kind !== "error";
    document.body.classList.toggle("is-loading", kind === "loading");
    document.body.classList.toggle("is-stale", kind === "error");
    if (kind === "success" || kind === "warning") {
      updateTimestamp();
      if (!hasRevealed) {
        hasRevealed = true;
        requestAnimationFrame(() => document.body.classList.add("ui-ready"));
      }
    }
    if (kind === "error") setText("last-updated", "Pemuatan gagal");
  }

  function authHeaders() {
    const token = localStorage.getItem("access_token");
    if (!token) return { Accept: "application/json" };
    return { Accept: "application/json", Authorization: `Bearer ${token}` };
  }

  function decodeTokenPayload(token) {
    const parts = token.split(".");
    if (parts.length !== 3) return null;
    const payload = parts[1];
    const base64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const json = decodeURIComponent(atob(base64).split("").map(c =>
      "%" + ("00" + c.charCodeAt(0).toString(16)).slice(-2)
    ).join(""));
    try { return JSON.parse(json); } catch (_) { return null; }
  }

  function isTokenExpiringSoon(token) {
    const claims = decodeTokenPayload(token);
    if (!claims || !claims.exp) return true;
    const expiresAt = claims.exp * 1000;
    const now = Date.now();
    const windowMs = 5 * 60 * 1000;
    return now >= expiresAt - windowMs;
  }

  async function refreshAccessToken() {
    const refreshToken = localStorage.getItem("refresh_token");
    if (!refreshToken) return false;
    try {
      const response = await fetch("/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      const data = await response.json();
      if (!response.ok || !data.success) {
        return false;
      }
      localStorage.setItem("access_token", data.data.access_token);
      localStorage.setItem("refresh_token", data.data.refresh_token);
      return true;
    } catch (_) {
      return false;
    }
  }

  async function ensureValidToken() {
    const token = localStorage.getItem("access_token");
    if (!token) return false;
    if (isTokenExpiringSoon(token)) {
      const refreshed = await refreshAccessToken();
      if (!refreshed) {
        localStorage.removeItem("access_token");
        localStorage.removeItem("refresh_token");
        window.location.href = "/login";
        return false;
      }
    }
    return true;
  }

  async function fetchJSON(path, params, signal) {
    const url = new URL(path, window.location.origin);
    Object.entries(params || {}).forEach(([key, value]) => {
      if (value !== "" && value !== null && value !== undefined) url.searchParams.set(key, value);
    });
    const tokenValid = await ensureValidToken();
    const headers = tokenValid ? authHeaders() : { Accept: "application/json" };
    const response = await fetch(url, { headers, signal });
    let payload;
    try { payload = await response.json(); } catch (_error) { payload = null; }
    if (response.status === 401) {
      localStorage.removeItem("access_token");
      localStorage.removeItem("refresh_token");
      window.location.href = "/login";
      throw new Error("Sesi berakhir. Silakan masuk kembali.");
    }
    if (!response.ok) throw new Error(payload && payload.error ? payload.error : `Permintaan gagal (${response.status})`);
    if (!payload || typeof payload !== "object") throw new Error("Respons API tidak valid.");
    return payload;
  }

  async function apiCall(method, path, body, params) {
    const url = new URL(path, window.location.origin);
    Object.entries(params || {}).forEach(([key, value]) => {
      if (value !== "" && value !== null && value !== undefined) url.searchParams.set(key, value);
    });
    const options = {
      method: method || "GET",
      headers: Object.assign({}, authHeaders(), { "Content-Type": "application/json" }),
    };
    if (body !== undefined && body !== null) {
      options.body = typeof body === "string" ? body : JSON.stringify(body);
    }
    const response = await fetch(url, options);
    let payload;
    try { payload = await response.json(); } catch (_error) { payload = null; }
    if (response.status === 401) {
      window.location.href = "/login";
      throw new Error("Sesi berakhir. Silakan masuk kembali.");
    }
    if (!response.ok) {
      const errMsg = (payload && payload.error && payload.error.message) ? payload.error.message : (payload && payload.error) || `Permintaan gagal (${response.status})`;
      throw new Error(errMsg);
    }
    return payload;
  }

  function configureCharts() {
    if (!window.Chart) return false;
    Chart.defaults.font.family = '"IBM Plex Sans", Aptos, sans-serif';
    Chart.defaults.font.size = 11;
    Chart.defaults.color = "#687983";
    Chart.defaults.animation = false;
    Chart.defaults.plugins.legend.labels.usePointStyle = true;
    Chart.defaults.plugins.legend.labels.boxWidth = 8;
    Chart.defaults.plugins.legend.labels.padding = 16;
    Chart.defaults.plugins.legend.labels.font = { size: 11, weight: "600" };
    Chart.defaults.plugins.tooltip.backgroundColor = "#0f172a";
    Chart.defaults.plugins.tooltip.padding = 12;
    Chart.defaults.plugins.tooltip.titleFont = { size: 11 };
    Chart.defaults.plugins.tooltip.bodyFont = { size: 11 };
    return true;
  }

  function makeChart(id, config) {
    const canvas = byId(id);
    if (!canvas) return null;
    const fallback = canvas.parentElement.querySelector(".chart-fallback");
    if (!window.Chart) {
      canvas.hidden = true;
      if (!fallback) {
        const message = document.createElement("p");
        message.className = "chart-fallback";
        message.textContent = "Grafik belum tersedia. Ringkasan angka dan tabel tetap dapat digunakan.";
        canvas.parentElement.appendChild(message);
      }
      return null;
    }
    canvas.hidden = false;
    if (fallback) fallback.remove();
    const current = charts.get(id);
    if (current) current.destroy();
    const chart = new Chart(canvas, config);
    charts.set(id, chart);
    const redraw = () => {
      if (charts.get(id) !== chart) return;
      chart.resize();
      chart.update("none");
    };
    requestAnimationFrame(redraw);
    const animatedPanel = canvas.closest(".page-view > *");
    if (animatedPanel && !document.body.classList.contains("ui-ready")) {
      const redrawAfterReveal = (event) => {
        if (event.target !== animatedPanel) return;
        animatedPanel.removeEventListener("animationend", redrawAfterReveal);
        redraw();
      };
      animatedPanel.addEventListener("animationend", redrawAfterReveal);
    }
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(redraw);
    return chart;
  }

  function axisOptions(unit, beginAtZero = true) {
    return {
      beginAtZero,
      border: { display: false },
      grid: { color: "rgba(226,232,240,.8)", drawTicks: false },
      ticks: { padding: 8, font: { size: 10 }, maxTicksLimit: 6, callback: (value) => `${number(value, 0)}${unit ? ` ${unit}` : ""}` }
    };
  }

  function lineDataset(label, data, color, axis = "y", options = {}) {
    return {
      label,
      data,
      borderColor: color,
      backgroundColor: options.background || color,
      yAxisID: axis,
      borderWidth: options.width || 2,
      borderDash: options.dash || [],
      pointRadius: 0,
      pointHoverRadius: 4,
      tension: .18,
      fill: options.fill || false,
      spanGaps: true
    };
  }

  function setKpiDelta(id, selected, baseline, preference = "min") {
    const node = byId(id);
    const current = Number(selected || 0);
    const reference = Number(baseline || 0);
    if (state.scenario === "baseline") {
      node.textContent = "Acuan Baseline";
      node.dataset.trend = "neutral";
      return;
    }
    if (!reference) {
      node.textContent = current ? "Baseline bernilai 0" : "Setara Baseline";
      node.dataset.trend = current ? (preference === "max" ? "good" : "bad") : "neutral";
      return;
    }
    const delta = (current - reference) / Math.abs(reference) * 100;
    const good = preference === "max" ? delta > 0 : delta < 0;
    node.textContent = `${delta > 0 ? "+" : delta < 0 ? "−" : ""}${number(Math.abs(delta), 1)}% vs Baseline`;
    node.dataset.trend = Math.abs(delta) < .05 ? "neutral" : good ? "good" : "bad";
  }

  function setCostBar(id, value, total) {
    const share = total > 0 ? Number(value || 0) / total * 100 : 0;
    byId(id).style.width = `${clamp(share, 0, 100)}%`;
  }

  const chartValueLabels = {
    id: "researchValueLabels",
    afterDatasetsDraw(chart, _args, options) {
      if (!options || !options.enabled) return;
      const dataset = chart.data.datasets[0];
      const meta = chart.getDatasetMeta(0);
      const formatter = options.formatter || ((value) => number(value, 1));
      const { ctx } = chart;
      ctx.save();
      ctx.fillStyle = "#243844";
      ctx.font = '600 10px "IBM Plex Mono", monospace';
      ctx.textAlign = "center";
      meta.data.forEach((element, index) => {
        const value = dataset.data[index];
        ctx.fillText(formatter(value, index), element.x, Math.max(element.y - 9, chart.chartArea.top + 11));
      });
      ctx.restore();
    }
  };

  const timePhasePlugin = {
    id: "researchTimePhases",
    beforeDatasetsDraw(chart, _args, options) {
      if (!options || !options.enabled || !chart.scales.x) return;
      const bands = [
        { start: 0, end: 24, label: "Malam", color: "rgba(8,47,87,.045)" },
        { start: 24, end: 72, label: "Jendela surya", color: "rgba(223,111,36,.055)" },
        { start: 72, end: 95, label: "Malam", color: "rgba(8,47,87,.045)" }
      ];
      const { ctx, chartArea, scales } = chart;
      ctx.save();
      bands.forEach((band) => {
        const left = scales.x.getPixelForValue(band.start);
        const right = scales.x.getPixelForValue(band.end);
        ctx.fillStyle = band.color;
        ctx.fillRect(left, chartArea.top, right - left, chartArea.bottom - chartArea.top);
        ctx.fillStyle = "#7d8c94";
        ctx.font = '600 8px "IBM Plex Sans", sans-serif';
        ctx.textAlign = "center";
        ctx.fillText(band.label.toUpperCase(), (left + right) / 2, chartArea.top + 11);
      });
      ctx.restore();
    }
  };

  const lineEndLabels = {
    id: "researchLineEndLabels",
    afterDatasetsDraw(chart, _args, options) {
      if (!options || !options.enabled) return;
      const { ctx, chartArea } = chart;
      ctx.save();
      chart.data.datasets.forEach((dataset, datasetIndex) => {
        const meta = chart.getDatasetMeta(datasetIndex);
        const visibleEnd = Number.isFinite(Number(chart.options.scales.x.max)) ? Number(chart.options.scales.x.max) : meta.data.length - 1;
        const last = meta.data[visibleEnd];
        if (!last || meta.hidden) return;
        ctx.fillStyle = dataset.borderColor;
        ctx.font = '600 9px "IBM Plex Sans", sans-serif';
        ctx.textAlign = "right";
        const offset = (datasetIndex - (chart.data.datasets.length - 1) / 2) * 10;
        ctx.fillText(dataset.label, chartArea.right - 4, Math.min(Math.max(last.y - 5 + offset, chartArea.top + 11), chartArea.bottom - 4));
      });
      ctx.restore();
    }
  };

  function syncDispatchHover(sourceId, elements) {
    const targetId = sourceId === "chart-dispatch-power" ? "chart-dispatch-soc" : "chart-dispatch-power";
    const target = charts.get(targetId);
    if (!target) return;
    if (!elements.length) {
      target.setActiveElements([]);
      target.tooltip.setActiveElements([], { x: 0, y: 0 });
      target.update("none");
      return;
    }
    const index = elements[0].index;
    const active = target.data.datasets.map((_dataset, datasetIndex) => ({ datasetIndex, index }));
    target.setActiveElements(active);
    target.tooltip.setActiveElements(active, { x: target.scales.x.getPixelForValue(index), y: target.chartArea.top });
    target.update("none");
  }

  function renderDailyChart() {
    if (!dailyData) return;
    const comparisons = dailyData.scenario_comparisons || {};
    const metric = byId("daily-metric").value;
    const configurations = {
      cost: { label: "Total biaya", unit: "juta IDR", value: (row) => Number(row.demo_total_with_om_idr) / 1_000_000, prefer: "min" },
      fuel: { label: "Konsumsi BBM", unit: "liter", value: (row) => Number(row.fuel_l), prefer: "min" },
      hours: { label: "Jam genset", unit: "jam", value: (row) => Number(row.generator_hours), prefer: "min" },
      pv: { label: "PV terserap", unit: "kWh", value: (row) => Number(row.pv_absorbed_kwh), prefer: "max" }
    };
    const config = configurations[metric];
    const values = SCENARIOS.map((name) => config.value(comparisons[name] || {}));
    const targetValue = config.prefer === "max" ? Math.max(...values) : Math.min(...values);
    const bestIndex = values.indexOf(targetValue);
    setText("daily-chart-summary", `${LABELS[SCENARIOS[bestIndex]]} mencatat ${config.label.toLowerCase()} ${number(targetValue, 2)} ${config.unit}, nilai ${config.prefer === "max" ? "tertinggi" : "terendah"} pada tanggal terpilih.`);
    makeChart("chart-daily-comparison", {
      type: "bar",
      data: {
        labels: SCENARIOS.map((name) => LABELS[name]),
        datasets: [{ label: config.label, data: values, backgroundColor: SCENARIOS.map((name) => name === state.scenario ? COLORS[name] : `${COLORS[name]}99`), borderColor: SCENARIOS.map((name) => COLORS[name]), borderWidth: SCENARIOS.map((name) => name === state.scenario ? 2 : 0), borderRadius: 3, borderSkipped: false, maxBarThickness: 88 }]
      },
      plugins: [chartValueLabels],
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: "index", intersect: false },
        plugins: { legend: { display: false }, tooltip: { callbacks: { label: (context) => `${config.label}: ${number(context.raw, 2)} ${config.unit}` } }, researchValueLabels: { enabled: true, formatter: (value) => `${number(value, 2)}${config.unit === "juta IDR" ? " jt" : ""}` } },
        scales: { x: { grid: { display: false }, border: { display: false }, ticks: { font: { size: 11, weight: "600" } } }, y: { ...axisOptions(config.unit === "juta IDR" ? "jt" : "", true), grace: "14%" } }
      }
    });
  }

  async function loadDaily(signal) {
    setStatus("loading", "Memuat snapshot dan KPI harian…");
    const data = await fetchJSON("/api/v1/dashboard/daily", { date: state.date, scenario: state.scenario, time: state.time }, signal);
    dailyData = data;
    const snapshot = data.snapshot || {};
    const cumulative = data.cumulative_kpi || {};
    const finalKpi = data.final_daily_kpi || {};
    const costs = finalKpi.costs || {};
    const source = costs.source_cost || {};
    const withOm = costs.demo_cost_with_om || {};
    const comparisons = data.scenario_comparisons || {};
    const baselineCost = Number((comparisons.baseline || {}).demo_total_with_om_idr || 0);
    const selectedCost = Number((comparisons[state.scenario] || {}).demo_total_with_om_idr || 0);
    const costDelta = baselineCost ? ((selectedCost - baselineCost) / baselineCost) * 100 : 0;
    const isBaseline = state.scenario === "baseline";

    setText("d-gen-status", snapshot.genset_status || "—");
    setText("d-gen-hours", `${number(cumulative.generator_hours, 2)} jam kumulatif`);
    setText("d-pv-power", `${number(snapshot.pv_absorbed_kw, 1)} kW`);
    setText("d-pv-energy", `${number(cumulative.pv_absorbed_kwh, 1)} kWh kumulatif`);
    setText("d-soc", `${number(snapshot.soc_pct, 1)}%`);
    setText("d-battery-mode", `${modeLabel(snapshot.battery_mode)} · ${number(snapshot.battery_kw, 1)} kW`);
    setText("d-load", `${number(snapshot.load_kw, 1)} kW`);
    setText("d-load-energy", `${number(cumulative.load_kwh, 1)} kWh kumulatif`);
    setText("daily-weather", weatherLabel(data.weather));
    setText("daily-insight-scenario", LABELS[state.scenario]);
    setText("daily-insight-title", isBaseline ? "Baseline menjadi acuan kinerja harian" : `${LABELS[state.scenario]} menekan biaya ${number(Math.abs(costDelta), 1)}% dari Baseline`);
    setText("daily-insight-copy", `Pada ${state.time} WIB, rekonstruksi mencatat genset ${snapshot.genset_status === "ON" ? "aktif" : "tidak aktif"}, PV terserap ${number(snapshot.pv_absorbed_kw, 1)} kW, beban ${number(snapshot.load_kw, 1)} kW, dan SOC BESS ${number(snapshot.soc_pct, 1)}%.`);
    setText("daily-cost-delta", isBaseline ? "Acuan" : `${costDelta < 0 ? "−" : "+"}${number(Math.abs(costDelta), 1)}%`);
    setText("daily-cost-delta-copy", isBaseline ? "Skenario pembanding utama" : `${idr(Math.abs(baselineCost - selectedCost))} ${costDelta < 0 ? "lebih rendah" : "lebih tinggi"}`);
    setText("kpi-fuel", number(finalKpi.fuel_l, 1));
    setText("kpi-pv", number(finalKpi.pv_absorbed_kwh, 1));
    setText("kpi-curtail", number(finalKpi.curtailment_kwh, 1));
    setText("kpi-ens", number(finalKpi.ens_kwh, 3));
    setText("kpi-source-cost", idr(source.total_idr));
    setText("kpi-demo-cost", idr(withOm.total_idr));
    setText("cost-fuel", idr(source.fuel_cost_idr));
    setText("cost-ens", idr(source.ens_penalty_idr));
    setText("cost-start", idr(source.transition_penalty_idr));
    setText("cost-om", idr(withOm.om_cost_idr));
    setText("cost-total", idr(withOm.total_idr));
    setText("cost-total-summary", idr(withOm.total_idr));
    setText("daily-om-label", `O&M ${number(withOm.om_rate_pct_of_fuel_cost, 2)}% biaya BBM`);
    const baseline = comparisons.baseline || {};
    setKpiDelta("delta-fuel", finalKpi.fuel_l, baseline.fuel_l, "min");
    setKpiDelta("delta-pv", finalKpi.pv_absorbed_kwh, baseline.pv_absorbed_kwh, "max");
    setKpiDelta("delta-curtail", finalKpi.curtailment_kwh, baseline.curtailment_kwh, "min");
    setKpiDelta("delta-ens", finalKpi.ens_kwh, baseline.ens_kwh, "min");
    setKpiDelta("delta-source-cost", source.total_idr, baseline.source_total_cost_idr, "min");
    setKpiDelta("delta-total-cost", withOm.total_idr, baseline.demo_total_with_om_idr, "min");
    setCostBar("cost-fuel-bar", source.fuel_cost_idr, withOm.total_idr);
    setCostBar("cost-ens-bar", source.ens_penalty_idr, withOm.total_idr);
    setCostBar("cost-start-bar", source.transition_penalty_idr, withOm.total_idr);
    setCostBar("cost-om-bar", withOm.om_cost_idr, withOm.total_idr);
    setText("provenance-text", data.provenance || "Kurva direkonstruksi; bukan pengukuran.");
    renderDailyChart();
    setStatus(chartsAvailable ? "success" : "warning", chartsAvailable ? `${formatDate(state.date)} · ${state.time} WIB · data berhasil diperbarui.` : "Data berhasil dimuat, tetapi grafik belum tersedia.");
  }

  function renderDispatchTable() {
    if (!dispatchData) return;
    const key = `${dispatchData.date}:${dispatchData.scenario}`;
    if (dispatchTableRenderedFor === key) return;
    const body = byId("dispatch-table-body");
    const fragment = document.createDocumentFragment();
    dispatchData.timestamps.forEach((timestamp, index) => {
      const row = document.createElement("tr");
      [timestamp.slice(11, 16), number(dispatchData.load_kw[index], 2), number(dispatchData.genset_kw[index], 2), number(dispatchData.pv_absorbed_kw[index], 2), number(dispatchData.battery_kw[index], 2), number(dispatchData.soc_pct[index], 2)].forEach((value) => {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.appendChild(cell);
      });
      fragment.appendChild(row);
    });
    body.replaceChildren(fragment);
    dispatchTableRenderedFor = key;
  }

  function renderDispatchCharts(data) {
    const labels = data.timestamps.map((stamp) => stamp.slice(11, 16));
    const batteryDischarge = data.battery_kw.map((value) => Math.max(Number(value), 0));
    const batteryCharge = data.battery_kw.map((value) => Math.min(Number(value), 0));
    const powerDatasets = [
      lineDataset("Beban", data.load_kw, "#334155", "power", { width: 2.2 }),
      lineDataset("Genset", data.genset_kw, "#0076b9", "power"),
      lineDataset("PV terserap", data.pv_absorbed_kw, "#df6f24", "power", { background: "rgba(223,111,36,.1)", fill: true }),
      lineDataset("BESS discharge", batteryDischarge, "#3d7217", "power", { background: "rgba(61,114,23,.1)", fill: true }),
      lineDataset("BESS charging", batteryCharge, "#78aebf", "power", { background: "rgba(120,174,191,.11)", fill: true })
    ];
    const socDatasets = [
      lineDataset("SOC minimum", labels.map(() => data.soc_bounds_pct.min), "#a83a45", "soc", { dash: [5, 4], width: 1.2 }),
      lineDataset("SOC maksimum", labels.map(() => data.soc_bounds_pct.max), "#a83a45", "soc", { dash: [5, 4], width: 1.2, background: "rgba(111,174,34,.09)", fill: "-1" }),
      lineDataset("SOC", data.soc_pct, "#3d7217", "soc", { width: 2.6 })
    ];
    if (data.comparison) {
      powerDatasets.push(lineDataset(`${LABELS[data.comparison.scenario]} · genset`, data.comparison.genset_kw, "#082f57", "power", { dash: [7, 5], width: 1.4 }));
      socDatasets.push(lineDataset(`${LABELS[data.comparison.scenario]} · SOC`, data.comparison.soc_pct, "#df6f24", "soc", { dash: [7, 5], width: 1.4 }));
    }
    makeChart("chart-dispatch-power", {
      type: "line",
      data: { labels, datasets: powerDatasets },
      plugins: [timePhasePlugin],
      options: { responsive: true, maintainAspectRatio: false, interaction: { mode: "index", intersect: false }, onHover: (_event, elements) => syncDispatchHover("chart-dispatch-power", elements), plugins: { legend: { position: "bottom" }, tooltip: { callbacks: { label: (context) => `${context.dataset.label}: ${number(context.raw, 1)} kW` } }, researchTimePhases: { enabled: true } }, scales: { x: { grid: { display: false }, border: { display: false }, ticks: { maxTicksLimit: 9, font: { size: 10 } } }, power: { ...axisOptions("kW", false), title: { display: true, text: "Daya model (kW)", font: { size: 11 } } } } }
    });
    makeChart("chart-dispatch-soc", {
      type: "line",
      data: { labels, datasets: socDatasets },
      plugins: [timePhasePlugin],
      options: { responsive: true, maintainAspectRatio: false, interaction: { mode: "index", intersect: false }, onHover: (_event, elements) => syncDispatchHover("chart-dispatch-soc", elements), plugins: { legend: { position: "bottom" }, tooltip: { callbacks: { label: (context) => `${context.dataset.label}: ${number(context.raw, 1)}%` } }, researchTimePhases: { enabled: true } }, scales: { x: { grid: { display: false }, border: { display: false }, ticks: { maxTicksLimit: 9, font: { size: 10 } } }, soc: { ...axisOptions("%", false), min: 0, max: 100, title: { display: true, text: "SOC (%)", font: { size: 11 } } } } }
    });
  }

  async function loadDispatch(signal) {
    setStatus("loading", "Menyusun 96 interval profil operasi…");
    const data = await fetchJSON("/api/v1/dashboard/dispatch", { date: state.date, scenario: state.scenario, compare: state.compare }, signal);
    dispatchData = data;
    dispatchTableRenderedFor = "";
    byId("dispatch-table-body").replaceChildren();
    renderDispatchCharts(data);
    const peakLoad = Math.max(...data.load_kw);
    const peakPv = Math.max(...data.pv_absorbed_kw);
    const batteryMin = Math.min(...data.battery_kw);
    const batteryMax = Math.max(...data.battery_kw);
    const labels = data.timestamps.map((stamp) => stamp.slice(11, 16));
    const peakLoadIndex = data.load_kw.indexOf(peakLoad);
    const peakPvIndex = data.pv_absorbed_kw.indexOf(peakPv);
    setText("dispatch-weather", weatherLabel(data.weather));
    setText("dispatch-load-total", `${number(energy(data.load_kw), 1)} kWh`);
    setText("dispatch-gen-total", `${number(energy(data.genset_kw), 1)} kWh`);
    setText("dispatch-pv-total", `${number(energy(data.pv_absorbed_kw), 1)} kWh`);
    setText("dispatch-soc-range", `${number(Math.min(...data.soc_pct), 1)}–${number(Math.max(...data.soc_pct), 1)}%`);
    setText("dispatch-peak-pv-time", `Puncak PV ${labels[peakPvIndex]} · ${number(peakPv, 1)} kW`);
    setText("dispatch-peak-load-time", `Puncak beban ${labels[peakLoadIndex]} · ${number(peakLoad, 1)} kW`);
    setText("dispatch-bess-callout", Math.abs(batteryMin) > batteryMax ? `Charging maks. ${number(Math.abs(batteryMin), 1)} kW` : `Discharge maks. ${number(batteryMax, 1)} kW`);
    setText("dispatch-power-summary", `Puncak beban ${number(peakLoad, 1)} kW, puncak PV terserap ${number(peakPv, 1)} kW, dan rentang daya BESS ${number(batteryMin, 1)} hingga ${number(batteryMax, 1)} kW.`);
    setText("dispatch-soc-summary", `SOC berada pada rentang ${number(Math.min(...data.soc_pct), 1)}–${number(Math.max(...data.soc_pct), 1)}% dengan batas skenario ${number(data.soc_bounds_pct.min, 0)}–${number(data.soc_bounds_pct.max, 0)}%.`);
    setText("provenance-text", data.provenance || "Kurva direkonstruksi; bukan pengukuran.");
    if (byId("dispatch-table-details").open) renderDispatchTable();
    setStatus(chartsAvailable ? "success" : "warning", chartsAvailable ? `${formatDate(state.date)} · profil ${LABELS[state.scenario]} berhasil diperbarui.` : "Data profil berhasil dimuat, tetapi grafik belum tersedia.");
  }

  function appendMonthlyRow(body, label, values) {
    const row = document.createElement("tr");
    const heading = document.createElement("td");
    heading.textContent = label;
    row.appendChild(heading);
    values.forEach((value, index) => {
      const cell = document.createElement("td");
      cell.textContent = value;
      if (SCENARIOS[index] === state.scenario) cell.classList.add("is-active");
      row.appendChild(cell);
    });
    body.appendChild(row);
  }

  function renderMonthlyChart() {
    if (!monthlyData) return;
    const metric = byId("monthly-metric").value;
    const definitions = {
      generator_hours: { label: "Jam genset", unit: "jam" },
      pv_absorbed_kwh: { label: "PV terserap", unit: "kWh" },
      curtailment_kwh: { label: "Curtailment", unit: "kWh" }
    };
    const definition = definitions[metric];
    const labels = monthlyData.daily_series.baseline.map((entry) => entry.date.slice(8));
    const selected = monthlyData.daily_series[state.scenario].map((entry) => Number(entry[metric]));
    const average = selected.reduce((sum, value) => sum + value, 0) / selected.length;
    setText("monthly-chart-summary", `${LABELS[state.scenario]} mencatat rata-rata ${definition.label.toLowerCase()} ${number(average, 2)} ${definition.unit} per hari selama Januari 2025.`);
    makeChart("chart-monthly-trend", {
      type: "line",
      data: { labels, datasets: SCENARIOS.map((name) => lineDataset(LABELS[name], monthlyData.daily_series[name].map((entry) => entry[metric]), COLORS[name], "y", { width: name === state.scenario ? 2.8 : 1.5 })) },
      plugins: [lineEndLabels],
      options: { responsive: true, maintainAspectRatio: false, interaction: { mode: "index", intersect: false }, plugins: { legend: { position: "bottom" }, tooltip: { callbacks: { title: (items) => `${items[0].label} Januari 2025`, label: (context) => `${context.dataset.label}: ${number(context.raw, 2)} ${definition.unit}` } }, researchLineEndLabels: { enabled: true } }, scales: { x: { grid: { display: false }, border: { display: false }, ticks: { maxTicksLimit: 8, font: { size: 10 } } }, y: axisOptions(definition.unit, true) } }
    });
  }

  async function loadMonthly(signal) {
    setStatus("loading", "Memuat perbandingan Januari 2025…");
    const data = await fetchJSON("/api/v1/dashboard/monthly", { month: DEFAULTS.month }, signal);
    monthlyData = data;
    const reports = SCENARIOS.map((name) => data.scenarios[name].source_report);
    const withOm = SCENARIOS.map((name) => data.scenarios[name].demo_cost_with_om);
    const rows = [
      ["BBM (liter)", reports.map((row) => number(row.fuel_l, 1))],
      ["Jam genset", reports.map((row) => number(row.generator_hours, 2))],
      ["PV terserap (kWh)", reports.map((row) => number(row.pv_absorbed_kwh, 1))],
      ["Curtailment (kWh)", reports.map((row) => number(row.curtailment_kwh, 1))],
      ["ENS (kWh)", reports.map((row) => number(row.ens_kwh, 3))],
      ["Intensitas BBM (L/kWh)", reports.map((row) => number(row.fuel_intensity_l_per_kwh, 3))],
      ["Start / stop", reports.map((row) => `${number(row.starts, 0)} / ${number(row.stops, 0)}`)],
      ["Biaya sumber", reports.map((row) => idr(row.source_total_cost_idr))],
      ["Total + O&M", withOm.map((row) => idr(row.total_idr))]
    ];
    const body = byId("monthly-table-body");
    body.replaceChildren();
    rows.forEach(([label, values]) => appendMonthlyRow(body, label, values));
    const baseline = reports[0];
    const best = reports[2];
    const costs = reports.map((row) => Number(row.source_total_cost_idr));
    const scoreIds = ["baseline", "hemat", "hemat-plus"];
    costs.forEach((cost, index) => {
      setText(`score-${scoreIds[index]}-cost`, idr(cost));
      byId(`score-${scoreIds[index]}-bar`).style.width = `${clamp(cost / costs[0] * 100, 0, 100)}%`;
      if (index > 0) setText(`score-${scoreIds[index]}-delta`, `−${number((1 - cost / costs[0]) * 100, 1)}% vs Baseline`);
    });
    setText("monthly-saving", `${number((1 - best.source_total_cost_idr / baseline.source_total_cost_idr) * 100, 1)}%`);
    setText("monthly-intensity", number(best.fuel_intensity_l_per_kwh, 3));
    setText("monthly-hours-drop", `${number(baseline.generator_hours - best.generator_hours, 1)} jam`);
    setText("provenance-text", data.provenance || "Target laporan dan seri harian rekonstruksi.");
    renderMonthlyChart();
    setStatus(chartsAvailable ? "success" : "warning", chartsAvailable ? "Perbandingan Januari 2025 berhasil diperbarui." : "Data bulanan berhasil dimuat, tetapi grafik belum tersedia.");
  }

  const riskMarkerPlugin = {
    id: "researchRiskMarkers",
    afterDatasetsDraw(chart, _args, pluginOptions) {
      if (!pluginOptions || !pluginOptions.markers) return;
      const { ctx, chartArea, scales } = chart;
      pluginOptions.markers.forEach((marker, index) => {
        const x = scales.x.getPixelForValue(marker.value);
        if (x < chartArea.left || x > chartArea.right) return;
        ctx.save();
        ctx.strokeStyle = marker.color;
        ctx.lineWidth = 1.5;
        ctx.setLineDash(index ? [4, 3] : []);
        ctx.beginPath();
        ctx.moveTo(x, chartArea.top);
        ctx.lineTo(x, chartArea.bottom);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.fillStyle = marker.color;
        ctx.font = '700 10px Aptos, "Trebuchet MS", sans-serif';
        ctx.textAlign = x > chartArea.right - 80 ? "right" : "left";
        ctx.fillText(marker.label, x + (x > chartArea.right - 80 ? -5 : 5), chartArea.top + 13 + index * 13);
        ctx.restore();
      });
    }
  };

  function setRiskScale(summary, edges) {
    const minimum = Number(edges[0]);
    const maximum = Number(edges[edges.length - 1]);
    const setMarker = (id, value) => {
      const marker = byId(id);
      const position = clamp((Number(value) - minimum) / (maximum - minimum) * 100, 0, 100);
      marker.style.left = `${position}%`;
    };
    setMarker("risk-marker-mean", summary.mean_cost_idr);
    setMarker("risk-marker-var", summary.var95_cost_idr);
    setMarker("risk-marker-cvar", summary.cvar95_cost_idr);
    setText("risk-scale-mean-value", idr(summary.mean_cost_idr, false));
    setText("risk-scale-var-value", idr(summary.var95_cost_idr, false));
    setText("risk-scale-cvar-value", idr(summary.cvar95_cost_idr, false));
  }

  async function loadRisk(signal) {
    setStatus("loading", "Memuat ringkasan risiko biaya…");
    const data = await fetchJSON("/api/v1/dashboard/risk", null, signal);
    const summary = data.summary || {};
    const parameters = data.parameters || {};
    const histogram = data.histogram || {};
    const edges = histogram.bin_edges_idr || [];
    const reservePct = Number(summary.mean_cost_idr) ? Number(summary.reserve_idr) / Number(summary.mean_cost_idr) * 100 : 0;
    setText("risk-mean", idr(summary.mean_cost_idr));
    setText("risk-var", idr(summary.var95_cost_idr));
    setText("risk-cvar", idr(summary.cvar95_cost_idr));
    setText("risk-reserve", idr(summary.reserve_idr));
    setText("risk-reserve-pct", `${number(reservePct, 1)}%`);
    setText("risk-verdict-title", `Cadangan ${number(reservePct, 1)}% memisahkan biaya rata-rata dari VaR 95%`);
    setText("risk-verdict-copy", `Biaya tipikal ${idr(summary.mean_cost_idr, false)} meningkat menjadi ${idr(summary.var95_cost_idr, false)} pada batas risiko 95%; CVaR menunjukkan rata-rata ekor ${idr(summary.cvar95_cost_idr, false)}.`);
    byId("risk-reserve-pct").parentElement.style.setProperty("--reserve-angle", `${clamp(reservePct / 100 * 360, 0, 360)}deg`);
    setText("risk-count", number(parameters.scenario_count, 0));
    setText("risk-alpha", number(parameters.alpha, 2));
    setText("risk-sigma", number(parameters.pv_sigma, 2));
    setText("risk-fuel-price", `${idr(parameters.fuel_price_idr_per_l, false)}/L`);
    setText("risk-start-penalty", idr(parameters.start_penalty_idr, false));
    setText("risk-chart-summary", `VaR 95% berada ${idr(summary.reserve_idr, false)} di atas biaya rata-rata; CVaR 95% mencapai ${idr(summary.cvar95_cost_idr, false)}.`);
    setText("provenance-text", `${data.provenance.document} · ${data.provenance.section}. ${histogram.note}`);
    setRiskScale(summary, edges);
    const centers = (histogram.counts || []).map((_count, index) => (edges[index] + edges[index + 1]) / 2);
    const markers = histogram.markers_idr || {};
    makeChart("chart-risk", {
      type: "bar",
      data: { datasets: [{ label: "Jumlah skenario", data: centers.map((x, index) => ({ x, y: histogram.counts[index] })), backgroundColor: "#0076b9", borderColor: "#004b87", borderWidth: 1, borderRadius: 3, borderSkipped: false, barPercentage: 1, categoryPercentage: .94 }] },
      plugins: [riskMarkerPlugin],
      options: { responsive: true, maintainAspectRatio: false, parsing: false, plugins: { legend: { display: false }, tooltip: { callbacks: { title: (items) => idr(items[0].parsed.x, false), label: (context) => `${number(context.parsed.y, 0)} skenario` } }, researchRiskMarkers: { markers: [{ label: "Rata-rata", value: markers.mean, color: "#3d7217" }, { label: "VaR 95%", value: markers.var95, color: "#a64b16" }, { label: "CVaR 95%", value: markers.cvar95, color: "#a83a45" }] } }, scales: { x: { type: "linear", min: edges[0], max: edges[edges.length - 1], grid: { display: false }, ticks: { maxTicksLimit: 7, callback: (value) => `${number(value / 1000, 0)}k`, font: { size: 10 } }, title: { display: true, text: "Biaya operasi (IDR)", font: { size: 11 } } }, y: { ...axisOptions("", true), title: { display: true, text: "Frekuensi", font: { size: 11 } } } } }
    });
    setStatus(chartsAvailable ? "success" : "warning", chartsAvailable ? "Ringkasan risiko biaya berhasil diperbarui." : "Data risiko berhasil dimuat, tetapi grafik belum tersedia.");
  }

  async function reloadCurrent() {
    if (activeRequestController) activeRequestController.abort();
    const controller = new AbortController();
    activeRequestController = controller;
    try {
      if (state.page === "daily") await loadDaily(controller.signal);
      else if (state.page === "dispatch") await loadDispatch(controller.signal);
      else if (state.page === "monthly") await loadMonthly(controller.signal);
      else if (state.page === "modules") await window.loadModulesPage(controller.signal);
      else await loadRisk(controller.signal);
    } catch (error) {
      if (error.name === "AbortError") return;
      console.error("Dashboard API:", error);
      setStatus("error", `Data belum dapat diperbarui: ${error.message}. Periksa server lalu coba lagi.`);
    } finally {
      if (activeRequestController === controller) activeRequestController = null;
    }
  }

  function applyWeather(value) {
    state.weather = value;
    if (value === "clear") state.date = "2025-01-25";
    if (value === "cloudy") state.date = "2025-01-11";
    byId("daily-date").value = state.date;
    byId("dispatch-date").value = state.date;
    document.querySelectorAll("[data-weather-date]").forEach((button) => button.setAttribute("aria-pressed", String(button.dataset.weatherDate === state.date && value !== "auto")));
    setText("weather-caption", value === "auto" ? "Klasifikasi dari energi PV" : `${weatherLabel(value)} dipetakan ke ${formatDate(state.date)}`);
    updateFilterSummary();
    syncURL();
    reloadCurrent();
  }

  function populateTimeOptions() {
    const select = byId("daily-time");
    const options = document.createDocumentFragment();
    for (let minute = 0; minute < 24 * 60; minute += 15) {
      const value = `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
      const option = document.createElement("option");
      option.value = value;
      option.textContent = value;
      options.appendChild(option);
    }
    select.replaceChildren(options);
  }

  function bindControls() {
    populateTimeOptions();
    document.body.dataset.page = state.page;
    document.body.dataset.scenario = state.scenario;
    document.querySelectorAll("[data-nav]").forEach((link) => {
      if (link.dataset.nav === state.page) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    });
    byId("global-scenario").value = state.scenario;
    byId("global-weather").value = state.weather;
    byId("daily-date").value = state.date;
    byId("daily-time").value = state.time;
    byId("dispatch-date").value = state.date;
    byId("dispatch-compare").value = state.compare;
    updateFilterSummary();

    if (state.page === "monthly") byId("weather-filter").classList.add("is-hidden");
    if (state.page === "risk") {
      byId("scenario-filter").classList.add("is-hidden");
      byId("weather-filter").classList.add("is-hidden");
    }

    byId("filter-toggle").addEventListener("click", () => toggleFilterPanel());
    byId("global-scenario").addEventListener("change", (event) => { state.scenario = event.target.value; document.body.dataset.scenario = state.scenario; if (state.compare === state.scenario) { state.compare = ""; byId("dispatch-compare").value = ""; } updateFilterSummary(); syncURL(); reloadCurrent(); });
    byId("global-weather").addEventListener("change", (event) => applyWeather(event.target.value));
    byId("daily-date").addEventListener("change", (event) => { state.date = event.target.value; state.weather = "auto"; byId("global-weather").value = "auto"; updateFilterSummary(); syncURL(); reloadCurrent(); });
    byId("daily-time").addEventListener("change", (event) => { state.time = event.target.value; updateFilterSummary(); syncURL(); reloadCurrent(); });
    byId("daily-metric").addEventListener("change", renderDailyChart);
    byId("dispatch-date").addEventListener("change", (event) => { state.date = event.target.value; state.weather = "auto"; byId("global-weather").value = "auto"; updateFilterSummary(); syncURL(); reloadCurrent(); });
    byId("dispatch-compare").addEventListener("change", (event) => { state.compare = event.target.value === state.scenario ? "" : event.target.value; event.target.value = state.compare; syncURL(); reloadCurrent(); });
    byId("monthly-metric").addEventListener("change", renderMonthlyChart);
    byId("dispatch-table-details").addEventListener("toggle", (event) => { if (event.currentTarget.open) renderDispatchTable(); });
    byId("retry-button").addEventListener("click", reloadCurrent);
    document.querySelectorAll("[data-weather-date]").forEach((button) => {
      button.setAttribute("aria-pressed", String(button.dataset.weatherDate === state.date && state.weather !== "auto"));
      button.addEventListener("click", () => { state.date = button.dataset.weatherDate; state.weather = state.date === "2025-01-25" ? "clear" : "cloudy"; document.querySelectorAll("[data-weather-date]").forEach((candidate) => candidate.setAttribute("aria-pressed", String(candidate === button))); byId("global-weather").value = state.weather; byId("dispatch-date").value = state.date; updateFilterSummary(); syncURL(); reloadCurrent(); });
    });
  }

  // Load modules HTML dynamically
  async function loadModulesSection() {
    try {
      const response = await fetch('/static/partials/modules.html');
      const html = await response.text();
      const container = byId('modules-container');
      if (container) container.innerHTML = html;
    } catch (err) {
      console.error("Failed to load modules section:", err);
    }
  }

  async function init() {
    bindControls();
    syncURL();
    chartsAvailable = configureCharts();
    await loadModulesSection();
    try {
      const meta = await fetchJSON("/api/v1/dashboard/meta");
      if (meta.provenance && meta.provenance.curves) setText("provenance-text", `${meta.provenance.curves}. Data operasional tetap terisolasi.`);
      if (meta.dataset) {
        setText("dataset-target-count", `${number(meta.dataset.daily_target_rows, 0)} baris`);
        setText("dataset-interval-count", `${number(meta.dataset.reconstructed_interval_points, 0)} titik`);
        setText("dataset-scenario-count", number(meta.dataset.scenario_count, 0));
      }
    } catch (_error) {
      // Metadata memperkaya konteks; endpoint halaman tetap menjadi sumber data utama.
    }
    try {
      const me = await fetchJSON("/auth/me");
      const nameEl = byId("user-name");
      if (nameEl && me && me.data) {
        nameEl.textContent = me.data.username || me.data.email || "Admin";
      }
    } catch (_error) {
      // Profil opsional; halaman tetap berfungsi tanpa data user.
    }
    const userMenu = byId("user-menu");
    const userMenuBtn = byId("user-menu-btn");
    const userDropdown = byId("user-dropdown");
    if (userMenu && userMenuBtn) {
      userMenuBtn.addEventListener("click", (e) => {
        e.stopPropagation();
        const isOpen = userMenu.classList.toggle("is-open");
        userMenuBtn.setAttribute("aria-expanded", String(isOpen));
        if (userDropdown) userDropdown.hidden = !isOpen;
      });
      document.addEventListener("click", (e) => {
        if (!userMenu.contains(e.target)) {
          userMenu.classList.remove("is-open");
          userMenuBtn.setAttribute("aria-expanded", "false");
          if (userDropdown) userDropdown.hidden = true;
        }
      });
      document.addEventListener("keydown", (e) => {
        if (e.key === "Escape" && userMenu.classList.contains("is-open")) {
          userMenu.classList.remove("is-open");
          userMenuBtn.setAttribute("aria-expanded", "false");
          if (userDropdown) userDropdown.hidden = true;
          userMenuBtn.focus();
        }
      });
    }
    const logoutForm = document.querySelector(".logout-form");
    if (logoutForm) {
      logoutForm.addEventListener("submit", () => {
        localStorage.removeItem("access_token");
        localStorage.removeItem("refresh_token");
      });
    }
    if (localStorage.getItem("access_token")) {
      setInterval(async () => {
        const token = localStorage.getItem("access_token");
        if (token && isTokenExpiringSoon(token)) {
          await refreshAccessToken();
        }
      }, 60 * 1000);
    }
    await reloadCurrent();
    if (window.initModules) window.initModules();
  }

  window.reloadCurrent = reloadCurrent;
  // Ekspor helper functions untuk modules.js
  window.byId = byId;
  window.setText = setText;
  window.number = number;
  window.apiCall = apiCall;
  window.setStatus = setStatus;
  window.updateTimestamp = updateTimestamp;

  document.addEventListener("DOMContentLoaded", init);
})();
