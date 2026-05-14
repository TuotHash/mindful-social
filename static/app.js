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

  // resolveEndpoint inspects the textarea's data-<kind>-* attributes and
  // returns the URL the editor should POST uploads to. The "template" form
  // ("/nodes/{id}/images") is paired with a form-input name that supplies
  // the id at runtime — used on the New-post composer where the parent
  // topic isn't known until the user picks one.
  function resolveEndpoint(textarea, kind) {
    var direct = textarea.dataset[kind + "Endpoint"];
    if (direct) return direct;
    var template = textarea.dataset[kind + "EndpointTemplate"];
    var sourceName = textarea.dataset[kind + "EndpointSource"];
    if (!template || !sourceName || !textarea.form) return "";
    var checked = textarea.form.querySelector('input[name="' + sourceName + '"]:checked');
    var value = checked ? checked.value : "";
    if (!value) return "";
    return template.replace("{id}", encodeURIComponent(value));
  }

  function resolveImageEndpoint(textarea) { return resolveEndpoint(textarea, "image"); }
  function resolveVideoEndpoint(textarea) { return resolveEndpoint(textarea, "video"); }

  function hasVideoEndpoint(textarea) {
    return !!(textarea.dataset.videoEndpoint || textarea.dataset.videoEndpointTemplate);
  }

  var VIDEO_ERROR_MESSAGES = {
    noFileGiven: "Choose a video to upload.",
    typeNotAllowed: "That file isn't a video we can transcode.",
    fileTooLarge: "Videos must be 256 MB or smaller.",
    importError: "Could not upload the video. Please try again.",
    noPermission: "You don't have permission to upload videos here.",
  };

  // uploadVideoFile streams a single file to the video endpoint resolved
  // from the textarea (so endpoint switching when the composer's parent
  // topic changes Just Works) and resolves to the public /uploads path.
  function uploadVideoFile(textarea, file) {
    return new Promise(function (resolve, reject) {
      var endpoint = resolveVideoEndpoint(textarea);
      if (!endpoint) {
        reject(new Error(VIDEO_ERROR_MESSAGES.noPermission));
        return;
      }
      var form = new FormData();
      form.append("video", file);
      var xhr = new XMLHttpRequest();
      xhr.open("POST", endpoint);
      xhr.setRequestHeader("X-CSRF-Token", csrfToken());
      xhr.onload = function () {
        var body = null;
        try { body = JSON.parse(xhr.responseText); } catch (e) {}
        if (xhr.status >= 200 && xhr.status < 300 && body && body.data && body.data.filePath) {
          resolve(body.data.filePath);
          return;
        }
        var code = (body && body.error) || "importError";
        reject(new Error(VIDEO_ERROR_MESSAGES[code] || VIDEO_ERROR_MESSAGES.importError));
      };
      xhr.onerror = function () { reject(new Error(VIDEO_ERROR_MESSAGES.importError)); };
      xhr.send(form);
    });
  }

  // videoTag renders the HTML block the markdown sanitizer is configured
  // to accept verbatim. Keep this in sync with the bluemonday allow-list
  // in internal/views/markdown.go.
  function videoTag(filePath) {
    return '<video controls playsinline preload="metadata" src="' + filePath + '"></video>';
  }

  // triggerVideoUpload opens a hidden file picker, then uploads the
  // selected clip while a placeholder line keeps the spot in the
  // document. On success the placeholder is rewritten to the <video>
  // tag; on failure it is removed and the editor shows an error notice.
  function triggerVideoUpload(textarea, editor) {
    var input = document.createElement("input");
    input.type = "file";
    input.accept = "video/*";
    input.style.display = "none";
    document.body.appendChild(input);
    input.addEventListener("change", function () {
      var file = input.files && input.files[0];
      document.body.removeChild(input);
      if (!file) return;
      handleVideoUpload(textarea, editor, file);
    });
    input.click();
  }

  function handleVideoUpload(textarea, editor, file) {
    var cm = editor.codemirror;
    var doc = cm.getDoc();
    var cursor = doc.getCursor();
    var prefix = cursor.ch === 0 ? "" : "\n\n";
    var placeholder = prefix + "<!-- uploading video… -->\n\n";
    var from = { line: cursor.line, ch: cursor.ch };
    doc.replaceRange(placeholder, from);
    var to = doc.posFromIndex(doc.indexFromPos(from) + placeholder.length);
    var marker = doc.markText(from, to, { className: "cm-uploading", clearOnEnter: false });
    cm.setCursor(to);

    var finish = function (replacement) {
      var range = marker.find();
      marker.clear();
      if (!range) {
        // The user already edited the placeholder out — fall back to
        // splicing at the current cursor.
        var cur = doc.getCursor();
        doc.replaceRange(replacement, cur);
        return;
      }
      doc.replaceRange(replacement, range.from, range.to);
    };

    uploadVideoFile(textarea, file).then(function (filePath) {
      finish(prefix + videoTag(filePath) + "\n\n");
    }).catch(function (err) {
      finish("");
      if (typeof editor.element !== "undefined" && editor.element) {
        var msg = (err && err.message) || VIDEO_ERROR_MESSAGES.importError;
        alert(msg);
      }
    });
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

      var videoEnabled = hasVideoEndpoint(textarea);
      if (videoEnabled) {
        options.toolbar.push({
          name: "upload-video",
          action: function (mde) { triggerVideoUpload(textarea, mde); },
          icon: icon("movie"),
          title: "Upload video",
        });
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
