// GitShelf client behaviors. Kept tiny and CSP-friendly (no inline handlers).
(function () {
  "use strict";

  // --- theme: resolve to a concrete light/dark so highlight.css [data-theme]
  // rules apply even in "auto" mode. ---
  var root = document.documentElement;
  function systemDark() {
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  function resolveTheme() {
    var stored = null;
    try { stored = localStorage.getItem("gitshelf-theme"); } catch (e) {}
    if (stored === "light" || stored === "dark") return stored;
    // honor an explicit server-set theme attribute, else system preference
    var attr = root.getAttribute("data-theme");
    if (attr === "light" || attr === "dark") return attr;
    return systemDark() ? "dark" : "light";
  }
  function applyTheme(t) { root.setAttribute("data-theme", t); }
  applyTheme(resolveTheme());

  document.addEventListener("DOMContentLoaded", function () {
    var toggle = document.getElementById("theme-toggle");
    if (toggle) {
      toggle.addEventListener("click", function () {
        var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
        applyTheme(next);
        try { localStorage.setItem("gitshelf-theme", next); } catch (e) {}
      });
    }

    // ref / branch selector navigation
    var selects = document.querySelectorAll("select.js-nav-select");
    Array.prototype.forEach.call(selects, function (sel) {
      sel.addEventListener("change", function () {
        if (sel.value) location.href = sel.value;
      });
    });

    // line-number anchor highlight on blob pages
    if (location.hash && /^#L\d+$/.test(location.hash)) {
      var el = document.querySelector(location.hash + ", [id='" + location.hash.slice(1) + "']");
      if (el) el.classList.add("line-highlight");
    }

    // Copy buttons on Markdown code blocks (GitHub-style).
    addCopyButtons(document.querySelectorAll(".markdown-body pre"));
  });

  function copyText(text) {
    // navigator.clipboard needs a secure context (https/localhost); LAN over
    // http falls back to a hidden textarea + execCommand.
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try {
        document.execCommand("copy") ? resolve() : reject();
      } catch (e) {
        reject(e);
      }
      document.body.removeChild(ta);
    });
  }

  function addCopyButtons(blocks) {
    Array.prototype.forEach.call(blocks, function (pre) {
      if (pre.querySelector(".copy-btn")) return;
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "copy-btn";
      btn.textContent = "Copy";
      btn.setAttribute("aria-label", "Copy code");
      btn.addEventListener("click", function () {
        var code = pre.querySelector("code") || pre;
        copyText(code.innerText).then(function () {
          btn.textContent = "Copied";
          btn.classList.add("copied");
          setTimeout(function () {
            btn.textContent = "Copy";
            btn.classList.remove("copied");
          }, 1500);
        }, function () {
          btn.textContent = "Failed";
          setTimeout(function () { btn.textContent = "Copy"; }, 1500);
        });
      });
      pre.classList.add("has-copy");
      pre.appendChild(btn);
    });
  }
})();
