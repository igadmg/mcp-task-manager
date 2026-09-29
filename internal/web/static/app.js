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
