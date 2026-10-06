// Copy-to-clipboard for [data-copy] controls, e.g. a task's branch name.
// One listener delegated on document, so it keeps working after htmx swaps a
// board or panel in; it stops the click from reaching the card underneath.
// Client-side only: it sends no request.
(function () {
  "use strict";

  // Plain http on a LAN address is not a secure context, so there is no
  // navigator.clipboard there: copy through a hidden textarea instead.
  function fallbackCopy(text) {
    var area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    var ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (e) {
      ok = false;
    }
    document.body.removeChild(area);
    return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
  }

  function copy(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text).catch(function () {
        return fallbackCopy(text);
      });
    }
    return fallbackCopy(text);
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-copy]");
    if (!button) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    if (!button.dataset.label) {
      button.dataset.label = button.textContent;
    }
    copy(button.getAttribute("data-copy")).then(
      function () { button.textContent = "copied"; },
      function () { button.textContent = "failed"; }
    ).then(function () {
      setTimeout(function () { button.textContent = button.dataset.label; }, 1500);
    });
  });
})();

// Legend toggles for the Done column's line charts. Clicking a legend entry
// shows or hides its line; the choice is kept per card and line in
// localStorage (own key prefix, never htmx's history cache) and mirrored in
// memory, so it survives the board poll even when storage is unavailable.
// Every poll swaps in fresh server markup with the config defaults, so the
// recorded choices are re-applied after each swap. Client-side only: it
// sends no request and adds no htmx attribute.
(function () {
  "use strict";

  var PREFIX = "mcp-task-manager.stats-line:";
  var memory = {};

  // JSON keeps the key unambiguous whatever a custom card id contains.
  function key(card, line) {
    return PREFIX + JSON.stringify([card, line]);
  }

  // Reading window.localStorage itself throws where storage is blocked.
  function storage() {
    try {
      return window.localStorage;
    } catch (e) {
      return null;
    }
  }

  function load(card, line) {
    var k = key(card, line);
    if (Object.prototype.hasOwnProperty.call(memory, k)) {
      return memory[k];
    }
    var s = storage();
    if (!s) {
      return null;
    }
    try {
      var v = s.getItem(k);
      return v === "on" || v === "off" ? v : null;
    } catch (e) {
      return null;
    }
  }

  function save(card, line, state) {
    var k = key(card, line);
    memory[k] = state;
    var s = storage();
    if (!s) {
      return;
    }
    try {
      s.setItem(k, state);
    } catch (e) {
      // Quota or privacy mode: the memory copy still holds it.
    }
  }

  // Sets one line of one card explicitly, so re-applying is idempotent.
  function setLine(cardEl, line, on) {
    cardEl.querySelectorAll("[data-stats-line]").forEach(function (el) {
      if (el.getAttribute("data-stats-line") !== line) {
        return;
      }
      if (el.tagName.toLowerCase() === "button") {
        el.setAttribute("aria-pressed", on ? "true" : "false");
      } else {
        el.classList.toggle("stats-off", !on);
      }
    });
  }

  function applyAll() {
    document.querySelectorAll("[data-stats-card]").forEach(function (cardEl) {
      var card = cardEl.getAttribute("data-stats-card");
      cardEl.querySelectorAll("button[data-stats-line]").forEach(function (button) {
        var line = button.getAttribute("data-stats-line");
        var state = load(card, line);
        if (state) {
          setLine(cardEl, line, state === "on");
        }
      });
    });
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest(".stats-legend-item[data-stats-line]");
    if (!button) {
      return;
    }
    var cardEl = button.closest("[data-stats-card]");
    if (!cardEl) {
      return;
    }
    var line = button.getAttribute("data-stats-line");
    var on = button.getAttribute("aria-pressed") === "false";
    setLine(cardEl, line, on);
    save(cardEl.getAttribute("data-stats-card"), line, on ? "on" : "off");
  });

  ["htmx:afterSettle", "htmx:load", "htmx:historyRestore"].forEach(function (name) {
    document.addEventListener(name, applyAll);
  });
  applyAll();
})();
