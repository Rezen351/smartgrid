"use strict";

(() => {
  let modulesList = [];
  let nodesList = [];
  let discoveredList = [];
  let activeNodeForTags = "";
  let liveTelemetryTimer = null;
  let liveActiveNodeID = "";

  window.loadModulesPage = async (signal) => {
    setStatus("loading", "Memuat data modul dan perangkat IoT dari server...");
    try {
      const [modRes, nodeRes, discRes] = await Promise.all([
        apiCall("GET", "/api/module/modules", null, null),
        apiCall("GET", "/api/module/nodes", null, null),
        apiCall("GET", "/api/module/nodes/discovered", null, null),
      ]);

      modulesList = (modRes && modRes.data && modRes.data.modules) || [];
      nodesList = (nodeRes && nodeRes.data && nodeRes.data.nodes) || [];
      discoveredList = (discRes && discRes.data && discRes.data.nodes) || [];

      setText("kpi-mod-count", number(modulesList.length, 0));
      const pairedCount = nodesList.filter((n) => n.paired).length;
      setText("kpi-paired-nodes", number(pairedCount, 0));
      const onlineCount = nodesList.filter((n) => n.status === "online").length;
      setText("kpi-online-nodes", number(onlineCount, 0));
      setText("kpi-discovered-nodes", number(discoveredList.length, 0));

      const discBadge = byId("discovered-badge");
      if (discBadge) {
        if (discoveredList.length > 0) {
          discBadge.textContent = String(discoveredList.length);
          discBadge.style.display = "inline-flex";
        } else {
          discBadge.style.display = "none";
        }
      }

      renderModulesGrid();
      renderNodesTable();
      renderDiscoveredPanel();
      populateModuleSelects();
      populateNodeSelectForTags();

      updateTimestamp();
      setStatus("success", "Data modul & IoT berhasil disinkronkan.");
    } catch (err) {
      if (err.name === "AbortError") return;
      console.error("Modules API error:", err);
      setStatus("error", `Gagal memuat data modul: ${err.message}`);
    }
  };

  function renderModulesGrid() {
    const container = byId("modules-list-container");
    if (!container) return;
    if (modulesList.length === 0) {
      container.innerHTML = `<div class="table-empty" style="grid-column: 1 / -1; background:#fff; border:1px dashed var(--slate-300); border-radius:8px; padding:32px;">Belum ada modul terdaftar. Klik tombol <strong>+ Tambah Modul</strong> untuk membuat modul baru.</div>`;
      return;
    }

    container.innerHTML = modulesList.map((m) => {
      const moduleNodes = nodesList.filter((n) => n.module_id === m.id);
      const onlineNodes = moduleNodes.filter((n) => n.status === "online");
      const desc = m.description ? escapeHtml(m.description) : "<em>Tidak ada deskripsi</em>";

      return `
        <article class="module-card" data-module-id="${escapeHtml(m.id)}">
          <div class="module-card-head">
            <h3>${escapeHtml(m.name)}</h3>
            <span class="module-badge-nodes">${moduleNodes.length} Node (${onlineNodes.length} Online)</span>
          </div>
          <p class="module-card-desc">${desc}</p>
          <div class="module-card-actions">
            <button type="button" class="btn-table-action" onclick="window.manageModuleNodes('${escapeHtml(m.id)}')">Lihat Node</button>
            <button type="button" class="btn-action-icon" title="Edit Modul" onclick="window.editModule('${escapeHtml(m.id)}')">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"></path><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"></path></svg>
            </button>
            <button type="button" class="btn-action-icon danger" title="Hapus Modul" onclick="window.deleteModule('${escapeHtml(m.id)}')">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>
            </button>
          </div>
        </article>
      `;
    }).join("");
  }

  function renderNodesTable() {
    const tbody = byId("nodes-table-body");
    if (!tbody) return;

    const statusFilter = byId("filter-node-status") ? byId("filter-node-status").value : "";
    const moduleFilter = byId("filter-node-module") ? byId("filter-node-module").value : "";

    let filtered = nodesList;
    if (statusFilter) filtered = filtered.filter((n) => n.status === statusFilter);
    if (moduleFilter) filtered = filtered.filter((n) => n.module_id === moduleFilter);

    if (filtered.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" class="table-empty">Tidak ada node yang sesuai filter.</td></tr>`;
      return;
    }

    tbody.innerHTML = filtered.map((n) => {
      const moduleObj = modulesList.find((m) => m.id === n.module_id);
      const moduleName = moduleObj ? escapeHtml(moduleObj.name) : (n.paired ? "Modul Terhapus" : "<em>Belum Dipasangkan</em>");
      const statusClass = n.status === "online" ? "online" : (n.status === "offline" ? "offline" : "unknown");
      const statusText = n.status === "online" ? "Online (Live)" : (n.status === "offline" ? "Offline" : "Unknown");
      const lastSeenText = n.last_seen_at ? formatTimeAgo(n.last_seen_at) : (n.discovered_at ? formatTimeAgo(n.discovered_at) : "—");
      const ipMac = `${escapeHtml(n.ip || "—")}<br><small style="color:var(--slate-400);">${escapeHtml(n.mac || "—")}</small>`;

      return `
        <tr>
          <td><span class="status-pill ${statusClass}">${statusText}</span></td>
          <td><strong>${escapeHtml(n.node_id)}</strong></td>
          <td>${escapeHtml(n.name || "—")}</td>
          <td>${moduleName}</td>
          <td>${ipMac}</td>
          <td><code>${escapeHtml(n.fw_version || "—")}</code></td>
          <td><small title="${escapeHtml(n.last_seen_at || "")}">${lastSeenText}</small></td>
          <td>
            <div class="action-btns-group">
              <button type="button" class="btn-table-action live" title="Lihat Live Telemetri (Redis)" onclick="window.viewLiveTelemetry('${escapeHtml(n.node_id)}')">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="vertical-align:-1px;"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon></svg> Live
              </button>
              ${!n.paired ? `
                <button type="button" class="btn-table-action" onclick="window.openPairModal('${escapeHtml(n.node_id)}')">Pair</button>
              ` : `
                <button type="button" class="btn-table-action" onclick="window.unpairNode('${escapeHtml(n.node_id)}')">Unpair</button>
              `}
              <button type="button" class="btn-table-action" title="Kelola Sensor & Aktuator" onclick="window.manageNodeTags('${escapeHtml(n.node_id)}')">Tags</button>
              <button type="button" class="btn-table-action danger" title="Hapus Node" onclick="window.deleteNode('${escapeHtml(n.node_id)}')">&times;</button>
            </div>
          </td>
        </tr>
      `;
    }).join("");
  }

  function renderDiscoveredPanel() {
    const panel = byId("discovered-panel");
    const container = byId("discovered-nodes-list");
    if (!panel || !container) return;

    if (discoveredList.length === 0) {
      panel.style.display = "none";
      return;
    }

    panel.style.display = "block";
    container.innerHTML = discoveredList.map((d) => {
      return `
        <div class="discovered-card-item">
          <div>
            <strong>${escapeHtml(d.node_id)}</strong>
            <small>IP: ${escapeHtml(d.ip || "—")} · MAC: ${escapeHtml(d.mac || "—")}</small>
          </div>
          <button type="button" class="btn-sm-primary" onclick="window.openPairModal('${escapeHtml(d.node_id)}')">Pair ke Modul</button>
        </div>
      `;
    }).join("");
  }

  function populateModuleSelects() {
    const filterSelect = byId("filter-node-module");
    const pairSelect = byId("pair-target-module");

    const options = modulesList.map((m) => `<option value="${escapeHtml(m.id)}">${escapeHtml(m.name)}</option>`).join("");

    if (filterSelect) {
      const currentVal = filterSelect.value;
      filterSelect.innerHTML = `<option value="">Semua Modul</option>` + options;
      filterSelect.value = currentVal;
    }
    if (pairSelect) {
      pairSelect.innerHTML = `<option value="">-- Pilih Modul --</option>` + options;
    }
  }

  function populateNodeSelectForTags() {
    const select = byId("tags-node-select");
    if (!select) return;
    const currentVal = select.value;
    const options = nodesList.map((n) => `<option value="${escapeHtml(n.node_id)}">${escapeHtml(n.name || n.node_id)} (${escapeHtml(n.node_id)})</option>`).join("");
    select.innerHTML = `<option value="">-- Pilih Node untuk Mengelola Tag --</option>` + options;
    if (currentVal && nodesList.some((n) => n.node_id === currentVal)) {
      select.value = currentVal;
    } else if (activeNodeForTags) {
      select.value = activeNodeForTags;
    }
  }

  // flattenObject recursively walks a telemetry payload and emits dot-path keys
  // for every value, including leaf values and intermediate objects.
  // e.g. { telemetry: { modbus: { cwt1: { temp: 22.5 } } } }
  // becomes [["telemetry", {...}], ["telemetry.modbus", {...}], ["telemetry.modbus.cwt1", {...}], ["telemetry.modbus.cwt1.temp", 22.5]].
  // This mirrors the backend's resolvePath dot-path format so any nested key can be selected as source_key.
  function flattenObject(obj, prefix = "", out = []) {
    if (obj === null || typeof obj !== "object") {
      if (prefix) out.push([prefix, obj]);
      return out;
    }
    if (Array.isArray(obj)) {
      obj.forEach((item, index) => {
        const path = prefix ? `${prefix}.${index}` : String(index);
        flattenObject(item, path, out);
      });
      return out;
    }
    if (prefix) out.push([prefix, obj]);
    Object.entries(obj).forEach(([key, value]) => {
      const path = prefix ? `${prefix}.${key}` : key;
      flattenObject(value, path, out);
    });
    return out;
  }

  async function autoDetectSourceKey(nodeId) {
    const container = byId("key-picker-container");
    if (!container) return;
    const sourceInput = byId("tag-source-key");
    const hint = byId("auto-detect-hint");

    container.innerHTML = `<div class="key-picker"><div class="key-picker-head"><span>Mengambil telemetri live...</span></div></div>`;
    container.style.display = "block";
    if (hint) hint.textContent = "Mengambil payload telemetri terbaru dari Redis...";

    try {
      const payload = await apiCall("GET", `/api/module/nodes/${encodeURIComponent(nodeId)}/live`);
      const dataObj = (payload && payload.data) ? payload.data : payload;
      const entries = (dataObj && typeof dataObj === "object") ? flattenObject(dataObj).filter(([k]) => !["node_id", "timestamp", "time"].includes(k)) : [];

      if (entries.length === 0) {
        container.innerHTML = `<div class="key-picker"><div class="key-picker-head"><span>Telemetri</span></div><div class="key-picker-empty">Belum ada payload telemetri untuk node ini.</div></div>`;
        if (hint) hint.textContent = "Tidak ada data telemetri di Redis untuk node ini.";
        return;
      }

      container.innerHTML = `<div class="key-picker"><div class="key-picker-head"><span>${entries.length} kunci terdeteksi dari payload live</span><button type="button" class="btn-sm-secondary" id="btn-close-key-picker">Tutup</button></div><div class="key-picker-list">${entries.map(([key, val]) => `<div class="key-picker-item${(val !== null && typeof val === 'object') ? ' is-object' : ''}" data-key="${escapeHtml(key)}"><code>${escapeHtml(key)}</code><small>${escapeHtml((val !== null && typeof val === 'object') ? '[object]' : String(val))}</small></div>`).join("")}</div></div>`;

      container.querySelectorAll(".key-picker-item").forEach((item) => {
        item.addEventListener("click", () => {
          const key = item.dataset.key;
          if (sourceInput) sourceInput.value = key;
          container.style.display = "none";
          if (hint) hint.textContent = `Source key terpilih: ${key}`;
        });
      });

      const closeBtn = byId("btn-close-key-picker");
      if (closeBtn) {
        closeBtn.addEventListener("click", () => {
          container.style.display = "none";
          if (hint) hint.textContent = "Klik 'Auto Detect' untuk mengisi source key dari payload telemetri terbaru.";
        });
      }
      if (hint) hint.textContent = `Pilih salah satu kunci di bawah untuk mengisi source key (${entries.length} kunci terdeteksi).`;
    } catch (err) {
      container.innerHTML = `<div class="key-picker"><div class="key-picker-head"><span>Telemetri</span></div><div class="key-picker-empty">Gagal memuat telemetri: ${escapeHtml(err.message)}</div></div>`;
      if (hint) hint.textContent = `Gagal mengambil data live: ${err.message}`;
    }
  }

  async function loadTagsForNode(nodeId) {
    activeNodeForTags = nodeId;
    const manager = byId("tags-manager-content");
    if (!nodeId) {
      if (manager) manager.style.display = "none";
      return;
    }
    if (manager) manager.style.display = "block";

    try {
      const [sensorRes, actRes] = await Promise.all([
        apiCall("GET", `/api/module/nodes/${encodeURIComponent(nodeId)}/tags`),
        apiCall("GET", `/api/module/nodes/${encodeURIComponent(nodeId)}/actuators`),
      ]);

      const sensorTags = (sensorRes && sensorRes.data && sensorRes.data.tags) || [];
      const actTags = (actRes && actRes.data && actRes.data.tags) || [];

      const sTbody = byId("sensor-tags-table-body");
      if (sTbody) {
        if (sensorTags.length === 0) {
          sTbody.innerHTML = `<tr><td colspan="7" class="table-empty">Belum ada tag sensor untuk node ini.</td></tr>`;
        } else {
          sTbody.innerHTML = sensorTags.map((t) => `
            <tr>
              <td><code>${escapeHtml(t.source_key)}</code></td>
              <td>${escapeHtml(t.tag_name)}</td>
              <td>${escapeHtml(t.display_name || "—")}</td>
              <td>${escapeHtml(t.unit || "—")}</td>
              <td>${escapeHtml(t.data_type || "—")}</td>
              <td><span class="status-pill ${t.enabled ? "online" : "offline"}">${t.enabled ? "Aktif" : "Nonaktif"}</span></td>
              <td>
                <button type="button" class="btn-table-action danger" onclick="window.deleteTag('${escapeHtml(nodeId)}', '${escapeHtml(t.id)}', 'sensor')">Hapus</button>
              </td>
            </tr>
          `).join("");
        }
      }

      const aTbody = byId("actuator-tags-table-body");
      if (aTbody) {
        if (actTags.length === 0) {
          aTbody.innerHTML = `<tr><td colspan="6" class="table-empty">Belum ada tag aktuator untuk node ini.</td></tr>`;
        } else {
          aTbody.innerHTML = actTags.map((t) => `
            <tr>
              <td><code>${escapeHtml(t.source_key)}</code></td>
              <td>${escapeHtml(t.tag_name)}</td>
              <td>${escapeHtml(t.display_name || "—")}</td>
              <td>${escapeHtml(t.unit || "—")}</td>
              <td><span class="status-pill ${t.enabled ? "online" : "offline"}">${t.enabled ? "Aktif" : "Nonaktif"}</span></td>
              <td>
                <button type="button" class="btn-table-action danger" onclick="window.deleteTag('${escapeHtml(nodeId)}', '${escapeHtml(t.id)}', 'actuator')">Hapus</button>
              </td>
            </tr>
          `).join("");
        }
      }
    } catch (err) {
      console.error("Tags fetch error:", err);
      alert("Gagal memuat tags: " + err.message);
    }
  }

  window.manageModuleNodes = (moduleId) => {
    const tabNodesBtn = document.querySelector('.mod-tab-btn[data-tab="tab-nodes"]');
    if (tabNodesBtn) tabNodesBtn.click();
    const filterMod = byId("filter-node-module");
    if (filterMod) {
      filterMod.value = moduleId;
      renderNodesTable();
    }
  };

  window.editModule = (moduleId) => {
    const mod = modulesList.find((m) => m.id === moduleId);
    if (!mod) return;
    byId("modal-module-title").textContent = "Edit Modul";
    byId("mod-edit-id").value = mod.id;
    byId("mod-input-name").value = mod.name || "";
    byId("mod-input-desc").value = mod.description || "";
    byId("modal-module").hidden = false;
  };

  window.deleteModule = async (moduleId) => {
    if (!confirm("Apakah Anda yakin ingin menghapus modul ini? Semua node yang ter-pair akan di-unpair.")) return;
    try {
      await apiCall("DELETE", `/api/module/modules/${encodeURIComponent(moduleId)}`);
      await window.reloadCurrent();
    } catch (err) {
      alert("Gagal menghapus modul: " + err.message);
    }
  };

  window.openPairModal = (nodeId) => {
    byId("pair-node-id").value = nodeId;
    byId("pair-node-name").value = "";
    populateModuleSelects();
    byId("modal-pair").hidden = false;
  };

  window.unpairNode = async (nodeId) => {
    if (!confirm(`Unpair node ${nodeId} dari modul saat ini?`)) return;
    try {
      await apiCall("POST", `/api/module/nodes/${encodeURIComponent(nodeId)}/unpair`);
      await window.reloadCurrent();
    } catch (err) {
      alert("Gagal unpair node: " + err.message);
    }
  };

  window.deleteNode = async (nodeId) => {
    if (!confirm(`Hapus node ${nodeId}? Data konfigurasi node akan dihapus.`)) return;
    try {
      await apiCall("DELETE", `/api/module/nodes/${encodeURIComponent(nodeId)}`);
      await window.reloadCurrent();
    } catch (err) {
      alert("Gagal menghapus node: " + err.message);
    }
  };

  window.manageNodeTags = (nodeId) => {
    const tabTagsBtn = document.querySelector('.mod-tab-btn[data-tab="tab-tags"]');
    if (tabTagsBtn) tabTagsBtn.click();
    const select = byId("tags-node-select");
    if (select) {
      select.value = nodeId;
      loadTagsForNode(nodeId);
    }
  };

  window.deleteTag = async (nodeId, tagId, kind) => {
    if (!confirm("Hapus tag ini?")) return;
    try {
      const endpoint = kind === "actuator"
        ? `/api/module/nodes/${encodeURIComponent(nodeId)}/actuators/${encodeURIComponent(tagId)}`
        : `/api/module/nodes/${encodeURIComponent(nodeId)}/tags/${encodeURIComponent(tagId)}`;
      await apiCall("DELETE", endpoint);
      await loadTagsForNode(nodeId);
    } catch (err) {
      alert("Gagal menghapus tag: " + err.message);
    }
  };

  window.viewLiveTelemetry = async (nodeId) => {
    liveActiveNodeID = nodeId;
    const modal = byId("modal-live");
    setText("live-node-subtitle", `Hardware Node: ${nodeId}`);
    setText("live-raw-content", "Mengambil data telemetri live dari Redis...");
    if (modal) modal.hidden = false;

    async function pollLive() {
      if (!liveActiveNodeID || (modal && modal.hidden)) return;
      try {
        const payload = await apiCall("GET", `/api/module/nodes/${encodeURIComponent(liveActiveNodeID)}/live`);
        setText("live-last-timestamp", `Update: ${new Intl.DateTimeFormat("id-ID", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(new Date())}`);
        const dataObj = (payload && payload.data) ? payload.data : payload;
        const jsonStr = JSON.stringify(dataObj, null, 2);
        setText("live-raw-content", jsonStr);
      } catch (err) {
        setText("live-raw-content", `Status: ${err.message}\n(Belum ada telemetri masuk dari MQTT untuk node ini atau payload di Redis sudah kedaluwarsa).`);
      }
    }

    await pollLive();
    if (liveTelemetryTimer) clearInterval(liveTelemetryTimer);
    liveTelemetryTimer = setInterval(pollLive, 2500);
  };

  function escapeHtml(str) {
    if (str === null || str === undefined) return "";
    return String(str).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#039;");
  }

  function formatTimeAgo(isoString) {
    if (!isoString) return "—";
    const then = new Date(isoString).getTime();
    const now = Date.now();
    const diffSec = Math.floor((now - then) / 1000);
    if (diffSec < 10) return "Baru saja";
    if (diffSec < 60) return `${diffSec} detik lalu`;
    const diffMin = Math.floor(diffSec / 60);
    if (diffMin < 60) return `${diffMin} menit lalu`;
    const diffHour = Math.floor(diffMin / 60);
    if (diffHour < 24) return `${diffHour} jam lalu`;
    return `${Math.floor(diffHour / 24)} hari lalu`;
  }

  function bindModulesControls() {
    document.querySelectorAll(".mod-tab-btn").forEach((btn) => {
      btn.addEventListener("click", () => {
        document.querySelectorAll(".mod-tab-btn").forEach((b) => {
          b.classList.remove("is-active");
          b.setAttribute("aria-selected", "false");
        });
        document.querySelectorAll(".mod-tab-pane").forEach((p) => p.classList.remove("is-active"));
        btn.classList.add("is-active");
        btn.setAttribute("aria-selected", "true");
        const target = byId(btn.dataset.tab);
        if (target) target.classList.add("is-active");
      });
    });

    const btnRefreshMod = byId("btn-refresh-modules");
    if (btnRefreshMod) btnRefreshMod.addEventListener("click", window.reloadCurrent);

    const btnOpenCreate = byId("btn-open-create-module");
    if (btnOpenCreate) {
      btnOpenCreate.addEventListener("click", () => {
        byId("modal-module-title").textContent = "Tambah Modul Baru";
        byId("mod-edit-id").value = "";
        byId("mod-input-name").value = "";
        byId("mod-input-desc").value = "";
        byId("modal-module").hidden = false;
      });
    }

    document.querySelectorAll("[data-close-modal]").forEach((btn) => {
      btn.addEventListener("click", () => {
        btn.closest(".modal-overlay").hidden = true;
        if (liveTelemetryTimer) {
          clearInterval(liveTelemetryTimer);
          liveTelemetryTimer = null;
        }
        liveActiveNodeID = "";
        const kp = byId("key-picker-container");
        if (kp) kp.style.display = "none";
      });
    });

    const formModule = byId("form-module");
    if (formModule) {
      formModule.addEventListener("submit", async (e) => {
        e.preventDefault();
        const editId = byId("mod-edit-id").value;
        const name = byId("mod-input-name").value.trim();
        const description = byId("mod-input-desc").value.trim();

        const btnSubmit = byId("btn-submit-module");
        btnSubmit.disabled = true;
        btnSubmit.textContent = "Menyimpan...";

        try {
          if (editId) {
            await apiCall("PUT", `/api/module/modules/${encodeURIComponent(editId)}`, { name, description, config: "" });
          } else {
            await apiCall("POST", "/api/module/modules", { name, description, config: "" });
          }
          byId("modal-module").hidden = true;
          await window.reloadCurrent();
        } catch (err) {
          alert("Gagal menyimpan modul: " + err.message);
        } finally {
          btnSubmit.disabled = false;
          btnSubmit.textContent = "Simpan Modul";
        }
      });
    }

    const formPair = byId("form-pair");
    if (formPair) {
      formPair.addEventListener("submit", async (e) => {
        e.preventDefault();
        const nodeId = byId("pair-node-id").value.trim();
        const moduleId = byId("pair-target-module").value;
        const name = byId("pair-node-name").value.trim();

        if (!moduleId) {
          alert("Pilih modul tujuan terlebih dahulu.");
          return;
        }

        const btnSubmit = byId("btn-submit-pair");
        btnSubmit.disabled = true;
        btnSubmit.textContent = "Memasangkan...";

        try {
          await apiCall("POST", `/api/module/nodes/${encodeURIComponent(nodeId)}/pair`, { module_id: moduleId, name });
          byId("modal-pair").hidden = true;
          await window.reloadCurrent();
        } catch (err) {
          alert("Gagal memasangkan node: " + err.message);
        } finally {
          btnSubmit.disabled = false;
          btnSubmit.textContent = "Pasangkan";
        }
      });
    }

    const filterStatus = byId("filter-node-status");
    if (filterStatus) filterStatus.addEventListener("change", renderNodesTable);
    const filterMod = byId("filter-node-module");
    if (filterMod) filterMod.addEventListener("change", renderNodesTable);

    const tagsNodeSelect = byId("tags-node-select");
    if (tagsNodeSelect) {
      tagsNodeSelect.addEventListener("change", (e) => loadTagsForNode(e.target.value));
    }

    const btnAddSensorTag = byId("btn-add-sensor-tag");
    if (btnAddSensorTag) {
      btnAddSensorTag.addEventListener("click", () => {
        if (!activeNodeForTags) {
          alert("Pilih node terlebih dahulu.");
          return;
        }
        byId("modal-tag-title").textContent = "Tambah Sensor Tag";
        byId("tag-kind").value = "sensor";
        byId("group-tag-datatype").style.display = "grid";
        byId("tag-source-key").value = "";
        byId("tag-name").value = "";
        byId("tag-display-name").value = "";
        byId("tag-unit").value = "";
        byId("tag-datatype").value = "float";
        byId("modal-tag").hidden = false;
        byId("key-picker-container").style.display = "none";
      });
    }

    const btnAutoDetect = byId("btn-auto-detect-key");
    if (btnAutoDetect) {
      btnAutoDetect.addEventListener("click", () => {
        if (!activeNodeForTags) {
          alert("Pilih node terlebih dahulu.");
          return;
        }
        autoDetectSourceKey(activeNodeForTags);
      });
    }

    const btnAddActuatorTag = byId("btn-add-actuator-tag");
    if (btnAddActuatorTag) {
      btnAddActuatorTag.addEventListener("click", () => {
        if (!activeNodeForTags) {
          alert("Pilih node terlebih dahulu.");
          return;
        }
        byId("modal-tag-title").textContent = "Tambah Actuator Tag";
        byId("tag-kind").value = "actuator";
        byId("group-tag-datatype").style.display = "none";
        byId("tag-source-key").value = "";
        byId("tag-name").value = "";
        byId("tag-display-name").value = "";
        byId("tag-unit").value = "";
        byId("modal-tag").hidden = false;
        byId("key-picker-container").style.display = "none";
      });
    }

    const formTag = byId("form-tag");
    if (formTag) {
      formTag.addEventListener("submit", async (e) => {
        e.preventDefault();
        const kind = byId("tag-kind").value;
        const source_key = byId("tag-source-key").value.trim();
        const tag_name = byId("tag-name").value.trim();
        const display_name = byId("tag-display-name").value.trim();
        const unit = byId("tag-unit").value.trim();
        const data_type = byId("tag-datatype").value;

        const body = {
          kind,
          source_key,
          tag_name,
          display_name,
          unit,
          enabled: true,
        };
        if (kind === "sensor") body.data_type = data_type;

        const btnSubmit = byId("btn-submit-tag");
        btnSubmit.disabled = true;
        btnSubmit.textContent = "Menyimpan...";

        try {
          const endpoint = kind === "actuator"
            ? `/api/module/nodes/${encodeURIComponent(activeNodeForTags)}/actuators`
            : `/api/module/nodes/${encodeURIComponent(activeNodeForTags)}/tags`;
          await apiCall("POST", endpoint, body);
          byId("modal-tag").hidden = true;
          await loadTagsForNode(activeNodeForTags);
        } catch (err) {
          alert("Gagal menyimpan tag: " + err.message);
        } finally {
          btnSubmit.disabled = false;
          btnSubmit.textContent = "Simpan Tag";
        }
      });
    }
  }

  window.initModules = () => {
    bindModulesControls();
  };
})();