(function () {
  const form = document.getElementById("connect-form");
  const targetInput = document.getElementById("terminal-target");
  const shellSelect = document.getElementById("terminal-shell");
  const connectButton = document.getElementById("connect-button");
  const statusEl = document.getElementById("status");
  const statusLabel = document.getElementById("status-label");
  const toolbar = document.querySelector(".toolbar");
  const terminalEl = document.getElementById("terminal");
  const vendorWarning = document.getElementById("vendor-warning");
  const replicaPicker = document.getElementById("replica-picker");

  let term = null;
  let fitAddon = null;
  let socket = null;
  let resizeObserver = null;
  let chromeResizeObserver = null;
  let connectionID = 0;
  let targetRequestID = 0;

  function setStatus(state, label) {
    statusEl.dataset.state = state;
    statusLabel.textContent = label || state;
    const active = state === "connecting" || state === "connected";
    connectButton.disabled = state === "connecting" || state === "resolving";
    shellSelect.disabled = active || state === "resolving";
    connectButton.dataset.action = active ? "disconnect" : "connect";
    connectButton.textContent = active ? "Disconnect" : "Connect";
    connectButton.type = active ? "button" : "submit";
    updateChromeLayout();
  }

  function visibleHeight(el) {
    if (!el || el.hidden) {
      return 0;
    }
    return Math.ceil(el.getBoundingClientRect().height);
  }

  function updateChromeLayout() {
    const toolbarHeight = visibleHeight(toolbar);
    const warningHeight = visibleHeight(vendorWarning);
    const pickerHeight = visibleHeight(replicaPicker);
    const terminalTop = toolbarHeight + warningHeight + pickerHeight;
    document.documentElement.style.setProperty("--toolbar-h", toolbarHeight + "px");
    document.documentElement.style.setProperty("--warning-h", warningHeight + "px");
    document.documentElement.style.setProperty("--terminal-top", terminalTop + "px");
    window.requestAnimationFrame(sendResize);
  }

  function watchChromeLayout() {
    if (chromeResizeObserver) {
      updateChromeLayout();
      return;
    }
    if (typeof ResizeObserver !== "function") {
      window.addEventListener("resize", updateChromeLayout);
      updateChromeLayout();
      return;
    }
    chromeResizeObserver = new ResizeObserver(updateChromeLayout);
    [toolbar, vendorWarning, replicaPicker].forEach(function (el) {
      if (el) {
        chromeResizeObserver.observe(el);
      }
    });
    window.addEventListener("resize", updateChromeLayout);
    updateChromeLayout();
  }

  function tokenQuery() {
    const token = new URLSearchParams(window.location.search).get("token");
    return token ? "?token=" + encodeURIComponent(token) : "";
  }

  function selectedShell() {
    return shellSelect.value || "/bin/sh";
  }

  function wsURL(allocationID, shell) {
    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    const query = new URLSearchParams(window.location.search);
    query.set("shell", shell || "/bin/sh");
    const encodedQuery = query.toString();
    return scheme + "//" + window.location.host + "/terminal/allocation/" + encodeURIComponent(allocationID) + (encodedQuery ? "?" + encodedQuery : "");
  }

  function serviceReplicasURL(serviceID) {
    return "/dashboard/api/services/" + encodeURIComponent(serviceID) + "/replicas" + tokenQuery();
  }

  function ensureTerminal() {
    if (term) {
      return true;
    }
    if (!window.Terminal || !window.FitAddon || !window.FitAddon.FitAddon) {
      vendorWarning.hidden = false;
      setStatus("error", "missing assets");
      return false;
    }
    term = new window.Terminal({
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 13,
      theme: {
        background: "#0b1010",
        foreground: "#edf2ef",
        cursor: "#4fd1a1",
        selectionBackground: "#26463a"
      }
    });
    fitAddon = new window.FitAddon.FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalEl);
    term.onData(function (data) {
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "stdin", data: data }));
      }
    });
    resizeObserver = new ResizeObserver(sendResize);
    resizeObserver.observe(terminalEl);
    window.addEventListener("resize", sendResize);
    sendResize();
    return true;
  }

  function sendResize() {
    if (!term || !fitAddon) {
      return;
    }
    fitAddon.fit();
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    }
  }

  function disconnect() {
    connectionID += 1;
    targetRequestID += 1;
    clearReplicaPicker();
    if (socket) {
      socket.close();
      socket = null;
    }
    setStatus("disconnected");
  }

  function clearReplicaPicker() {
    replicaPicker.hidden = true;
    replicaPicker.replaceChildren();
    updateChromeLayout();
  }

  function showReplicaPicker(serviceID, replicas) {
    clearReplicaPicker();
    const heading = document.createElement("div");
    heading.className = "replica-picker-title";
    const kicker = document.createElement("span");
    kicker.className = "replica-picker-kicker";
    kicker.textContent = "Choose replica";
    const service = document.createElement("span");
    service.className = "replica-picker-service";
    service.title = serviceID;
    service.textContent = serviceID;
    heading.appendChild(kicker);
    heading.appendChild(service);
    replicaPicker.appendChild(heading);
    const list = document.createElement("div");
    list.className = "replica-list";
    replicas.forEach(function (replica) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "replica-option";
      button.dataset.allocationId = replica.allocation_id || "";
      const allocation = document.createElement("span");
      allocation.className = "replica-allocation";
      allocation.textContent = replica.allocation_id || "unknown allocation";
      allocation.title = replica.allocation_id || "";
      const node = document.createElement("span");
      node.className = "replica-node";
      node.textContent = replica.node_id || "-";
      node.title = replica.node_id || "";
      button.appendChild(allocation);
      button.appendChild(node);
      list.appendChild(button);
    });
    replicaPicker.appendChild(list);
    replicaPicker.hidden = false;
    updateChromeLayout();
  }

  async function resolveServiceTarget(serviceID, requestID) {
    setStatus("resolving", "resolving");
    const response = await fetch(serviceReplicasURL(serviceID), {
      headers: { Accept: "application/json" }
    });
    if (requestID !== targetRequestID) {
      return "";
    }
    if (!response.ok) {
      throw new Error("service lookup failed");
    }
    const body = await response.json();
    if (requestID !== targetRequestID) {
      return "";
    }
    const replicas = Array.isArray(body.replicas) ? body.replicas.filter(function (replica) {
      return replica && replica.allocation_id;
    }) : [];
    if (replicas.length === 0) {
      throw new Error("no ready replicas");
    }
    if (replicas.length === 1) {
      return replicas[0].allocation_id;
    }
    showReplicaPicker(serviceID, replicas);
    setStatus("disconnected", "choose replica");
    return "";
  }

  async function connectTarget(targetID) {
    targetRequestID += 1;
    const requestID = targetRequestID;
    clearReplicaPicker();
    if (targetID.indexOf("svc-") === 0) {
      try {
        const allocationID = await resolveServiceTarget(targetID, requestID);
        if (allocationID && requestID === targetRequestID) {
          connect(allocationID, selectedShell());
        }
      } catch (err) {
        if (requestID === targetRequestID) {
          setStatus("error", err.message || "service lookup failed");
        }
      }
      return;
    }
    if (requestID === targetRequestID) {
      connect(targetID, selectedShell());
    }
  }

  function connect(allocationID, shell) {
    disconnect();
    if (!ensureTerminal()) {
      return;
    }
    term.reset();
    setStatus("connecting");
    const activeConnectionID = connectionID;
    const ws = new WebSocket(wsURL(allocationID, shell));
    socket = ws;
    ws.onopen = function () {
      if (socket !== ws || connectionID !== activeConnectionID) {
        ws.close();
        return;
      }
      setStatus("connected");
      sendResize();
      term.focus();
    };
    ws.onmessage = function (event) {
      if (socket !== ws || connectionID !== activeConnectionID) {
        return;
      }
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (_) {
        return;
      }
      if (msg.type === "stdout" || msg.type === "stderr") {
        term.write(msg.data || "");
      } else if (msg.type === "exit") {
        term.writeln("");
        term.writeln("[exit " + (msg.exit_code || 0) + "]");
        socket = null;
        ws.close();
        setStatus("exited");
      } else if (msg.type === "error") {
        term.writeln("");
        term.writeln("[error] " + (msg.message || "terminal error"));
        socket = null;
        ws.close();
        setStatus("error");
      }
    };
    ws.onerror = function () {
      if (socket !== ws || connectionID !== activeConnectionID) {
        return;
      }
      socket = null;
      setStatus("error");
    };
    ws.onclose = function () {
      if (socket !== ws || connectionID !== activeConnectionID) {
        return;
      }
      socket = null;
      setStatus("disconnected");
    };
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    const targetID = targetInput.value.trim();
    if (targetID) {
      connectTarget(targetID);
    }
  });

  connectButton.addEventListener("click", function (event) {
    if (connectButton.dataset.action === "disconnect") {
      event.preventDefault();
      disconnect();
    }
  });

  replicaPicker.addEventListener("click", function (event) {
    const option = event.target.closest(".replica-option");
    if (!option) {
      return;
    }
    const allocationID = option.dataset.allocationId;
    if (allocationID) {
      connect(allocationID, selectedShell());
    }
  });
  setStatus(window.__GATEWAYD_VENDOR_READY__ ? "disconnected" : "error", window.__GATEWAYD_VENDOR_READY__ ? "disconnected" : "missing assets");
  if (!window.__GATEWAYD_VENDOR_READY__) {
    vendorWarning.hidden = false;
  }
  watchChromeLayout();
})();
