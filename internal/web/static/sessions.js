/* Sessions UI (design §8). Two small surfaces, no framework:
 *
 *   - the workspace's session list + start form (#sessions-page)
 *   - one live session page (#session-page): an EventSource-driven feed,
 *     question/permission panels, follow-up messages and stop
 *
 * Every string that came from an event, the host or a form goes into the
 * DOM through textContent/values only - model and tool output is untrusted
 * and never becomes HTML.
 */
(function () {
  "use strict";

  var listPage = document.getElementById("sessions-page");
  if (listPage) initList(listPage);
  var page = document.getElementById("session-page");
  if (page) initSession(page);

  function show(el, text) {
    el.textContent = text;
    el.hidden = false;
  }

  function postJSON(url, csrf, payload) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Dashboard-CSRF": csrf },
      body: JSON.stringify(payload || {})
    }).then(function (resp) {
      return resp.json().then(function (data) {
        if (!resp.ok) {
          throw new Error((data.error && data.error.message) || "request failed");
        }
        return data;
      });
    });
  }

  // ---- session list + start form ----

  function initList(root) {
    var csrf = root.getAttribute("data-csrf");
    var form = document.getElementById("session-start-form");
    var err = document.getElementById("session-start-error");
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      err.hidden = true;
      postJSON(root.getAttribute("data-start-url"), csrf, {
        prompt: document.getElementById("session-prompt").value,
        task_id: document.getElementById("session-task-id").value || ""
      }).then(function (created) {
        window.location.assign("sessions/" + encodeURIComponent(created.id));
      }).catch(function (e) {
        show(err, e.message);
      });
    });
  }

  // ---- one live session ----

  var TERMINAL = { finished: true, failed: true, stopped: true };

  function initSession(root) {
    var csrf = csrfToken(root);
    var feed = document.getElementById("session-feed");
    var statusEl = document.getElementById("session-status");
    var errEl = document.getElementById("session-error");
    var connEl = document.getElementById("session-conn");
    var source = new EventSource(root.getAttribute("data-events-url"));

    source.onopen = function () {
      connEl.hidden = true;
    };
    source.onmessage = function (e) {
      connEl.hidden = true;
      var ev;
      try {
        ev = JSON.parse(e.data);
      } catch (err) {
        return;
      }
      handleEvent(root, feed, statusEl, ev);
      if (ev.kind === "status" && TERMINAL[ev.status]) {
        // The host closes the stream when the session ends; the browser
        // would otherwise reconnect forever.
        source.close();
      }
    };
    source.onerror = function () {
      if (TERMINAL[statusEl.getAttribute("data-status")]) {
        // The host closed after replaying a finished session: the feed is
        // complete, so this is not an error and must not reconnect.
        source.close();
        connEl.hidden = true;
        return;
      }
      // Otherwise EventSource reconnects on its own, resuming from the
      // last event id it saw (Last-Event-ID, replayed by the host) - but
      // the retry has to be visible, not silent.
      show(connEl, "Connection lost, retrying…");
    };

    bindControls(root, csrf, errEl, statusEl);
  }

  function csrfToken(root) {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return (meta && meta.getAttribute("content")) || root.getAttribute("data-csrf") || "";
  }

  function setStatus(statusEl, status) {
    statusEl.textContent = status;
    statusEl.setAttribute("data-status", status);
    statusEl.className = "chip sess-status sess-status-" + status;
  }

  // handleEvent appends one normalized event to the feed and maintains the
  // pending-question panel.
  function handleEvent(root, feed, statusEl, ev) {
    switch (ev.kind) {
    case "status":
      setStatus(statusEl, ev.status);
      if (ev.reason) {
        appendLine(feed, "system", "status: " + ev.status + " (" + ev.reason + ")");
      }
      break;
    case "user_message":
      appendLine(feed, "user", "you: " + ev.text);
      break;
    case "assistant_text":
      appendLine(feed, "assistant", ev.text);
      break;
    case "tool_use":
      appendToolUse(feed, ev);
      break;
    case "tool_result":
      appendLine(feed, ev.is_error ? "error" : "tool_result",
        (ev.is_error ? "tool error: " : "tool result: ") + ev.text);
      break;
    case "question":
      addPending(buildQuestionPanel(ev));
      break;
    case "permission":
      addPending(buildPermissionPanel(ev));
      break;
    case "request_resolved":
      removePending(ev.request_id);
      break;
    case "turn_result":
      appendLine(feed, "system", ev.ok ? "turn finished" : "turn failed");
      break;
    case "error":
      appendLine(feed, "error", "error " + ev.code + ": " + ev.message);
      break;
    }
  }

  function appendLine(feed, kind, text) {
    var div = document.createElement("div");
    div.className = "sess-line sess-msg-" + kind;
    div.textContent = text;
    feed.appendChild(div);
    feed.scrollTop = feed.scrollHeight;
  }

  function formatJSON(v) {
    if (v === undefined || v === null || v === "") return "";
    if (typeof v === "string") {
      try {
        return JSON.stringify(JSON.parse(v), null, 2);
      } catch (err) {
        return v;
      }
    }
    // sessionapi events carry input as a JSON object, not a string, so
    // JSON.parse(ev.input) never applies; stringify the value itself.
    try {
      return JSON.stringify(v, null, 2);
    } catch (err) {
      return String(v);
    }
  }

  function appendToolUse(feed, ev) {
    var details = document.createElement("details");
    details.className = "sess-line sess-msg-tool_use";
    var summary = document.createElement("summary");
    summary.textContent = "tool: " + (ev.name || "") + (ev.summary ? " - " + ev.summary : "");
    var pre = document.createElement("pre");
    pre.className = "sess-pre";
    pre.textContent = formatJSON(ev.input);
    details.appendChild(summary);
    details.appendChild(pre);
    feed.appendChild(details);
    feed.scrollTop = feed.scrollHeight;
  }

  // pendingByID matches by exact attribute comparison: request ids are
  // untrusted and interpolating one into a CSS selector throws on quotes
  // or brackets, killing the feed's event loop.
  function pendingByID(requestID) {
    var panels = document.querySelectorAll("#session-pending [data-request-id]");
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute("data-request-id") === requestID) return panels[i];
    }
    return null;
  }

  function addPending(panel) {
    if (!panel) return;
    if (!pendingByID(panel.getAttribute("data-request-id"))) {
      document.getElementById("session-pending").appendChild(panel);
    }
  }

  function removePending(requestID) {
    var panel = pendingByID(requestID);
    if (panel) {
      panel.remove();
    }
  }

  // ---- pending panels (mirror of _pending.html) ----

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function buildQuestionPanel(ev) {
    var panel = el("div", "sess-pending");
    panel.setAttribute("data-request-id", ev.request_id);
    panel.setAttribute("data-kind", "question");
    panel.appendChild(el("p", "sess-pending-title", "The session is asking"));
    (ev.questions || []).forEach(function (q, i) {
      var fieldset = el("fieldset", "sess-question");
      fieldset.setAttribute("data-question", q.question);
      var legend = el("legend", "mb-1 text-sm text-neutral-200");
      if (q.header) {
        legend.appendChild(el("span", "chip chip-muted", q.header));
        legend.appendChild(document.createTextNode(" "));
      }
      legend.appendChild(document.createTextNode(q.question));
      fieldset.appendChild(legend);
      (q.options || []).forEach(function (o, j) {
        var label = el("label", "sess-option");
        var input = el("input");
        input.type = q.multi_select ? "checkbox" : "radio";
        // One name per question, not per option: same-group radios make
        // the browser enforce a single pick. The request id and question
        // index keep groups distinct across questions and requests.
        input.name = "q-" + ev.request_id + "-" + i;
        input.value = o.label;
        label.appendChild(input);
        label.appendChild(el("span", "", o.label));
        if (o.description) label.appendChild(el("span", "meta", o.description));
        fieldset.appendChild(label);
      });
      var otherLabel = el("label", "sess-option");
      var other = el("input", "sess-other mt-1 w-full rounded-lg border border-white/10 bg-white/[0.03] px-3 py-1.5 font-mono text-sm text-neutral-100");
      other.type = "text";
      other.placeholder = "Other";
      otherLabel.appendChild(other);
      fieldset.appendChild(otherLabel);
      panel.appendChild(fieldset);
    });
    panel.appendChild(el("button", "sess-send-answer rounded-lg border border-white/10 px-3 py-1.5 text-sm text-neutral-200 hover:text-neutral-100", "Answer"));
    return panel;
  }

  function buildPermissionPanel(ev) {
    var panel = el("div", "sess-pending");
    panel.setAttribute("data-request-id", ev.request_id);
    panel.setAttribute("data-kind", "permission");
    var title = el("p", "sess-pending-title");
    title.appendChild(document.createTextNode("The session requests permission: "));
    title.appendChild(el("strong", "", ev.tool_name || ""));
    panel.appendChild(title);
    if (ev.description) panel.appendChild(el("p", "text-sm text-neutral-300", ev.description));
    if (ev.input) {
      var details = el("details", "mt-1");
      details.appendChild(el("summary", "meta", "Input"));
      var pre = el("pre", "sess-pre");
      pre.textContent = formatJSON(ev.input);
      details.appendChild(pre);
      panel.appendChild(details);
    }
    var row = el("div", "mt-2 flex flex-wrap items-center gap-2");
    var reason = el("input", "sess-deny-reason min-w-0 flex-1 rounded-lg border border-white/10 bg-white/[0.03] px-3 py-1.5 font-mono text-sm text-neutral-100");
    reason.type = "text";
    reason.placeholder = "Reason for declining (optional)";
    row.appendChild(reason);
    row.appendChild(el("button", "sess-allow rounded-lg border border-emerald-400/30 px-3 py-1.5 text-sm text-emerald-200 hover:text-emerald-100", "Allow"));
    row.appendChild(el("button", "sess-deny rounded-lg border border-rose-400/30 px-3 py-1.5 text-sm text-rose-200 hover:text-rose-100", "Deny"));
    panel.appendChild(row);
    return panel;
  }

  // ---- controls: answers, messages, stop (bound by delegation so the
  // server-rendered pending panels work too) ----

  function bindControls(root, csrf, errEl, statusEl) {
    var answersUrl = root.getAttribute("data-answers-url");
    var messagesUrl = root.getAttribute("data-messages-url");
    var stopUrl = root.getAttribute("data-stop-url");

    function fail(e) {
      show(errEl, e.message);
    }

    // A single-select question cannot hold both a checked option and an
    // "Other" text: picking one clears the other, whichever came last.
    // Delegated so the server-rendered pending panels behave identically.
    root.addEventListener("change", function (ev) {
      var input = ev.target;
      if (!(input instanceof HTMLInputElement) || input.type !== "radio" || !input.checked) return;
      var other = input.closest(".sess-question");
      other = other && other.querySelector(".sess-other");
      if (other) other.value = "";
    });
    root.addEventListener("input", function (ev) {
      var input = ev.target;
      if (!(input instanceof HTMLInputElement) || !input.classList.contains("sess-other")) return;
      var fieldset = input.closest(".sess-question");
      if (fieldset && !fieldset.querySelector('input[type="checkbox"]')) {
        fieldset.querySelectorAll('input[type="radio"]').forEach(function (r) {
          r.checked = false;
        });
      }
    });

    root.addEventListener("click", function (ev) {
      var target = ev.target;
      if (!(target instanceof Element)) return;

      if (target.closest(".sess-send-answer")) {
        var panel = target.closest(".sess-pending");
        var answers = {};
        var unanswered = [];
        panel.querySelectorAll(".sess-question").forEach(function (fieldset) {
          var picked = [];
          fieldset.querySelectorAll("input:checked").forEach(function (input) {
            picked.push(input.value);
          });
          var other = fieldset.querySelector(".sess-other");
          if (other && other.value.trim() !== "") {
            picked.push(other.value.trim());
          }
          // Exclusivity above keeps single-select to one value; multi-select
          // answers join with commas (design §4).
          var question = fieldset.getAttribute("data-question");
          if (picked.length === 0) {
            unanswered.push(question);
          }
          answers[question] = picked.join(", ");
        });
        if (unanswered.length > 0) {
          show(errEl, "Please answer every question before sending (missing: " + unanswered.join("; ") + ").");
          return;
        }
        postJSON(answersUrl, csrf, {
          request_id: panel.getAttribute("data-request-id"),
          behavior: "allow",
          answers: answers
        }).then(function () {
          errEl.hidden = true;
          panel.remove();
        }).catch(fail);
        return;
      }

      if (target.closest(".sess-allow") || target.closest(".sess-deny")) {
        var perm = target.closest(".sess-pending");
        var allow = !!target.closest(".sess-allow");
        var payload = {
          request_id: perm.getAttribute("data-request-id"),
          behavior: allow ? "allow" : "deny"
        };
        if (!allow) {
          var why = perm.querySelector(".sess-deny-reason");
          if (why && why.value.trim() !== "") {
            payload.message = why.value.trim();
          }
        }
        postJSON(answersUrl, csrf, payload).then(function () {
          perm.remove();
        }).catch(fail);
      }
    });

    document.getElementById("session-message-form").addEventListener("submit", function (ev) {
      ev.preventDefault();
      var input = document.getElementById("session-message-text");
      postJSON(messagesUrl, csrf, { text: input.value }).then(function () {
        input.value = "";
        errEl.hidden = true;
      }).catch(fail);
    });

    document.getElementById("session-stop").addEventListener("click", function () {
      postJSON(stopUrl, csrf, {}).catch(fail);
    });
  }
})();
