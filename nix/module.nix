# NixOS module for the Mindful Social server.
#
# Exposes the binary as a systemd service on 127.0.0.1, expects a
# reverse proxy (HAProxy / Caddy / nginx) to handle TLS upstream. By
# default provisions a local PostgreSQL database and a peer-authenticated
# unix socket; set services.mindful-social.database.createLocally to
# false to point at an external Postgres via DATABASE_URL.
{ config, lib, pkgs, ... }:

let
  cfg = config.services.mindful-social;
  ttsCfg = cfg.tts;

  # Snapshot of the TTS sidecar source files. Copied into the runtime
  # state directory on every start so a package upgrade replaces the
  # running code, while the venv + downloaded models stay where they
  # are (in /var/lib) instead of being rebuilt every deploy.
  ttsSource = pkgs.runCommand "mindful-tts-source" {} ''
    mkdir -p $out
    cp ${../tts/pyproject.toml}    $out/pyproject.toml
    cp ${../tts/uv.lock}           $out/uv.lock
    cp ${../tts/.python-version}   $out/.python-version
    cp ${../tts/server.py}         $out/server.py
    cp ${../tts/setup.sh}          $out/setup.sh
    cp ${../tts/run.sh}            $out/run.sh
    chmod +x $out/setup.sh $out/run.sh
  '';

  ttsSidecarURL = "http://${ttsCfg.listenAddr}:${toString ttsCfg.port}";
