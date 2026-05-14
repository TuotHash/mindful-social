(function () {
  function icon(name) {
    return '<span class="material-symbols-outlined">' + name + '</span>';
  }

  function sanitizePreview(html) {
    var doc = new DOMParser().parseFromString(html, "text/html");
    doc.querySelectorAll("script, style, iframe, object, embed, form, input, button").forEach(function (node) {
      node.remove();
    });
    doc.body.querySelectorAll("*").forEach(function (node) {
      Array.from(node.attributes).forEach(function (attr) {
        var name = attr.name.toLowerCase();
        var value = attr.value.trim().toLowerCase();
        if (name.indexOf("on") === 0 || value.indexOf("javascript:") === 0) {
          node.removeAttribute(attr.name);
        }
      });
    });
    return doc.body.innerHTML;
  }

  function initMarkdownEditors(root) {
    if (!window.EasyMDE) return;
    root.querySelectorAll("textarea.markdown-editor").forEach(function (textarea) {
      if (textarea.dataset.easyMdeReady === "true") return;
      textarea.dataset.easyMdeReady = "true";

      var editor = new EasyMDE({
        element: textarea,
        autoDownloadFontAwesome: false,
        spellChecker: false,
        status: false,
        minHeight: "150px",
        forceSync: true,
        previewClass: ["editor-preview", "markdown-body"],
        renderingConfig: {
          sanitizerFunction: sanitizePreview,
        },
        toolbar: [
          { name: "bold", action: EasyMDE.toggleBold, icon: icon("format_bold"), title: "Bold" },
          { name: "italic", action: EasyMDE.toggleItalic, icon: icon("format_italic"), title: "Italic" },
          { name: "heading", action: EasyMDE.toggleHeadingSmaller, icon: icon("format_h2"), title: "Heading" },
          "|",
          { name: "quote", action: EasyMDE.toggleBlockquote, icon: icon("format_quote"), title: "Quote" },
          { name: "unordered-list", action: EasyMDE.toggleUnorderedList, icon: icon("format_list_bulleted"), title: "Bulleted list" },
          { name: "ordered-list", action: EasyMDE.toggleOrderedList, icon: icon("format_list_numbered"), title: "Numbered list" },
          "|",
          { name: "link", action: EasyMDE.drawLink, icon: icon("link"), title: "Link" },
          { name: "code", action: EasyMDE.toggleCodeBlock, icon: icon("code"), title: "Code" },
          { name: "table", action: EasyMDE.drawTable, icon: icon("table"), title: "Table" },
          "|",
          { name: "preview", action: EasyMDE.togglePreview, icon: icon("visibility"), title: "Preview", noDisable: true },
          { name: "side-by-side", action: EasyMDE.toggleSideBySide, icon: icon("splitscreen"), title: "Side by side", noDisable: true },
          { name: "fullscreen", action: EasyMDE.toggleFullScreen, icon: icon("fullscreen"), title: "Fullscreen", noDisable: true },
        ],
      });

      textarea.form && textarea.form.addEventListener("submit", function () {
        editor.codemirror.save();
      });
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    initMarkdownEditors(document);
  });

  document.addEventListener("htmx:afterSwap", function (event) {
    initMarkdownEditors(event.target || document);
  });
})();
