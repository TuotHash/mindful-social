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

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute("content") : "";
  }

  // resolveImageEndpoint inspects the textarea's data-image-* attributes and
  // returns the URL EasyMDE should POST uploads to. The "template" form
  // ("/nodes/{id}/images") is paired with a form-input name that supplies
  // the id at runtime — used on the New-post composer where the parent
  // topic isn't known until the user picks one.
  function resolveImageEndpoint(textarea) {
    if (textarea.dataset.imageEndpoint) return textarea.dataset.imageEndpoint;
    var template = textarea.dataset.imageEndpointTemplate;
    var sourceName = textarea.dataset.imageEndpointSource;
    if (!template || !sourceName || !textarea.form) return "";
    var checked = textarea.form.querySelector('input[name="' + sourceName + '"]:checked');
    var value = checked ? checked.value : "";
    if (!value) return "";
    return template.replace("{id}", encodeURIComponent(value));
  }

  function initMarkdownEditors(root) {
    if (!window.EasyMDE) return;
    root.querySelectorAll("textarea.markdown-editor").forEach(function (textarea) {
      if (textarea.dataset.easyMdeReady === "true") return;
      textarea.dataset.easyMdeReady = "true";

      var initialEndpoint = resolveImageEndpoint(textarea);
      var uploadEnabled = !!(initialEndpoint || textarea.dataset.imageEndpointTemplate);

      var options = {
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
        ],
      };

      if (uploadEnabled) {
        options.toolbar.push("|");
        options.toolbar.push({
          name: "upload-image",
          action: EasyMDE.drawUploadedImage,
          icon: icon("image"),
          title: "Upload image",
        });
        options.uploadImage = true;
        options.imagePathAbsolute = true;
        options.imageMaxSize = 8 * 1024 * 1024;
        options.imageAccept = "image/png, image/jpeg, image/gif";
        options.imageUploadEndpoint = initialEndpoint;
        options.imageCSRFHeader = true;
        options.imageCSRFName = "X-CSRF-Token";
        options.imageCSRFToken = csrfToken();
        options.errorMessages = {
          noFileGiven: "Choose an image to upload.",
          typeNotAllowed: "Only PNG, JPEG and GIF images are accepted.",
          fileTooLarge: "Images must be 8 MB or smaller.",
          importError: "Could not upload the image. Please try again.",
          noPermission: "You don't have permission to upload images here.",
        };
      }

      options.toolbar.push("|");
      options.toolbar.push({ name: "preview", action: EasyMDE.togglePreview, icon: icon("visibility"), title: "Preview", noDisable: true });
      options.toolbar.push({ name: "side-by-side", action: EasyMDE.toggleSideBySide, icon: icon("splitscreen"), title: "Side by side", noDisable: true });
      options.toolbar.push({ name: "fullscreen", action: EasyMDE.toggleFullScreen, icon: icon("fullscreen"), title: "Fullscreen", noDisable: true });

      var editor = new EasyMDE(options);

      // Keep the endpoint in sync when the user picks (or changes) the
      // parent topic on the composer. Without this the editor would keep
      // the empty endpoint captured at init and uploads would fail.
      var sourceName = textarea.dataset.imageEndpointSource;
      if (uploadEnabled && sourceName && textarea.form) {
        textarea.form.addEventListener("change", function (e) {
          if (!e.target || e.target.name !== sourceName) return;
          editor.options.imageUploadEndpoint = resolveImageEndpoint(textarea);
        });
      }

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