in {
  options.services.mindful-social = {
    enable = lib.mkEnableOption "the Mindful Social server";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix {};
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix {}";
      description = ''
        Mindful Social package to run. Defaults to building from the
        sibling package.nix, so the module works standalone on channels
        without the flake.
      '';
    };

    listenAddr = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      example = "127.0.0.1:8080";
      description = ''
        TCP address the HTTP server binds to. Keep it on 127.0.0.1 and
        terminate TLS in a reverse proxy in front of it.
      '';
    };

    logLevel = lib.mkOption {
      type = lib.types.enum [ "debug" "info" "warn" "error" ];
      default = "info";
      description = ''
        Minimum JSON log level emitted by the server.
      '';
    };

    publicBaseURL = lib.mkOption {
      type = lib.types.str;
      example = "https://mindful.example.org";
      description = ''
        Absolute origin the browser sees. OAuth callback URLs derive
        from this, so it must match the URL users actually visit.
      '';
    };

    signupEnabled = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Whether the email + password signup form is open. Setting this
        to false closes signups; OAuth/SSO logins still create accounts
        for new users.
      '';
    };

    adminUsers = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      example = [ "alice" "bob" ];
      description = ''
        Usernames promoted to the admin role on every startup. Unknown
        usernames are logged and skipped. Demotions through the admin
        UI stick — this list only grants admin, never revokes.
      '';
    };

    environment = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = {};
      example = {
        GOOGLE_CLIENT_ID = "1234.apps.googleusercontent.com";
        AI_ENDPOINT_URL = "http://127.0.0.1:11434/v1";
        AI_MODEL = "llama3.1:8b";
      };
      description = ''
        Additional environment variables merged into the systemd unit.
        Useful for non-secret OAuth client IDs and OIDC discovery URLs.
        Put actual secrets in environmentFile instead so they stay out
        of the Nix store.

        AI node drafting (optional) is configured here too: set
        AI_ENDPOINT_URL to an OpenAI-compatible endpoint (e.g. a local
        Ollama at http://127.0.0.1:11434/v1) and AI_MODEL to the model
        name. Leave AI_ENDPOINT_URL unset to keep the feature disabled.
        A hosted provider's AI_API_KEY belongs in environmentFile.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/mindful-social.env";
      description = ''
        Path to a systemd EnvironmentFile holding secret variables
        (OAuth client secrets, DATABASE_URL for external databases,
        AI_API_KEY for a hosted AI provider). Typically managed by
        sops-nix or agenix so the file is decrypted into /run/secrets
        at boot and never written to the Nix store.
      '';
    };

    nodeImage = {
      maxUploadBytes = lib.mkOption {
        type = lib.types.ints.positive;
        default = 8 * 1024 * 1024;
        description = ''
          Raw request-body ceiling (in bytes) for node image uploads,
          enforced before decoding. Anything larger is rejected with
          HTTP 413.
        '';
      };

      maxDimension = lib.mkOption {
        type = lib.types.ints.positive;
        default = 1920;
        description = ''
          Longest side, in pixels, of stored node images. Inputs larger
          than this are downscaled while preserving aspect ratio.
        '';
      };

      bytesPerMegapixel = lib.mkOption {
        type = lib.types.ints.positive;
        default = 500 * 1024;
        description = ''
          Target post-compression byte budget per megapixel. The JPEG
          encoder walks its quality ladder until the resized image fits
          this budget. GIFs bypass recompression to preserve animation.
        '';
      };
    };

    database = {
      createLocally = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          When true, enables PostgreSQL on the host, creates the
          mindful_social database and user, and points the service at
          the local unix socket using peer authentication. When false,
          DATABASE_URL must be supplied via environmentFile.
        '';
      };

      name = lib.mkOption {
        type = lib.types.str;
        default = "mindful-social";
        description = "Local database name. Only used when createLocally is true.";
      };

      user = lib.mkOption {
        type = lib.types.str;
        default = "mindful-social";
        description = "Local database role and matching system user. Only used when createLocally is true.";
      };
    };

    tts = {
      enable = lib.mkEnableOption "the Kokoro TTS sidecar (adds podcast-style narration to nodes)";

      listenAddr = lib.mkOption {
        type = lib.types.str;
        default = "127.0.0.1";
        description = ''
          Loopback address the Python sidecar binds to. Keep this on
          localhost; the main service talks to it over HTTP and nothing
          else should reach it.
        '';
      };

      port = lib.mkOption {
        type = lib.types.port;
        default = 8090;
        description = ''
          TCP port for the sidecar. The main service composes
          TTS_SIDECAR_URL from listenAddr + this value.
        '';
      };

      user = lib.mkOption {
        type = lib.types.str;
        default = "mindful-tts";
        description = ''
          System user that owns the sidecar's state directory (venv +
          downloaded models). Kept separate from the main service user
          so the TTS sandbox is independent — a future model bug can't
          touch the Postgres socket.
        '';
      };

      stateDirectoryName = lib.mkOption {
        type = lib.types.str;
        default = "mindful-social-tts";
        description = ''
          Name under /var/lib for the sidecar's state directory. The
          Python venv (.venv) and downloaded models (~700 MB) live
          there and persist across restarts.
        '';
      };

      memoryHigh = lib.mkOption {
        type = lib.types.str;
        default = "3G";
        description = ''
          systemd MemoryHigh budget for the sidecar. Loaded models sit
          around 1.5-2 GB; the cap leaves headroom for synthesis spikes
          without letting the unit grow into the rest of the VPS.
        '';
      };

      firstBootTimeout = lib.mkOption {
        type = lib.types.int;
        default = 30 * 60;
        description = ''
          TimeoutStartSec, in seconds. First boot has to install ~1 GB
          of Python wheels (PyTorch CPU is the biggest) and download
          two Kokoro ONNX models from Hugging Face. Subsequent starts
          are fast because both caches are idempotent.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable {
    # Local Postgres provisioning. ensureUsers grants the system user
    # access to its own database via peer auth on the unix socket — no
    # password required, no DATABASE_URL secret needed.
    services.postgresql = lib.mkIf cfg.database.createLocally {
      enable = true;
      ensureDatabases = [ cfg.database.name ];
      ensureUsers = [{
        name = cfg.database.user;
        ensureDBOwnership = true;
      }];
    };

    # A static system user is required when talking to local Postgres
    # via peer auth: the OS user name has to match the database role.
    # DynamicUser would generate a fresh UID every boot and break that.
    # The TTS user is independent — it never needs Postgres — but we
    # declare both here in a single attrset to keep the module
    # composable with itself.
    users.users = lib.mkMerge [
      (lib.mkIf cfg.database.createLocally {
        "${cfg.database.user}" = {
          isSystemUser = true;
          group = cfg.database.user;
          description = "Mindful Social service user";
        };
      })
      (lib.mkIf ttsCfg.enable {
        "${ttsCfg.user}" = {
          isSystemUser = true;
          group = ttsCfg.user;
          description = "Mindful Social TTS sidecar user";
        };
      })
    ];

    users.groups = lib.mkMerge [
      (lib.mkIf cfg.database.createLocally {
        "${cfg.database.user}" = {};
      })
      (lib.mkIf ttsCfg.enable {
        "${ttsCfg.user}" = {};
      })
    ];

    systemd.services.mindful-social = {
      description = "Mindful Social — community discourse server";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ]
        ++ lib.optional cfg.database.createLocally "postgresql.service";
      wants = [ "network-online.target" ]
        ++ lib.optional cfg.database.createLocally "postgresql.service";

      # ffmpeg/ffprobe drive the video-upload transcode pipeline. They
      # need to be on the unit's PATH because the handler resolves them
      # with exec.LookPath, and ProtectSystem=strict otherwise hides
      # /run/current-system/sw from the sandbox.
      path = [ pkgs.ffmpeg-headless ];

      environment = {
        LISTEN_ADDR = cfg.listenAddr;
        LOG_LEVEL = cfg.logLevel;
        PUBLIC_BASE_URL = cfg.publicBaseURL;
        SIGNUP_ENABLED = if cfg.signupEnabled then "true" else "false";
        NODE_IMAGE_MAX_UPLOAD_BYTES = toString cfg.nodeImage.maxUploadBytes;
        NODE_IMAGE_MAX_DIMENSION = toString cfg.nodeImage.maxDimension;
        NODE_IMAGE_BYTES_PER_MEGAPIXEL = toString cfg.nodeImage.bytesPerMegapixel;
        # Matches StateDirectory below. systemd creates and owns this
        # path, so it's the one place the sandboxed unit can write.
        DATA_DIR = "/var/lib/mindful-social";
      }
      // lib.optionalAttrs (cfg.adminUsers != []) {
        ADMIN_USERS = lib.concatStringsSep "," cfg.adminUsers;
      }
      // lib.optionalAttrs cfg.database.createLocally {
        DATABASE_URL = "postgres:///${cfg.database.name}?host=/run/postgresql";
      }
      // lib.optionalAttrs ttsCfg.enable {
        TTS_SIDECAR_URL = ttsSidecarURL;
      }
      // cfg.environment;

      serviceConfig = {
        ExecStart = lib.getExe cfg.package;
        Restart = "on-failure";
        RestartSec = 5;

        User = if cfg.database.createLocally then cfg.database.user else "mindful-social";
        Group = if cfg.database.createLocally then cfg.database.user else "mindful-social";
        DynamicUser = !cfg.database.createLocally;

        StateDirectory = "mindful-social";
        StateDirectoryMode = "0750";

        EnvironmentFile = lib.mkIf (cfg.environmentFile != null) cfg.environmentFile;

        # Sandboxing. The service only needs to bind a localhost TCP
        # port, talk to Postgres (unix socket or TCP), and reach OAuth
        # IdPs over HTTPS. Everything else gets locked down.
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        ProtectHostname = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        NoNewPrivileges = true;
        RestrictSUIDSGID = true;
        RestrictRealtime = true;
        RestrictNamespaces = true;
        LockPersonality = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [ "@system-service" "~@privileged" "~@resources" ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        CapabilityBoundingSet = [];
        AmbientCapabilities = [];
        UMask = "0077";
      };
    };

    # Kokoro TTS sidecar. Boots a Python venv inside StateDirectory on
    # first start (downloading wheels + Kokoro models — slow once, fast
    # forever after), then runs the FastAPI server. The main service
    # autoconfigures TTS_SIDECAR_URL above when ttsCfg.enable is true,
    # so the two units come up independently without ordering coupling.
    systemd.services.mindful-social-tts = lib.mkIf ttsCfg.enable {
      description = "Mindful Social — Kokoro TTS sidecar";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      # Runtime PATH needs:
      #   - bash       resolves the #!/usr/bin/env bash shebang in
      #                setup.sh and run.sh
      #   - coreutils  setup.sh calls cp, mkdir, etc.
      #   - uv         manages the Python venv
      #   - python3    interpreter the venv targets
      #   - espeak-ng  phonemizer used by misaki / Kokoro
      #   - ffmpeg     pipes synthesized WAV through libopus encoding
      path = with pkgs; [ bash coreutils uv python312 espeak-ng ffmpeg-headless ];

      environment = {
        # uv keeps the venv inside the state directory so it survives
        # reboots and package upgrades. Without this it would try to
        # build .venv next to pyproject.toml — which sits in the
        # read-only Nix store and would fail.
        UV_PROJECT_ENVIRONMENT = "/var/lib/${ttsCfg.stateDirectoryName}/.venv";

        # Pin uv's cache and the HuggingFace cache into the state dir
        # too, otherwise they'd land in $HOME (which doesn't exist for
        # the sandboxed unit) or /root (also denied).
        UV_CACHE_DIR = "/var/lib/${ttsCfg.stateDirectoryName}/.uv-cache";
        HF_HOME = "/var/lib/${ttsCfg.stateDirectoryName}/hf-cache";
        XDG_CACHE_HOME = "/var/lib/${ttsCfg.stateDirectoryName}/.cache";

        # Force uv to use the nix-provided Python instead of fetching
        # its own managed build. The managed-install path writes outside
        # the state directory and tries to call out to astral, which the
        # sandbox blocks anyway.
        UV_PYTHON_PREFERENCE = "only-system";

        # The Python wheels for numpy, torch, and onnxruntime are
        # pre-built against a standard FHS layout — they dlopen
        # libstdc++.so.6, libgomp.so.1, etc. by bare filename. On
        # NixOS those aren't on the default loader path, so we
        # explicitly publish a small set of system libraries the
        # wheels expect.
        LD_LIBRARY_PATH = lib.makeLibraryPath (with pkgs; [
          stdenv.cc.cc.lib   # libstdc++, libgomp, libgcc_s
          zlib               # libz — torch / onnxruntime
          openssl            # libssl, libcrypto — HF Hub TLS
        ]);

        TTS_HOST = ttsCfg.listenAddr;
        TTS_PORT = toString ttsCfg.port;
      };

      serviceConfig = {
        Type = "exec";

        # Two pre-start steps. The first runs as root (the "+" prefix
        # disables the User= switch for this line only) and fixes
        # ownership of the state directory. systemd creates
        # StateDirectory the first time the unit starts but doesn't
        # re-chown it if User= changes later, so a state dir left over
        # from a deploy that used a different user (or none) would
        # stay root-owned and uv would EACCES on the cache dir. The
        # chown is idempotent and cheap.
        #
        # The second step runs as the unit user and does the actual
        # work: refresh source files from the store, then run setup.sh
        # (which itself is idempotent).
        ExecStartPre = [
          "+${pkgs.coreutils}/bin/chown -R ${ttsCfg.user}:${ttsCfg.user} /var/lib/${ttsCfg.stateDirectoryName}"
          (pkgs.writeShellScript "mindful-tts-prestart" ''
            set -eu
            cd "$STATE_DIRECTORY"
            ${pkgs.rsync}/bin/rsync -a --delete \
              --exclude=.venv --exclude=models \
              --exclude=.uv-cache --exclude=hf-cache --exclude=.cache \
              ${ttsSource}/ "$STATE_DIRECTORY"/
            # rsync -a preserves perms, and Nix-store source files are
            # 0444 / 0555 (read-only) — so without this chmod the
            # state directory itself ends up 0555 and uv hits EACCES
            # trying to create .uv-cache, .venv, etc. Restoring owner
            # write to every synced path is idempotent and cheap.
            ${pkgs.coreutils}/bin/chmod -R u+w "$STATE_DIRECTORY"
            ./setup.sh
          '').outPath
        ];

        ExecStart = "${pkgs.bash}/bin/bash -c './run.sh'";
        WorkingDirectory = "/var/lib/${ttsCfg.stateDirectoryName}";

        User = ttsCfg.user;
        Group = ttsCfg.user;

        StateDirectory = ttsCfg.stateDirectoryName;
        StateDirectoryMode = "0750";

        Restart = "on-failure";
        RestartSec = 10;
        TimeoutStartSec = ttsCfg.firstBootTimeout;

        # Memory ceiling. MemoryHigh throttles softly, MemoryMax is the
        # hard kill point at 1.5x the soft target.
        MemoryHigh = ttsCfg.memoryHigh;
        MemoryMax = ttsCfg.memoryHigh;

        # Sandboxing. The sidecar only needs to bind a localhost port
        # and reach out to PyPI / HuggingFace on first boot. Everything
        # else gets locked down, same shape as the main service.
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        ProtectHostname = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        NoNewPrivileges = true;
        RestrictSUIDSGID = true;
        RestrictRealtime = true;
        RestrictNamespaces = true;
        LockPersonality = true;
        SystemCallArchitectures = "native";
        # @resources is allowed here (unlike the main service) because
        # PyTorch's CPU backend touches scheduler affinity calls.
        SystemCallFilter = [ "@system-service" "~@privileged" ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        CapabilityBoundingSet = [];
        AmbientCapabilities = [];
        UMask = "0077";
      };
    };
  };
}
