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

// Pane scroll across a swap.
//
// Every column scrolls inside itself - the strip's columns in their .pane, the
// board's status columns in their .column-body - so the board's five-second
// poll replaces #board *inside* scroll containers. While the old node is gone the
// container has no content, the browser clamps scrollTop to 0, and the column
// would jump back to the top on every refresh. Remember each pane's offset
// under its data-pane key and put it back once the swap has settled, so a
// refresh - and a step through the chain, which rebuilds the whole strip -
// carries on from where the reader was.
//
// The panel is in the map too, which it was not while only #board polled. An
// open workspace polls the whole strip, so the panel is now replaced every
// interval with the SAME task's content and has to keep its place. What still
// belongs at the top is a panel whose content is a DIFFERENT task. A card click
// swaps the whole strip now (so the poll follows the selection), so the swap
// target no longer says which case it is: the panel's data-task does. The reset
// clears the stored offset as well - afterSwap fires before afterSettle, so a
// reset that left the old offset behind would be undone one event later.
//
// Like the stats toggles: one delegated set of listeners, no request, and no
// htmx attribute in the markup.
(function () {
  "use strict";

  var offsets = {};
  // The task the panel showed before the swap in flight.
  var panelTask = null;

  function currentPanelTask() {
    var panel = document.getElementById("panel");
    return panel ? panel.getAttribute("data-task") : null;
  }

  // Keyed on the attribute alone, so any scroll container that carries a
  // data-pane joins in: a .pane of the strip, the panel, or a board column's
  // body.
  function panes() {
    return document.querySelectorAll("[data-pane]");
  }

  function remember() {
    panelTask = currentPanelTask();
    panes().forEach(function (pane) {
      offsets[pane.getAttribute("data-pane")] = pane.scrollTop;
    });
  }

  function restore() {
    panes().forEach(function (pane) {
      var top = offsets[pane.getAttribute("data-pane")];
      if (top > 0 && pane.scrollTop !== top) {
        pane.scrollTop = top;
      }
    });
  }

  // A pane the reader is scrolling right now, so an offset is current even if
  // the swap is triggered from somewhere that does not bubble beforeSwap.
  document.addEventListener(
    "scroll",
    function (event) {
      var pane = event.target;
      if (pane && pane.matches && pane.matches("[data-pane]")) {
        offsets[pane.getAttribute("data-pane")] = pane.scrollTop;
      }
    },
    true
  );

  // A panel holding another task starts at its top: without this the restore
  // below would open the next card's description wherever the previous one
  // was scrolled to. The stored offset goes with it, or the restore on the
  // next afterSettle would put it straight back.
  document.addEventListener("htmx:afterSwap", function () {
    var panel = document.getElementById("panel");
    if (panel && panel.getAttribute("data-task") !== panelTask) {
      panel.scrollTop = 0;
      offsets[panel.getAttribute("data-pane")] = 0;
      panelTask = panel.getAttribute("data-task");
    }
  });

  document.addEventListener("htmx:beforeSwap", remember);
  ["htmx:afterSwap", "htmx:afterSettle", "htmx:load"].forEach(function (name) {
    document.addEventListener(name, restore);
  });
})();

// Strip motion only on a real change.
//
// The slide of the board off the left and the fade-in of the newest column
// are keyframe animations, and a keyframe animation restarts whenever its
// element is inserted afresh. Every poll and every step through the chain
// replaces the whole #strip, so if the server put the animated classes in the
// markup the board would slide away again every five seconds and on every file
// click. Instead the markup only says the state (.strip-shifted, .unit-working)
// and this compares it with the strip it replaces: .strip-enter goes on only
// when the board was on screen before the swap, .unit-enter only on a working
// column whose pane key the previous strip did not hold. A whole page load and
// a history restore animate nothing. No request, no htmx attribute.
(function () {
  "use strict";

  // What the strip held before the swap in flight; null when the swap does
  // not replace #strip.
  var before = null;

  function paneKeys(strip) {
    var keys = {};
    strip.querySelectorAll("[data-pane]").forEach(function (pane) {
      keys[pane.getAttribute("data-pane")] = true;
    });
    return keys;
  }

  document.addEventListener("htmx:beforeSwap", function (event) {
    var target = event.detail && event.detail.target;
    if (!target || target.id !== "strip") {
      before = null;
      return;
    }
    before = {
      shifted: !!target.querySelector(".strip-shifted"),
      keys: paneKeys(target)
    };
  });

  document.addEventListener("htmx:afterSwap", function () {
    if (!before) {
      return;
    }
    var was = before;
    before = null;
    var strip = document.getElementById("strip");
    if (!strip) {
      return;
    }
    var shifted = strip.querySelector(".strip-shifted");
    if (shifted && !was.shifted) {
      shifted.classList.add("strip-enter");
    }
    strip.querySelectorAll(".unit-working").forEach(function (unit) {
      var pane = unit.querySelector("[data-pane]");
      if (pane && !was.keys[pane.getAttribute("data-pane")]) {
        unit.classList.add("unit-enter");
      }
    });
  });
})();
