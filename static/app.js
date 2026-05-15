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

  function closestForm(target) {
    if (!target) return null;
    if (target.tagName === "FORM") return target;
    return target.closest ? target.closest("form") : null;
  }

  function formErrorScope(form) {
    return form.closest(".modal-dialog, .page.narrow, .auth, .account-field, section.card") || form.parentElement;
  }

  function firstInvalidControl(form) {
    return Array.from(form.elements || []).find(function (field) {
      return field.willValidate && !field.validity.valid;
    }) || null;
  }

  function fieldLabel(field) {
    var explicit = field.getAttribute("aria-label") || field.dataset.errorLabel;
    if (explicit) return explicit;

    var fieldset = field.type === "radio" ? field.closest("fieldset") : null;
    var legend = fieldset && fieldset.querySelector("legend");
    if (legend) return legend.textContent.trim();

    var label = field.closest("label");
    if (label) {
      var clone = label.cloneNode(true);
      clone.querySelectorAll("input, textarea, select, small, .EasyMDEContainer").forEach(function (node) {
        node.remove();
      });
      var text = clone.textContent.replace(/\s+/g, " ").trim();
      if (text) return text;
    }

    return field.name ? field.name.replace(/[_-]+/g, " ") : "This field";
  }

  function validationMessage(field) {
    var label = fieldLabel(field);
    var validity = field.validity || {};
    if (validity.valueMissing) return label + " is required.";
    if (validity.typeMismatch && field.type === "email") return "Enter a valid email address.";
    if (validity.typeMismatch && field.type === "url") return "Enter a valid URL.";
    if (validity.tooShort) return label + " must be at least " + field.minLength + " characters.";
    if (validity.tooLong) return label + " must be " + field.maxLength + " characters or fewer.";
    if (validity.patternMismatch && field.name === "username") {
      return "Username must be 3-32 characters: letters, digits, dot, dash, underscore.";
    }
    return field.validationMessage || "Check the highlighted field.";
  }

  function setFieldInvalid(field, invalid) {
    if (!field) return;
    if (invalid) {
      field.setAttribute("aria-invalid", "true");
    } else {
      field.removeAttribute("aria-invalid");
    }

    var group = field.type === "radio" ? field.closest("fieldset") : null;
    if (group) {
      if (invalid) group.setAttribute("aria-invalid", "true");
      else group.removeAttribute("aria-invalid");
    }

    var editor = field.nextElementSibling && field.nextElementSibling.classList.contains("EasyMDEContainer")
      ? field.nextElementSibling
      : null;
    if (editor) {
      if (invalid) editor.setAttribute("aria-invalid", "true");
      else editor.removeAttribute("aria-invalid");
    }
  }

  function clearInvalidState(form) {
    form.querySelectorAll('[aria-invalid="true"]').forEach(function (field) {
      field.removeAttribute("aria-invalid");
    });
  }

  function clientErrorBanner(form) {
    if (!form) return null;
    var scope = formErrorScope(form);
    if (!scope) return null;

    var existing = scope.querySelector('.flash[data-client-error="true"], .flash[role="alert"]');
    if (existing) return existing;

    var banner = document.createElement("div");
    banner.className = "flash danger";
    banner.setAttribute("role", "alert");
    banner.setAttribute("data-client-error", "true");

    var anchor = form;
    while (anchor.parentElement && anchor.parentElement !== scope) {
      anchor = anchor.parentElement;
    }
    scope.insertBefore(banner, anchor);
    return banner;
  }

  function showClientError(target, message) {
    var form = closestForm(target);
    var banner = clientErrorBanner(form);
    if (!banner) return;
    banner.textContent = message;
    banner.hidden = false;
    banner.classList.add("danger");
    banner.setAttribute("role", "alert");
    banner.setAttribute("data-client-error", "true");
    banner.scrollIntoView && banner.scrollIntoView({ block: "nearest" });
  }

  function clearClientError(form) {
    if (!form) return;
    var scope = formErrorScope(form);
    if (!scope) return;
    scope.querySelectorAll('.flash[data-client-error="true"]').forEach(function (banner) {
      banner.hidden = true;
      banner.textContent = "";
    });
  }

  function selectedValue(form, name) {
    var checked = form.querySelector('input[name="' + name + '"]:checked');
    return checked ? checked.value : "";
  }

  function setRadioRequired(form, name, required) {
    form.querySelectorAll('input[type="radio"][name="' + name + '"]').forEach(function (input) {
      input.required = required;
    });
  }

  function syncConditionalRequirements(form) {
    if (form.classList.contains("post-form")) {
      var type = selectedValue(form, "type");
      var topicMode = selectedValue(form, "topic_parent_mode") || "root";
      var needsParent = type === "view" || (type === "topic" && topicMode === "sub");
      setRadioRequired(form, "parent_topic_id", needsParent);
    }

    var toMode = selectedValue(form, "to_mode");
    if (toMode) {
      var existingMode = toMode !== "new";
      var targets = form.querySelectorAll('input[type="radio"][name="to_id"]');
      targets.forEach(function (input) {
        input.required = existingMode;
        input.disabled = !existingMode;
      });

      var newFindingTitle = form.querySelector('input[name="new_finding_title"]');
      if (newFindingTitle) {
        newFindingTitle.required = !existingMode;
        newFindingTitle.disabled = existingMode;
      }
    }
  }

  function customFormError(form) {
    if (form.classList.contains("post-form")) {
      var type = selectedValue(form, "type");
      var topicMode = selectedValue(form, "topic_parent_mode") || "root";
      var needsParent = type === "view" || (type === "topic" && topicMode === "sub");
      if (needsParent && !selectedValue(form, "parent_topic_id")) {
        return {
          field: form.querySelector('input[name="find_topic"]') || form.querySelector('input[name="parent_topic_id"]'),
          message: type === "topic"
            ? "A sub-topic must be connected to a parent topic. Search and select one above."
            : "A view must be connected to a parent topic. Search and select one above.",
        };
      }
    }

    var toMode = selectedValue(form, "to_mode");
    if (toMode === "new") {
      var title = form.querySelector('input[name="new_finding_title"]');
      if (title && title.value.trim() === "") {
        return { field: title, message: "Type a title for the new finding." };
      }
    } else if (toMode === "existing" && !selectedValue(form, "to_id")) {
      return {
        field: form.querySelector('input[name="find"]') || form.querySelector('input[name="to_id"]'),
        message: "Select a target node.",
      };
    }

    return null;
  }

  function focusInvalidField(field) {
    if (!field) return;
    if (field.focus) field.focus({ preventScroll: true });
    field.scrollIntoView && field.scrollIntoView({ block: "center" });
  }

  function showFieldError(form, field, message) {
    clearInvalidState(form);
    setFieldInvalid(field, true);
    showClientError(form, message);
    focusInvalidField(field);
  }

  function validateClientForm(form) {
    syncConditionalRequirements(form);

    var custom = customFormError(form);
    if (custom) {
      showFieldError(form, custom.field, custom.message);
      return false;
    }

    if (form.checkValidity && !form.checkValidity()) {
      var invalid = firstInvalidControl(form);
      showFieldError(form, invalid, invalid ? validationMessage(invalid) : "Check the highlighted field.");
      return false;
    }

    return true;
  }

  function initClientErrors(root) {
    root.querySelectorAll("form").forEach(function (form) {
      if (form.dataset.clientErrorsReady === "true") return;
      form.dataset.clientErrorsReady = "true";
      syncConditionalRequirements(form);

      form.addEventListener("invalid", function (event) {
        event.preventDefault();
        var first = firstInvalidControl(form) || event.target;
        if (event.target !== first) return;
        showFieldError(form, first, validationMessage(first));
      }, true);

      form.addEventListener("submit", function (event) {
        if (validateClientForm(form)) return;
        event.preventDefault();
        event.stopPropagation();
      });

      form.addEventListener("input", function (event) {
        if (!event.target || !event.target.matches("input, textarea, select")) return;
        syncConditionalRequirements(form);
        setFieldInvalid(event.target, false);
        clearClientError(form);
      });

      form.addEventListener("change", function (event) {
        if (!event.target || !event.target.matches("input, textarea, select")) return;
        syncConditionalRequirements(form);
        setFieldInvalid(event.target, false);
        clearClientError(form);
      });
    });
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

  var IMAGE_ERROR_MESSAGES = {
    noFileGiven: "Choose an image to upload.",
    typeNotAllowed: "Only PNG, JPEG and GIF images are accepted.",
    fileTooLarge: "Images must be 8 MB or smaller.",
    importError: "Could not upload the image. Please try again.",
    noPermission: "You don't have permission to upload images here.",
  };

  function uploadImageFile(textarea, file, onSuccess, onError, options) {
    var messages = options.errorMessages || IMAGE_ERROR_MESSAGES;
    if (!file) {
      onError(messages.noFileGiven);
      return;
    }
    if (file.size > options.imageMaxSize) {
      onError(messages.fileTooLarge);
      return;
    }
    if (["image/png", "image/jpeg", "image/gif"].indexOf(file.type) < 0) {
      onError(messages.typeNotAllowed);
      return;
    }

    var endpoint = resolveImageEndpoint(textarea);
    if (!endpoint) {
      onError(messages.noPermission);
      return;
    }

    var form = new FormData();
    form.append("image", file);
    var xhr = new XMLHttpRequest();
    xhr.open("POST", endpoint);
    xhr.setRequestHeader("X-CSRF-Token", csrfToken());
    xhr.onload = function () {
      var body = null;
      try { body = JSON.parse(xhr.responseText); } catch (e) {}
      if (xhr.status >= 200 && xhr.status < 300 && body && body.data && body.data.filePath) {
        clearClientError(textarea.form);
        onSuccess(body.data.filePath);
        return;
      }
      var code = (body && body.error) || "importError";
      onError(messages[code] || messages.importError);
    };
    xhr.onerror = function () { onError(messages.importError); };
    xhr.send(form);
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
      clearClientError(textarea.form);
      finish(prefix + videoTag(filePath) + "\n\n");
    }).catch(function (err) {
      finish("");
      if (typeof editor.element !== "undefined" && editor.element) {
        var msg = (err && err.message) || VIDEO_ERROR_MESSAGES.importError;
        showClientError(textarea.form || textarea, msg);
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
        options.errorMessages = IMAGE_ERROR_MESSAGES;
        options.errorCallback = function (message) {
          showClientError(textarea.form || textarea, message || IMAGE_ERROR_MESSAGES.importError);
        };
        options.imageUploadFunction = function (file, onSuccess, onError) {
          uploadImageFile(textarea, file, onSuccess, onError, options);
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

  function initArgumentGraphs(root) {
    root.querySelectorAll("[data-argument-graph]").forEach(function (graph) {
      if (graph.dataset.argumentGraphReady === "true") return;
      graph.dataset.argumentGraphReady = "true";

      var dataEl = graph.querySelector("[data-argument-graph-data]");
      var svg = graph.querySelector("[data-graph-svg]");
      if (!dataEl || !svg) return;

      var data = { nodes: [], edges: [] };
      try {
        data = JSON.parse(dataEl.textContent || "{}") || data;
      } catch (e) {
        data = { nodes: [], edges: [] };
      }
      var nodesByID = {};
      var edges = [];
      var adjacency = {};

      function normalizeGraphData(next) {
        data = next || { nodes: [], edges: [] };
        data.nodes = Array.isArray(data.nodes) ? data.nodes : [];
        data.edges = Array.isArray(data.edges) ? data.edges : [];

        nodesByID = {};
        data.nodes.forEach(function (node) {
          nodesByID[node.id] = node;
        });

        edges = data.edges.filter(function (edge) {
          edge.kind = edge.kind === "relates_to" ? "related" : edge.kind;
          return nodesByID[edge.from] && nodesByID[edge.to];
        });

        adjacency = {};
        edges.forEach(function (edge) {
          adjacency[edge.from] = adjacency[edge.from] || {};
          adjacency[edge.to] = adjacency[edge.to] || {};
          adjacency[edge.from][edge.to] = true;
          adjacency[edge.to][edge.from] = true;
        });
      }

      normalizeGraphData(data);

      var search = graph.querySelector("[data-graph-search]");
      var typeInputs = Array.from(graph.querySelectorAll("[data-graph-type]"));
      var depthInput = graph.querySelector("[data-graph-depth]");
      var depthValueEl = graph.querySelector("[data-graph-depth-value]");
      var visibleCount = graph.querySelector("[data-graph-visible-count]");
      var titleEl = graph.querySelector("[data-graph-title]");
      var metaEl = graph.querySelector("[data-graph-meta]");
      var openEl = graph.querySelector("[data-graph-open]");
      var graphEndpoint = graph.dataset.argumentGraphEndpoint || "/graph/data";
      var serverQuery = ((search && search.value) || "").trim().toLowerCase();
      var searchTimer = null;
      var searchSeq = 0;
      var markerPrefix = "argument-graph-arrow-" + Math.random().toString(36).slice(2);
      var selectedID = "";
      var viewport = null;
      var viewHeight = 560;
      var zoom = 1;
      var pan = { x: 0, y: 0 };
      var dragging = false;
      var lastPointer = null;

      function svgEl(name, attrs) {
        var el = document.createElementNS("http://www.w3.org/2000/svg", name);
        Object.keys(attrs || {}).forEach(function (key) {
          el.setAttribute(key, attrs[key]);
        });
        return el;
      }

      function hashString(value) {
        var hash = 0;
        for (var i = 0; i < value.length; i++) {
          hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
        }
        return hash;
      }

      function truncate(value, max) {
        value = value || "";
        if (value.length <= max) return value;
        return value.slice(0, Math.max(0, max - 1)) + "…";
      }

      function activeTypes() {
        var active = {};
        typeInputs.forEach(function (input) {
          active[input.value] = input.checked;
        });
        return active;
      }

      function matchesQuery(node, query) {
        if (!query) return true;
        var haystack = [
          node.title || "",
          node.authorUsername || "",
          node.type || "",
        ].join(" ").toLowerCase();
        return haystack.indexOf(query) >= 0;
      }

      function currentQuery() {
        return ((search && search.value) || "").trim().toLowerCase();
      }

      // currentDepth reads the connection-depth slider — the number of
      // extra hops to include around each direct match. Default 2 keeps
      // a node's neighbours and their neighbours visible without dragging
      // in distant subgraphs the user didn't search for.
      function currentDepth() {
        if (!depthInput) return 2;
        var v = parseInt(depthInput.value, 10);
        if (isNaN(v) || v < 0) return 0;
        return v;
      }

      function filteredNodes() {
        var active = activeTypes();
        var query = currentQuery();

        if (!query) {
          return data.nodes.filter(function (node) {
            return !!active[node.type];
          });
        }

        // Trust the server's match flag once its response has caught up
        // with the user's typing; until then fall back to a local
        // substring match against the previous response. This keeps the
        // depth-slider BFS working both during typing and after settle —
        // the prior code disabled the client filter as soon as the
        // server query matched, which left the slider with nothing to do.
        var useMatchFlags = (query === serverQuery);
        var keep = {};
        var frontier = [];
        data.nodes.forEach(function (node) {
          if (!active[node.type]) return;
          var seed = useMatchFlags ? node.match === true : matchesQuery(node, query);
          if (seed) {
            keep[node.id] = true;
            frontier.push(node.id);
          }
        });

        var depth = currentDepth();
        for (var hop = 0; hop < depth && frontier.length > 0; hop++) {
          var next = [];
          for (var i = 0; i < frontier.length; i++) {
            var neighbors = adjacency[frontier[i]];
            if (!neighbors) continue;
            var ids = Object.keys(neighbors);
            for (var j = 0; j < ids.length; j++) {
              var nid = ids[j];
              if (keep[nid]) continue;
              keep[nid] = true;
              next.push(nid);
            }
          }
          frontier = next;
        }

        return data.nodes.filter(function (node) {
          if (!active[node.type]) return false;
          return keep[node.id];
        });
      }

      function layout(nodes) {
        var grouped = { topic: [], view: [], finding: [], other: [] };
        nodes.forEach(function (node) {
          (grouped[node.type] || grouped.other).push(node);
        });
        Object.keys(grouped).forEach(function (key) {
          grouped[key].sort(function (a, b) {
            return (a.title || "").localeCompare(b.title || "");
          });
        });

        var maxCount = Math.max(grouped.topic.length, grouped.view.length, grouped.finding.length, grouped.other.length, 1);
        viewHeight = Math.max(560, maxCount * 92 + 120);
        var columns = { topic: 210, view: 600, finding: 990, other: 600 };

        Object.keys(grouped).forEach(function (type) {
          var list = grouped[type];
          var gap = viewHeight / (list.length + 1);
          list.forEach(function (node, index) {
            var jitter = (hashString(node.id) % 45) - 22;
            node.x = columns[type] + jitter;
            node.y = Math.round((index + 1) * gap);
          });
        });
      }

      function markerID(kind) {
        return markerPrefix + "-" + kind;
      }

      function appendMarkers(defs) {
        ["supports", "opposes", "related"].forEach(function (kind) {
          var marker = svgEl("marker", {
            id: markerID(kind),
            viewBox: "0 0 10 10",
            refX: "9",
            refY: "5",
            markerWidth: "7",
            markerHeight: "7",
            orient: "auto",
            class: "argument-graph-arrow edge-" + kind,
          });
          marker.appendChild(svgEl("path", { d: "M0,0 L10,5 L0,10 z" }));
          defs.appendChild(marker);
        });
      }

      function edgePath(from, to) {
        var dx = to.x - from.x;
        var bend = Math.max(70, Math.min(190, Math.abs(dx) * 0.45));
        if (dx < 0) bend = -bend;
        return [
          "M", from.x, from.y,
          "C", from.x + bend, from.y,
          to.x - bend, to.y,
          to.x, to.y,
        ].join(" ");
      }

      function nodeRadius(node) {
        if (node.type === "topic") return 29;
        if (node.type === "view") return 24;
        return 20;
      }

      function renderInspector() {
        var node = nodesByID[selectedID];
        if (!titleEl || !metaEl || !openEl) return;
        titleEl.textContent = node ? node.title : "Choose a node";
        metaEl.replaceChildren();

        if (!node) {
          openEl.hidden = true;
          openEl.setAttribute("href", "#");
          return;
        }

        var chip = document.createElement("span");
        chip.className = "chip " + node.type;
        var dot = document.createElement("span");
        dot.className = "dot";
        chip.appendChild(dot);
        chip.appendChild(document.createTextNode(node.type));

        var author = document.createElement("span");
        author.textContent = "by " + node.authorUsername;

        var degree = edges.filter(function (edge) {
          return edge.from === node.id || edge.to === node.id;
        }).length;
        var connections = document.createElement("span");
        connections.textContent = degree === 1 ? "1 connection" : degree + " connections";

        metaEl.appendChild(chip);
        metaEl.appendChild(author);
        metaEl.appendChild(connections);

        openEl.hidden = false;
        openEl.setAttribute("href", "/nodes/" + node.slug);
      }

      function applyTransform() {
        if (!viewport) return;
        viewport.setAttribute("transform", "matrix(" + zoom + " 0 0 " + zoom + " " + pan.x + " " + pan.y + ")");
      }

      function clampZoom(value) {
        return Math.max(0.55, Math.min(2.4, value));
      }

      function svgPointFromClient(clientX, clientY) {
        var matrix = svg.getScreenCTM && svg.getScreenCTM();
        if (matrix && svg.createSVGPoint) {
          var point = svg.createSVGPoint();
          point.x = clientX;
          point.y = clientY;
          return point.matrixTransform(matrix.inverse());
        }

        var rect = svg.getBoundingClientRect();
        return {
          x: ((clientX - rect.left) * 1200) / Math.max(1, rect.width),
          y: ((clientY - rect.top) * viewHeight) / Math.max(1, rect.height),
        };
      }

      function svgCenterPoint() {
        var rect = svg.getBoundingClientRect();
        return svgPointFromClient(rect.left + rect.width / 2, rect.top + rect.height / 2);
      }

      function zoomAt(nextZoom, anchor) {
        nextZoom = clampZoom(nextZoom);
        if (nextZoom === zoom) return;

        var ratio = nextZoom / zoom;
        pan.x = anchor.x - (anchor.x - pan.x) * ratio;
        pan.y = anchor.y - (anchor.y - pan.y) * ratio;
        zoom = nextZoom;
        applyTransform();
      }

      function render() {
        var nodes = filteredNodes();
        var visible = {};
        nodes.forEach(function (node) {
          visible[node.id] = true;
        });
        var visibleEdges = edges.filter(function (edge) {
          return visible[edge.from] && visible[edge.to];
        });

        if (selectedID && !visible[selectedID]) {
          selectedID = "";
        }

        layout(nodes);
        svg.setAttribute("viewBox", "0 0 1200 " + viewHeight);
        while (svg.firstChild) svg.removeChild(svg.firstChild);

        var defs = svgEl("defs");
        appendMarkers(defs);
        svg.appendChild(defs);

        viewport = svgEl("g", { class: "argument-graph-viewport" });
        viewport.appendChild(svgEl("rect", {
          class: "argument-graph-background",
          x: "0",
          y: "0",
          width: "1200",
          height: String(viewHeight),
        }));

        var edgeLayer = svgEl("g", { class: "argument-graph-edges" });
        visibleEdges.forEach(function (edge) {
          var from = nodesByID[edge.from];
          var to = nodesByID[edge.to];
          if (!from || !to) return;
          var path = svgEl("path", {
            class: "argument-graph-edge edge-" + edge.kind + (selectedID && edge.from !== selectedID && edge.to !== selectedID ? " is-dim" : ""),
            d: edgePath(from, to),
            "marker-end": "url(#" + markerID(edge.kind) + ")",
          });
          var title = svgEl("title");
          title.textContent = (from.title || "") + " " + edge.kind + " " + (to.title || "");
          path.appendChild(title);
          edgeLayer.appendChild(path);
        });
        viewport.appendChild(edgeLayer);

        var nodeLayer = svgEl("g", { class: "argument-graph-nodes" });
        nodes.forEach(function (node) {
          var radius = nodeRadius(node);
          var related = selectedID && (node.id === selectedID || (adjacency[selectedID] && adjacency[selectedID][node.id]));
          var cls = "argument-graph-node node-" + node.type;
          if (node.id === selectedID) cls += " is-selected";
          if (selectedID && !related) cls += " is-dim";

          var groupEl = svgEl("g", {
            class: cls,
            transform: "translate(" + node.x + " " + node.y + ")",
            tabindex: "0",
            role: "button",
            "aria-label": node.title,
          });
          var title = svgEl("title");
          title.textContent = node.title + " by " + node.authorUsername;
          groupEl.appendChild(title);
          groupEl.appendChild(svgEl("circle", { r: String(radius), cx: "0", cy: "0" }));
          groupEl.appendChild(svgEl("rect", {
            class: "argument-graph-label-box",
            x: "-84",
            y: String(radius + 10),
            width: "168",
            height: "31",
            rx: "7",
          }));
          var text = svgEl("text", {
            class: "argument-graph-label",
            x: "0",
            y: String(radius + 30),
            "text-anchor": "middle",
          });
          text.textContent = truncate(node.title, 25);
          groupEl.appendChild(text);
          groupEl.addEventListener("click", function (event) {
            event.stopPropagation();
            selectedID = node.id;
            render();
          });
          groupEl.addEventListener("dblclick", function () {
            window.location.href = "/nodes/" + node.slug;
          });
          groupEl.addEventListener("keydown", function (event) {
            if (event.key !== "Enter" && event.key !== " ") return;
            event.preventDefault();
            selectedID = node.id;
            render();
          });
          nodeLayer.appendChild(groupEl);
        });
        viewport.appendChild(nodeLayer);
        svg.appendChild(viewport);
        applyTransform();

        if (visibleCount) {
          var noun = nodes.length === 1 ? "node" : "nodes";
          visibleCount.textContent = nodes.length + " " + noun + " visible";
        }
        renderInspector();
      }

      function resetView() {
        zoom = 1;
        pan = { x: 0, y: 0 };
        applyTransform();
      }

      function renderFromFilter() {
        resetView();
        render();
      }

      function graphDataURL(query) {
        var url = new URL(graphEndpoint, window.location.href);
        if (query) url.searchParams.set("q", query);
        return url.toString();
      }

      function fetchServerGraph(query) {
        if (!window.fetch) return;
        var seq = ++searchSeq;
        fetch(graphDataURL(query), {
          headers: { "Accept": "application/json" },
          credentials: "same-origin",
        }).then(function (response) {
          if (!response.ok) throw new Error("graph search failed");
          return response.json();
        }).then(function (nextData) {
          if (seq !== searchSeq || query !== currentQuery()) return;
          normalizeGraphData(nextData);
          serverQuery = query;
          selectedID = "";
          resetView();
          render();
        }).catch(function () {
          if (seq === searchSeq) serverQuery = "";
        });
      }

      function queueServerSearch() {
        if (searchTimer) clearTimeout(searchTimer);
        searchTimer = setTimeout(function () {
          fetchServerGraph(currentQuery());
        }, 220);
      }

      if (search) search.addEventListener("input", function () {
        renderFromFilter();
        queueServerSearch();
      });
      typeInputs.forEach(function (input) {
        input.addEventListener("change", renderFromFilter);
      });

      function syncDepthDisplay() {
        if (depthValueEl) depthValueEl.textContent = String(currentDepth());
      }
      if (depthInput) {
        syncDepthDisplay();
        depthInput.addEventListener("input", function () {
          syncDepthDisplay();
          render();
        });
      }

      graph.querySelectorAll("[data-graph-zoom]").forEach(function (button) {
        button.addEventListener("click", function () {
          var action = button.dataset.graphZoom;
          if (action === "reset") {
            resetView();
          } else {
            zoomAt(zoom * (action === "in" ? 1.2 : 0.84), svgCenterPoint());
          }
        });
      });

      svg.addEventListener("click", function (event) {
        if (!event.target.classList || !event.target.classList.contains("argument-graph-background")) return;
        selectedID = "";
        render();
      });

      svg.addEventListener("pointerdown", function (event) {
        if (event.target.closest && event.target.closest(".argument-graph-node")) return;
        dragging = true;
        lastPointer = { x: event.clientX, y: event.clientY };
        svg.setPointerCapture && svg.setPointerCapture(event.pointerId);
      });
      svg.addEventListener("pointermove", function (event) {
        if (!dragging || !lastPointer) return;
        var rect = svg.getBoundingClientRect();
        var dx = event.clientX - lastPointer.x;
        var dy = event.clientY - lastPointer.y;
        pan.x += (dx * 1200) / Math.max(1, rect.width);
        pan.y += (dy * viewHeight) / Math.max(1, rect.height);
        lastPointer = { x: event.clientX, y: event.clientY };
        applyTransform();
      });
      svg.addEventListener("pointerup", function (event) {
        dragging = false;
        lastPointer = null;
        svg.releasePointerCapture && svg.releasePointerCapture(event.pointerId);
      });
      svg.addEventListener("pointercancel", function () {
        dragging = false;
        lastPointer = null;
      });
      svg.addEventListener("wheel", function (event) {
        event.preventDefault();
        zoomAt(zoom * (event.deltaY < 0 ? 1.08 : 0.92), svgPointFromClient(event.clientX, event.clientY));
      }, { passive: false });

      render();
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    initClientErrors(document);
    initMarkdownEditors(document);
    initArgumentGraphs(document);
  });

  document.addEventListener("htmx:afterSwap", function (event) {
    initClientErrors(event.target || document);
    initMarkdownEditors(event.target || document);
    initArgumentGraphs(event.target || document);
  });
})();
